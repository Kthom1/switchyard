package core

// Upgrade is one transaction with a single commit point, writing BUILD.txt.
//
// Everything it changes is recorded in work/upgrade/journal before it changes.
// The journal's first line is a header written atomically before any service
// stops: the release it upgrades from and to, the bundle, and the options. Each
// later line names an absolute path and whether it existed; its original is
// saved under work/upgrade/previous first. One line marks that the new release
// was started and whether its Plane images differ, decided before it starts,
// since afterwards Plane may have migrated its data.
//
// Every run takes the installation lock and recovers before anything else:
//
//   - no journal: nothing changed since a previous run finished, so any
//     leftover work/upgrade directory is removed;
//   - BUILD.txt names the journal's target: the upgrade committed, so its
//     cleanup (CLI link, backup, journal, pruning) is finished;
//   - otherwise: it did not commit, so it is rolled back once the runner is
//     idle, and the previous release is started again.
//
// Only then does the run do what was asked. Each recovery step can be repeated,
// so an interruption at any point is handled by running upgrade again from any
// release. A failure before the commit rolls back the same way.

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Kthom1/switchyard/config"
)

// idleRetry is how often --wait checks the runner again.
var idleRetry = 30 * time.Second

// crashPoint lets tests stop an upgrade the way a killed process would.
var crashPoint = func(step string) {}

type UpgradeOptions struct {
	Bundle     string // extracted release directory holding switchyard, bin/symphony and BUILD.txt
	Wait       bool
	KeepBackup bool
}

// transaction is the journal header.
type transaction struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Bundle     string `json:"bundle"`
	Previous   string `json:"previous"`
	KeepBackup bool   `json:"keepBackup"`
	Began      string `json:"began"` // also names a kept backup, so it is unique
	dir        string
}

type journalEntry struct {
	Path    string `json:"path,omitempty"`
	Existed bool   `json:"existed,omitempty"`
	Link    string `json:"link,omitempty"` // the target, when the original was a symbolic link
	// IfMissing restores the original only when the path is gone, for a
	// snapshot taken against what something else, not upgrade, may remove.
	IfMissing bool `json:"ifMissing,omitempty"`
	Started   bool `json:"started,omitempty"`
	// PlaneImagesChanged is recorded with Started, while the originals and the
	// new Compose files are both still in place.
	PlaneImagesChanged bool `json:"planeImagesChanged,omitempty"`
}

// Upgrade moves an installation to the release it runs from.
func (a Installation) Upgrade(o UpgradeOptions) error {
	if !a.RunnerConfigured() {
		return errors.New("upgrade needs an installation with a connected repository; use switchyard init for a new one")
	}
	newBuild, err := readBuild(filepath.Join(o.Bundle, "BUILD.txt"))
	if err != nil {
		return fmt.Errorf("run upgrade from an extracted release bundle: %w", err)
	}
	unlock, err := a.lockUpgrade()
	if err != nil {
		return err
	}
	defer unlock()
	if err := a.recover(o.Wait); err != nil {
		return err
	}
	if err := a.checkMigrationBlock(); err != nil {
		return err
	}

	oldBuild, previous := a.installedRelease()
	if newBuild == oldBuild {
		fmt.Println("This installation already runs", short(newBuild)+".")
		return linkCLI(filepath.Join(o.Bundle, "switchyard"))
	}
	files, err := a.releaseFiles(o.Bundle)
	if err != nil {
		return err
	}
	baseline := a.baseline(oldBuild, previous)
	shipped := a.shippedFiles(oldBuild, previous)
	plan := func() ([]fileChange, error) {
		if err := a.checkUnit(previous, baseline); err != nil {
			return nil, err
		}
		return planChanges(a.Root, files, baseline, shipped)
	}
	// Refuse early; the plan that is applied is made once nothing runs.
	if _, err := plan(); err != nil {
		return err
	}
	projects, err := a.Projects()
	if err != nil {
		return err
	}
	// The journal begins before the idle check, so the runner stops straight
	// after it; a runner-side dispatch pause belongs in Yardmaster.
	t, err := a.begin(transaction{From: oldBuild, To: newBuild, Bundle: o.Bundle, Previous: previous, KeepBackup: o.KeepBackup})
	if err != nil {
		return err
	}
	crashPoint("begun")
	if err := a.waitUntilIdle(o.Wait); err != nil {
		_, closeErr := t.close(false)
		return errors.Join(err, closeErr)
	}
	changes, err := a.publish(t, plan, files, projects)
	if err != nil {
		if rollbackErr := a.rollback(t, o.Wait); rollbackErr != nil {
			return fmt.Errorf("upgrade failed (%v), and %w", err, rollbackErr)
		}
		return fmt.Errorf("upgrade failed, and %s was restored: %w", short(oldBuild), err)
	}

	// The commit point: from here the upgrade is only finished, never undone.
	if err := writeFile(filepath.Join(a.Root, "BUILD.txt"), files["BUILD.txt"].data, 0600); err != nil {
		if rollbackErr := a.rollback(t, o.Wait); rollbackErr != nil {
			return fmt.Errorf("could not record the release (%v), and %w", err, rollbackErr)
		}
		return fmt.Errorf("could not record the release, and %s was restored: %w", short(oldBuild), err)
	}
	crashPoint("committed")
	for _, change := range changes {
		switch {
		case change.linked:
			fmt.Printf("Left %s alone, since it is in a linked directory; install the release version yourself if you want it\n", change.path)
		case change.customized && change.retired:
			fmt.Printf("Kept customized %s, which this release no longer ships\n", change.path)
		case change.customized:
			fmt.Printf("Kept customized %s; the release version is %s.new\n", change.path, change.path)
		case change.retired:
			fmt.Printf("Removed %s, which this release no longer ships\n", change.path)
		}
	}
	if err := a.finish(t); err != nil {
		return err
	}
	fmt.Println("Upgraded to", short(newBuild)+".")
	return nil
}

// publish stops the services, backs up, and installs and starts the new release.
func (a Installation) publish(t transaction, plan func() ([]fileChange, error), files map[string]releaseFile, projects []Project) ([]fileChange, error) {
	// Stop the runner straight after the idle check so it cannot claim new work.
	// It stays disabled until up starts the new release.
	if err := a.stopRunner(); err != nil {
		return nil, fmt.Errorf("could not stop the runner: %w", err)
	}
	crashPoint("stopped")
	fmt.Println("Backing up before upgrading from", short(t.From), "to", short(t.To)+".")
	// The backup holds the current release's data, so it records that release.
	if err := a.Backup(filepath.Join(t.dir, "backup"), short(t.From)); err != nil {
		return nil, fmt.Errorf("backup failed: %w", err)
	}
	crashPoint("backed-up")
	// Files or the service may have changed while upgrade waited or backed up.
	changes, err := plan()
	if err != nil {
		return nil, err
	}
	for i, change := range changes {
		if err := a.apply(t, change); err != nil {
			return nil, err
		}
		if i == 0 {
			crashPoint("applying")
		}
	}
	manifest, err := releaseManifestData(t.To, files)
	if err != nil {
		return nil, err
	}
	if err := t.change(filepath.Join(a.Root, manifestName), func(path string) error { return writeFile(path, manifest, 0600) }); err != nil {
		return nil, err
	}
	crashPoint("applied")
	if err := a.refreshUnit(t); err != nil {
		return nil, err
	}
	crashPoint("unit")
	entries, err := t.entries()
	if err != nil {
		return nil, err
	}
	// up's checks and installing the (journaled) service start nothing, so the
	// release counts as started only after them, just before Plane's new images
	// can run.
	unitPath, unit, err := a.checkUp()
	if err != nil {
		return nil, err
	}
	if err := writeOnce(unitPath, []byte(unit), 0600); err != nil {
		return nil, err
	}
	if err := t.append(journalEntry{Started: true, PlaneImagesChanged: t.planeImagesChanged(entries, a.Root)}); err != nil {
		return nil, err
	}
	if err := a.start(); err != nil {
		return nil, err
	}
	crashPoint("started")
	if err := a.verifyUpgrade(t.Bundle, projects); err != nil {
		return nil, err
	}
	crashPoint("verified")
	return changes, nil
}

// apply publishes one planned change. The build record is left for the commit.
func (a Installation) apply(t transaction, change fileChange) error {
	switch {
	case change.path == "BUILD.txt", change.linked, change.retired && change.customized:
		return nil
	case change.retired:
		return t.change(filepath.Join(a.Root, change.path), func(path string) error {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return nil
		})
	}
	path := change.path
	if change.customized {
		path += ".new"
	}
	return t.change(filepath.Join(a.Root, path), func(path string) error {
		return writeFile(path, change.file.data, change.file.mode)
	})
}

func releaseManifestData(build string, files map[string]releaseFile) ([]byte, error) {
	manifest := releaseManifest{Build: build, Files: map[string]string{}}
	for path, file := range files {
		manifest.Files[path] = digest(file.data)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	return append(data, '\n'), err
}

// refreshUnit regenerates the runner's service from the new template. When the
// service is absent, up creates it, so it is journaled as absent and rollback
// removes it.
func (a Installation) refreshUnit(t transaction) error {
	path, err := config.UnitPath(a.Root)
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return t.change(path, func(string) error { return nil })
	} else if err != nil {
		return err
	}
	template, err := fs.ReadFile(a.Assets, "deploy/switchyard.service")
	if err != nil {
		return err
	}
	unit, _, err := a.runnerUnit(string(template), string(existing))
	if err != nil {
		return err
	}
	// A regular unit also needs the mode up gives it; a link is the operator's.
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if unit == string(existing) && (info.Mode()&fs.ModeSymlink != 0 || info.Mode().Perm() == 0600) {
		return nil
	}
	if err := t.change(path, func(path string) error { return writeFile(path, []byte(unit), 0600) }); err != nil {
		return err
	}
	return a.run("systemctl", "--user", "daemon-reload")
}

func (a Installation) verifyUpgrade(bundle string, projects []Project) error {
	if !sameFile(filepath.Join(a.Root, "bin/symphony"), filepath.Join(bundle, "bin/symphony")) {
		return errors.New("the installed runner does not match the release")
	}
	if err := a.Status(); err != nil {
		return err
	}
	after, err := a.Projects()
	if err != nil {
		return err
	}
	if !slices.Equal(after, projects) {
		return errors.New("project connections changed during the upgrade")
	}
	return nil
}

// recover finishes a committed upgrade or rolls back one that did not commit.
func (a Installation) recover(wait bool) error {
	t, err := a.openTransaction()
	if err != nil || t == nil {
		return err
	}
	if installed, _ := readBuild(filepath.Join(a.Root, "BUILD.txt")); installed == t.To {
		fmt.Println("Finishing the interrupted upgrade to", short(t.To)+".")
		return a.finish(*t)
	}
	fmt.Println("An upgrade to", short(t.To), "did not finish; restoring", short(t.From)+".")
	if err := a.rollback(*t, wait); err != nil {
		return err
	}
	fmt.Println("Restored", short(t.From)+".")
	return nil
}

// rollback restores every journaled path and starts the previous release again.
// It can be repeated until it succeeds.
func (a Installation) rollback(t transaction, wait bool) error {
	entries, err := t.entries()
	if err != nil {
		return err
	}
	// The new release's runner may already have claimed work, which is waited
	// for rather than cut short, so a runner that cannot report its work holds
	// rollback up too; stopping a stuck runner by hand lets it continue.
	if err := a.waitUntilIdle(wait); err != nil {
		return fmt.Errorf("%s is not restored yet: %w; if the runner is stuck, check switchyard logs runner, stop it with systemctl --user stop %s.service, then run upgrade again", short(t.From), err, config.Name(a.Root))
	}
	fmt.Println("Restoring the previous release.")
	// The runner must not run, or start at login, while its files change. systemd
	// stops it without any file the new release may have broken.
	if err := a.stopRunner(); err != nil {
		return fmt.Errorf("could not stop the runner to restore the previous release; nothing more was changed; run upgrade again: %w", err)
	}
	if err := t.restore(entries); err != nil {
		return fmt.Errorf("could not restore every file; run upgrade again: %w", err)
	}
	crashPoint("restored")
	if err := a.run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	// Plane stops with the restored scripts and Compose files; services only the
	// new release defined are orphans of those files.
	if err := a.compose("down", "--remove-orphans"); err != nil {
		return fmt.Errorf("could not stop Plane with the previous release's files; run upgrade again: %w", err)
	}
	migrated := slices.ContainsFunc(entries, func(e journalEntry) bool { return e.Started && e.PlaneImagesChanged })
	unit, err := config.UnitPath(a.Root)
	if err != nil {
		return err
	}
	if migrated {
		// The runner stays disabled, so it cannot start against a stopped Plane,
		// and the block below outlives the journal so up refuses too.
		// The block names where close keeps the snapshot; a retry after close
		// moved it finds it there.
		backup := snapshot(t.keptBackup())
		if !exists(t.keptBackup()) {
			backup = filepath.Join(t.keptBackup(), filepath.Base(snapshot(filepath.Join(t.dir, "backup"))))
		}
		block := fmt.Sprintf("An upgrade to %s started Plane with new images and was rolled back, so Plane's data may be migrated.\nRestore the backup in %s as in docs/backup.md, or delete this file if Plane's data was not migrated.\n", short(t.To), backup)
		if err := writeFile(a.migrationBlock(), []byte(block), 0600); err != nil {
			return err
		}
		kept, err := t.close(true)
		if err != nil {
			return err
		}
		return fmt.Errorf("the previous release's files are restored but its services were left stopped: the new release changed Plane's images and may have migrated its data; restore the backup in %s as in docs/backup.md", kept)
	}
	// Every journaled file, the service included, is back as it was before the
	// upgrade, so it starts as it is. Only when there was no service does up
	// create one, from the restored template.
	start := a.start
	if !exists(unit) {
		previous := a
		previous.Assets = os.DirFS(a.Root)
		start = previous.Up
	}
	if err := start(); err != nil {
		return fmt.Errorf("the previous release's files are restored but it did not start (%v); fix that and run upgrade again; the backup is in %s", err, snapshot(filepath.Join(t.dir, "backup")))
	}
	// Plane's data was not migrated, so the backup is kept only when asked for.
	kept, err := t.close(t.KeepBackup)
	if err != nil {
		return err
	}
	if kept != "" {
		fmt.Println("The backup is kept in", kept)
	}
	return nil
}

// finish completes a committed upgrade. It can be repeated until it succeeds.
func (a Installation) finish(t transaction) error {
	if err := linkCLI(filepath.Join(t.Bundle, "switchyard")); err != nil {
		return fmt.Errorf("the new release is running but %w; run upgrade again", err)
	}
	crashPoint("linked")
	// Pruning only removes, so it is repeated until the journal is closed.
	pruneReleases(t.Bundle, t.Previous)
	kept, err := t.close(t.KeepBackup)
	if err != nil {
		return err
	}
	if kept != "" {
		fmt.Println("Backup kept in", kept)
	}
	return nil
}

// begin writes the journal header before anything changes, together with a
// snapshot of a linked service: systemd removes the link when the runner is
// disabled, which recovery can do too, so the link must be journaled as soon
// as the journal exists. Upgrade's own changes to the service are journaled
// when it makes them; anything else, such as an operator removing a regular
// unit meanwhile, is left as it is.
func (a Installation) begin(t transaction) (transaction, error) {
	t.dir = filepath.Join(a.Root, "work", "upgrade")
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return t, err
	}
	t.Began = time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(suffix)
	header, err := json.Marshal(t)
	if err != nil {
		return t, err
	}
	journal := append(header, '\n')
	unit, err := config.UnitPath(a.Root)
	if err != nil {
		return t, err
	}
	if info, err := os.Lstat(unit); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		link, err := os.Readlink(unit)
		if err != nil {
			return t, err
		}
		entry, err := json.Marshal(journalEntry{Path: unit, Existed: true, Link: link, IfMissing: true})
		if err != nil {
			return t, err
		}
		journal = append(append(journal, entry...), '\n')
	}
	return t, writeFile(filepath.Join(t.dir, "journal"), journal, 0600)
}

// openTransaction reads an unfinished transaction. A work directory without a
// journal holds nothing that is still needed, so it is removed.
func (a Installation) openTransaction() (*transaction, error) {
	dir := filepath.Join(a.Root, "work", "upgrade")
	file, err := os.Open(filepath.Join(dir, "journal"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.RemoveAll(dir)
	} else if err != nil {
		return nil, err
	}
	defer file.Close()
	line, err := bufio.NewReader(file).ReadBytes('\n')
	var t transaction
	if err != nil || json.Unmarshal(line, &t) != nil || t.To == "" {
		return nil, fmt.Errorf("the upgrade record %s is unreadable; restore from the backup beside it as in docs/backup.md", file.Name())
	}
	t.dir = dir
	return &t, nil
}

func (t transaction) entries() ([]journalEntry, error) {
	data, err := os.ReadFile(filepath.Join(t.dir, "journal"))
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var entries []journalEntry
	for _, line := range lines[1:] {
		var entry journalEntry
		// A torn last line was written before its change began.
		if json.Unmarshal([]byte(line), &entry) == nil && (entry.Path != "" || entry.Started) {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (t transaction) append(entry journalEntry) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(t.dir, "journal"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = file.Write(append(line, '\n'))
	if err == nil {
		err = file.Sync()
	}
	return errors.Join(err, file.Close())
}

func (t transaction) saved(path string) string {
	return filepath.Join(t.dir, "previous", path)
}

// change journals path and saves its original before applying modify to it. A
// symbolic link is journaled with its target rather than saved.
func (t transaction) change(path string, modify func(string) error) error {
	info, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	entry := journalEntry{Path: path, Existed: err == nil}
	if info != nil && info.Mode()&fs.ModeSymlink != 0 {
		if entry.Link, err = os.Readlink(path); err != nil {
			return err
		}
	}
	if err := t.append(entry); err != nil {
		return err
	}
	if info != nil && entry.Link == "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := writeFile(t.saved(path), data, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return modify(path)
}

// restore undoes the journaled changes, newest first. A path without a saved
// original had not been changed yet.
func (t transaction) restore(entries []journalEntry) error {
	var errs []error
	for _, entry := range slices.Backward(entries) {
		if _, err := os.Lstat(entry.Path); entry.IfMissing && err == nil {
			continue
		}
		switch {
		case entry.Started:
		case entry.Link != "":
			if err := os.Remove(entry.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
				continue
			}
			errs = append(errs, os.Symlink(entry.Link, entry.Path))
		case !entry.Existed:
			if err := os.Remove(entry.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		default:
			info, err := os.Stat(t.saved(entry.Path))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			var data []byte
			if err == nil {
				data, err = os.ReadFile(t.saved(entry.Path))
			}
			if err == nil {
				err = writeFile(entry.Path, data, info.Mode().Perm())
			}
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// planeImagesChanged reports whether the journaled Compose files name other
// images than their originals, so starting the new release may have migrated
// Plane's data.
func (t transaction) planeImagesChanged(entries []journalEntry, root string) bool {
	for _, entry := range entries {
		if entry.Path != filepath.Join(root, "deploy/docker-compose.yml") && entry.Path != filepath.Join(root, "deploy/compose.override.yml") {
			continue
		}
		before, _ := os.ReadFile(t.saved(entry.Path))
		after, _ := os.ReadFile(entry.Path)
		if !slices.Equal(composeImages(before), composeImages(after)) {
			return true
		}
	}
	return false
}

var composeImage = regexp.MustCompile(`(?m)^\s*image:\s*(\S+)`)

func composeImages(data []byte) []string {
	images := map[string]bool{}
	for _, match := range composeImage.FindAllSubmatch(data, -1) {
		images[string(match[1])] = true
	}
	return slices.Sorted(maps.Keys(images))
}

// keptBackup is where close keeps the backup directory.
func (t transaction) keptBackup() string {
	return filepath.Join(filepath.Dir(t.dir), "upgrade-backup-"+t.Began)
}

// close ends the transaction: the backup is moved beside it when kept, then
// the journal goes, which is the moment the transaction is over. It returns the
// kept backup snapshot, ready for restore.
func (t transaction) close(keepBackup bool) (string, error) {
	kept := ""
	if keepBackup {
		kept = t.keptBackup()
		if err := os.Rename(filepath.Join(t.dir, "backup"), kept); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		crashPoint("backup moved")
		if !exists(kept) {
			kept = ""
		} else {
			kept = snapshot(kept)
		}
	}
	if err := os.Remove(filepath.Join(t.dir, "journal")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return kept, os.RemoveAll(t.dir)
}

// migrationBlock is present while Plane's data may have been migrated by a
// rolled-back release; up and upgrade refuse until it is resolved.
func (a Installation) migrationBlock() string {
	return filepath.Join(a.Root, "work", "upgrade-plane-migrated")
}

func (a Installation) checkMigrationBlock() error {
	data, err := os.ReadFile(a.migrationBlock())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("cannot read %s, so Plane's data may be migrated: %w", a.migrationBlock(), err)
	}
	return fmt.Errorf("%s(%s)", data, a.migrationBlock())
}

// snapshot names the snapshot scripts/backup created in dir, which is what a
// restore takes, or dir itself when there is not exactly one.
func snapshot(dir string) string {
	snapshots, _ := filepath.Glob(filepath.Join(dir, "switchyard-*"))
	if len(snapshots) != 1 {
		return dir
	}
	return snapshots[0]
}

// Exclusive keeps other commands that start, stop or back up the services from
// running alongside an upgrade or over one that did not finish. The returned
// function releases it.
func (a Installation) Exclusive() (func(), error) {
	if !exists(filepath.Join(a.Root, "config.json")) {
		return func() {}, nil
	}
	unlock, err := a.lockUpgrade()
	if err != nil {
		return nil, err
	}
	if exists(filepath.Join(a.Root, "work", "upgrade", "journal")) {
		unlock()
		return nil, errors.New("an upgrade of this installation did not finish; run switchyard upgrade from either release to finish or undo it first")
	}
	return unlock, nil
}

// lockUpgrade holds an installation-wide lock so only one upgrade inspects or
// changes recovery state at a time. The kernel releases it if upgrade exits.
func (a Installation) lockUpgrade() (func(), error) {
	path := filepath.Join(a.Root, "work", "upgrade.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("another switchyard upgrade is running for this installation; wait for it to finish")
	}
	return func() { file.Close() }, nil
}

// stopRunner stops and disables the runner, so a new login cannot start it
// while its files change; starting a release enables it again. Without a unit
// file it is stopped by name, since a loaded service keeps running after its
// file is removed. Stopping a unit that is not loaded fails, so that failure
// is accepted only when systemd confirms the runner is stopped.
func (a Installation) stopRunner() error {
	service := config.Name(a.Root) + ".service"
	unit, err := config.UnitPath(a.Root)
	if err != nil {
		return err
	}
	if exists(unit) {
		return a.run("systemctl", "--user", "disable", "--now", service)
	}
	stopErr := a.run("systemctl", "--user", "stop", service)
	if stopErr == nil {
		return nil
	}
	if state, err := a.runnerState(); err != nil {
		return errors.Join(stopErr, err)
	} else if !stoppedState(state) {
		return stopErr
	}
	return nil
}

// runnerState asks systemd for the runner's ActiveState. An error means systemd
// could not be asked, which says nothing about whether the runner runs.
func (a Installation) runnerState() (string, error) {
	out, err := a.command("systemctl", "--user", "show", "--property=ActiveState", "--value", config.Name(a.Root)+".service").Output()
	state := strings.TrimSpace(string(out))
	if err != nil || state == "" {
		return "", fmt.Errorf("systemd could not report the runner's state: %v", err)
	}
	return state, nil
}

func stoppedState(state string) bool { return state == "inactive" || state == "failed" }

// waitUntilIdle refuses, or with wait polls, while the runner has running or
// retrying work, or while an active runner cannot report its work.
func (a Installation) waitUntilIdle(wait bool) error {
	for {
		busy, err := a.activeWork()
		if err == nil && busy == "" {
			return nil
		}
		if !wait {
			if err != nil {
				return err
			}
			return fmt.Errorf("the runner has %s; rerun when it is idle, or add --wait", busy)
		}
		if err != nil {
			busy = err.Error()
		}
		fmt.Println("Waiting for the runner to become idle:", busy)
		time.Sleep(idleRetry)
	}
}

func (a Installation) activeWork() (string, error) {
	if service, err := a.runnerState(); err != nil {
		return "", fmt.Errorf("the runner cannot be confirmed idle: %w", err)
	} else if stoppedState(service) {
		return "", nil
	}
	client := http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/v1/state", a.Settings.RunnerPort))
	if err != nil {
		return "", fmt.Errorf("the runner is active but its state is unavailable, so it cannot be confirmed idle: %w", err)
	}
	defer response.Body.Close()
	var state struct {
		Counts *struct {
			Running  *int `json:"running"`
			Retrying *int `json:"retrying"`
		} `json:"counts"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&state) != nil ||
		state.Counts == nil || state.Counts.Running == nil || state.Counts.Retrying == nil {
		return "", errors.New("the runner returned an unreadable state, so it cannot be confirmed idle")
	}
	if *state.Counts.Running == 0 && *state.Counts.Retrying == 0 {
		return "", nil
	}
	return fmt.Sprintf("%d running and %d retrying tasks", *state.Counts.Running, *state.Counts.Retrying), nil
}
