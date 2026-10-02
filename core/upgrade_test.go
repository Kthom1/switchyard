package core

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Kthom1/switchyard/config"
)

const (
	oldBuild = "1111111111111111111111111111111111111111"
	newBuild = "2222222222222222222222222222222222222222"
)

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// applyPlan applies changes through a transaction, as an upgrade does.
func applyPlan(t *testing.T, a Installation, changes []fileChange) transaction {
	t.Helper()
	tx, err := a.begin(transaction{From: oldBuild, To: newBuild})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range changes {
		if err := a.apply(tx, change); err != nil {
			t.Fatal(err)
		}
	}
	return tx
}

func restorePlan(t *testing.T, tx transaction) {
	t.Helper()
	entries, err := tx.entries()
	if err == nil {
		err = tx.restore(entries)
	}
	if err != nil {
		t.Fatal(err)
	}
}

// recordForTest leaves what a committed upgrade to build leaves.
func recordForTest(t *testing.T, root, build string, files map[string]releaseFile) {
	t.Helper()
	manifest, err := releaseManifestData(build, files)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, manifestName), string(manifest))
	writeTestFile(t, filepath.Join(root, "BUILD.txt"), "Switchyard "+build+"\n")
}

func TestUpgradeReplacesUntouchedFilesAndKeepsCustomizedOnes(t *testing.T) {
	root, previous, bundle := t.TempDir(), t.TempDir(), t.TempDir()
	for path, contents := range map[string]string{
		"scripts/run":         "old run",
		"docs/backup.md":      "customized guide",
		"deploy/Caddyfile":    "same",
		"bin/symphony":        "patched runner",
		"BUILD.txt":           "Switchyard " + oldBuild + "\n",
		"docs/retired.md":     "left alone",
		"licenses/NOTICE.txt": "old notice",
	} {
		writeTestFile(t, filepath.Join(root, path), contents)
	}
	writeTestFile(t, filepath.Join(previous, "scripts/run"), "old run")
	writeTestFile(t, filepath.Join(previous, "docs/backup.md"), "release guide")
	writeTestFile(t, filepath.Join(previous, "BUILD.txt"), "Switchyard "+oldBuild+"\n")
	writeTestFile(t, filepath.Join(bundle, "bin/symphony"), "new runner")
	writeTestFile(t, filepath.Join(bundle, "BUILD.txt"), "Switchyard "+newBuild+"\n")
	writeTestFile(t, filepath.Join(bundle, "licenses/NOTICE.txt"), "new notice")
	writeTestFile(t, filepath.Join(bundle, "docs/backup.md"), "not an installed asset")
	a := Installation{Root: root, Assets: fstest.MapFS{
		"scripts/run":         {Data: []byte("new run")},
		"docs/backup.md":      {Data: []byte("new guide")},
		"deploy/Caddyfile":    {Data: []byte("same")},
		"scripts/added":       {Data: []byte("added")},
		"docs/recommended.md": {Data: []byte("recommended")},
	}}
	files, err := a.releaseFiles(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if string(files["docs/backup.md"].data) != "new guide" {
		t.Fatal("installed assets come from the CLI, not the bundle's source tree")
	}
	changes, err := planChanges(root, files, a.baseline(oldBuild, previous), nil)
	if err != nil {
		t.Fatal(err)
	}
	planned := map[string]bool{}
	for _, change := range changes {
		planned[change.path] = change.customized
	}
	want := map[string]bool{"scripts/run": false, "docs/backup.md": true, "bin/symphony": false, "BUILD.txt": false,
		"licenses/NOTICE.txt": false, "scripts/added": false, "docs/recommended.md": false}
	if fmt.Sprint(planned) != fmt.Sprint(want) {
		t.Fatalf("planned %v, want %v", planned, want)
	}

	tx := applyPlan(t, a, changes)
	for path, contents := range map[string]string{
		"scripts/run": "new run", "docs/backup.md": "customized guide", "docs/backup.md.new": "new guide",
		"bin/symphony": "new runner", "BUILD.txt": "Switchyard " + oldBuild + "\n", "scripts/added": "added",
		"docs/retired.md": "left alone",
	} {
		if got := readTestFile(t, filepath.Join(root, path)); got != contents {
			t.Fatalf("%s = %q, want %q", path, got, contents)
		}
	}
	if info, err := os.Stat(filepath.Join(root, "scripts/run")); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("scripts must stay executable: %v %v", info, err)
	}
	restorePlan(t, tx)
	for path, contents := range map[string]string{"scripts/run": "old run", "bin/symphony": "patched runner"} {
		if got := readTestFile(t, filepath.Join(root, path)); got != contents {
			t.Fatalf("rollback left %s = %q, want %q", path, got, contents)
		}
	}
	for _, path := range []string{"scripts/added", "docs/backup.md.new"} {
		if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
			t.Fatalf("rollback must remove added %s: %v", path, err)
		}
	}

	// After a committed upgrade the manifest decides, without the previous bundle.
	applyPlan(t, a, changes)
	recordForTest(t, root, newBuild, files)
	again, err := planChanges(root, files, a.baseline(newBuild, ""), a.shippedFiles(newBuild, ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].path != "docs/backup.md" || !again[0].customized {
		t.Fatalf("a still-customized file stays customized and nothing else changes: %+v", again)
	}
}

func TestUpgradeWithoutAnyBaselineTreatsChangedFilesAsCustomized(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "scripts/run"), "unknown origin")
	changes, err := planChanges(root, map[string]releaseFile{"scripts/run": {[]byte("release"), 0700}}, Installation{Root: root}.baseline("", ""), nil)
	if err != nil || len(changes) != 1 || !changes[0].customized {
		t.Fatalf("unknown files must be kept: %+v %v", changes, err)
	}
}

func TestUpgradeRetiresUnchangedFilesTheReleaseNoLongerShips(t *testing.T) {
	root := t.TempDir()
	a := Installation{Root: root}
	writeTestFile(t, filepath.Join(root, "docs/old.md"), "old")
	writeTestFile(t, filepath.Join(root, "docs/mine.md"), "edited")
	writeTestFile(t, filepath.Join(root, "scripts/run"), "run")
	if err := os.Chmod(filepath.Join(root, "scripts/run"), 0600); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "scripts/open"), "open")
	if err := os.Chmod(filepath.Join(root, "scripts/open"), 0777); err != nil {
		t.Fatal(err)
	}
	recordForTest(t, root, oldBuild, map[string]releaseFile{
		"docs/old.md": {data: []byte("old")}, "docs/mine.md": {data: []byte("shipped")},
		"scripts/run": {data: []byte("run")}, "scripts/open": {data: []byte("open")}, "BUILD.txt": {data: []byte("Switchyard " + oldBuild + "\n")},
	})
	files := map[string]releaseFile{"scripts/run": {[]byte("run"), 0700}, "scripts/open": {[]byte("open"), 0700}, "BUILD.txt": {[]byte("Switchyard " + newBuild + "\n"), 0600}}
	changes, err := planChanges(root, files, a.baseline(oldBuild, ""), a.shippedFiles(oldBuild, ""))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, change := range changes {
		got[change.path] = fmt.Sprintf("retired=%v customized=%v", change.retired, change.customized)
	}
	want := map[string]string{
		"docs/old.md":  "retired=true customized=false",
		"docs/mine.md": "retired=true customized=true",
		"scripts/run":  "retired=false customized=false",
		"scripts/open": "retired=false customized=false",
		"BUILD.txt":    "retired=false customized=false",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("planned %v, want %v", got, want)
	}
	tx := applyPlan(t, a, changes)
	if _, err := os.Stat(filepath.Join(root, "docs/old.md")); !os.IsNotExist(err) {
		t.Fatal("an unchanged file the release dropped is removed")
	}
	if readTestFile(t, filepath.Join(root, "docs/mine.md")) != "edited" {
		t.Fatal("a customized file the release dropped is kept")
	}
	if info, err := os.Stat(filepath.Join(root, "scripts/run")); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("an identical script regains its executable bit: %v %v", info, err)
	}
	if info, err := os.Stat(filepath.Join(root, "scripts/open")); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("an identical script loses permissions the release does not grant: %v %v", info, err)
	}
	restorePlan(t, tx)
	if readTestFile(t, filepath.Join(root, "docs/old.md")) != "old" {
		t.Fatal("rollback restores a removed file")
	}
	if info, err := os.Stat(filepath.Join(root, "scripts/run")); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("rollback restores the previous mode: %v %v", info, err)
	}
}

func TestShippedFilesComeFromThePreviousBundleWithoutAManifest(t *testing.T) {
	previous := t.TempDir()
	writeTestFile(t, filepath.Join(previous, "assets.go"), "package main\n\n//go:embed docs/old.md scripts/run\n//go:embed deploy/*.yml\nvar assets embed.FS\n")
	for _, path := range []string{"docs/old.md", "docs/source-only.md", "scripts/run", "deploy/compose.yml", "bin/symphony", "BUILD.txt", "licenses/NOTICE", "core/upgrade.go"} {
		writeTestFile(t, filepath.Join(previous, path), path)
	}
	got := Installation{Root: t.TempDir()}.shippedFiles(oldBuild, previous)
	want := []string{"BUILD.txt", "bin/symphony", "deploy/compose.yml", "docs/old.md", "licenses/NOTICE", "scripts/run"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("shipped %v, want %v", got, want)
	}
}

func TestUpgradeRefusesOrWaitsWhileTheRunnerIsBusy(t *testing.T) {
	bin := t.TempDir()
	active := filepath.Join(bin, "active")
	writeTestFile(t, filepath.Join(bin, "systemctl"), "#!/bin/sh\nif [ -e "+active+" ]; then echo active; else echo inactive; fi\n")
	if err := os.Chmod(filepath.Join(bin, "systemctl"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	var calls atomic.Int32
	idleAfter := int32(1 << 30)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/state" {
			http.NotFound(w, r)
			return
		}
		if calls.Add(1) > idleAfter {
			fmt.Fprint(w, `{"counts":{"running":0,"retrying":0,"blocked":2}}`)
			return
		}
		fmt.Fprint(w, `{"counts":{"running":1,"retrying":2}}`)
	}))
	defer server.Close()
	port := server.Listener.Addr().(*net.TCPAddr).Port
	a := Installation{Root: t.TempDir(), Settings: config.Settings{RunnerPort: port}}

	if err := a.waitUntilIdle(false); err != nil {
		t.Fatalf("a stopped runner is idle: %v", err)
	}
	writeTestFile(t, filepath.Join(bin, "systemctl"), "#!/bin/sh\nexit 1\n")
	if err := a.waitUntilIdle(false); err == nil || !strings.Contains(err.Error(), "cannot be confirmed idle") {
		t.Fatalf("a runner systemd cannot report on is not idle: %v", err)
	}
	writeTestFile(t, filepath.Join(bin, "systemctl"), "#!/bin/sh\nif [ -e "+active+" ]; then echo active; else echo inactive; fi\n")
	writeTestFile(t, active, "")
	if err := a.waitUntilIdle(false); err == nil || !strings.Contains(err.Error(), "1 running and 2 retrying") {
		t.Fatalf("busy runner must be refused: %v", err)
	}
	idleRetry = time.Millisecond
	t.Cleanup(func() { idleRetry = 30 * time.Second })
	calls.Store(0)
	idleAfter = 2
	if err := a.waitUntilIdle(true); err != nil || calls.Load() != 3 {
		t.Fatalf("wait must poll until idle: %v after %d calls", err, calls.Load())
	}

	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"counts":{}}`) })
	if err := a.waitUntilIdle(false); err == nil || !strings.Contains(err.Error(), "cannot be confirmed idle") {
		t.Fatalf("an unreadable state must not count as idle: %v", err)
	}
	server.Close()
	if err := a.waitUntilIdle(false); err == nil || !strings.Contains(err.Error(), "cannot be confirmed idle") {
		t.Fatalf("an unreachable active runner must not count as idle: %v", err)
	}
}

func TestUpgradeLinksTheCLIAndPrunesOlderReleasesAndRuntimes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	releases := filepath.Join(home, ".local/share/switchyard/releases")
	bundle := func(name, build string) string {
		dir := filepath.Join(releases, name, "switchyard")
		writeTestFile(t, filepath.Join(dir, "BUILD.txt"), "Switchyard "+build+"\n")
		writeTestFile(t, filepath.Join(dir, "switchyard"), "cli")
		return dir
	}
	oldest := bundle("v0.1.0", "3333333333333333333333333333333333333333")
	previous := bundle("v0.2.0", oldBuild)
	current := bundle("v0.3.0", newBuild)
	writeTestFile(t, filepath.Join(releases, "notes", "keep.txt"), "not a release")
	link := filepath.Join(home, ".local/bin/switchyard")
	if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(previous, "switchyard"), link); err != nil {
		t.Fatal(err)
	}
	install := Installation{Root: t.TempDir()}
	writeTestFile(t, filepath.Join(install.Root, "BUILD.txt"), "Switchyard "+oldBuild+"\n")
	if build, found := install.installedRelease(); build != oldBuild || found != previous {
		t.Fatalf("installed release = %q %q, want %q %q", build, found, oldBuild, previous)
	}
	writeTestFile(t, filepath.Join(install.Root, "BUILD.txt"), "Switchyard "+newBuild+"\n")
	if build, found := install.installedRelease(); build != newBuild || found != "" {
		t.Fatal("a link to a different build is not the installed release")
	}
	// Without a build record, an identical runner identifies the linked release.
	if err := os.Remove(filepath.Join(install.Root, "BUILD.txt")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(previous, "bin/symphony"), "old runner")
	writeTestFile(t, filepath.Join(install.Root, "bin/symphony"), "old runner")
	if build, found := install.installedRelease(); build != oldBuild || found != previous {
		t.Fatalf("matching runner: %q %q", build, found)
	}
	writeTestFile(t, filepath.Join(install.Root, "bin/symphony"), "some other runner")
	if build, found := install.installedRelease(); build != "" || found != "" {
		t.Fatalf("a different runner identifies nothing: %q %q", build, found)
	}

	linkCLI(filepath.Join(current, "switchyard"))
	if target, err := os.Readlink(link); err != nil || target != filepath.Join(current, "switchyard") {
		t.Fatalf("link = %q, %v", target, err)
	}
	newer := bundle("v0.4.0", "4444444444444444444444444444444444444444")
	unversioned := bundle("4cbe9f1", "5555555555555555555555555555555555555555")
	cache := filepath.Join(home, "data/.burrito")
	runtimes := map[string]bool{
		"symphony_erts-16.4_0.1.0+" + oldBuild:                                true,
		"yardmaster_erts-16.4_0.1.0+" + newBuild:                              true,
		"yardmaster_erts-16.4_0.1.0+4444444444444444444444444444444444444444": true,
		"symphony_erts-16.4_0.1.0+5555555555555555555555555555555555555555":   true,
		"symphony_erts-16.4_0.1.0+3333333333333333333333333333333333333333":   false,
		"other_erts-16.4_0.1.0+3333333333333333333333333333333333333333":      true,
	}
	for name := range runtimes {
		writeTestFile(t, filepath.Join(cache, name, "marker"), "")
	}
	removed := pruneReleases(current, previous)
	if fmt.Sprint(removed) != "[3333333333333333333333333333333333333333]" {
		t.Fatalf("removed builds = %v", removed)
	}
	for dir, want := range map[string]bool{oldest: false, previous: true, current: true, newer: true, unversioned: true, filepath.Join(releases, "notes"): true} {
		if _, err := os.Stat(dir); (err == nil) != want {
			t.Fatalf("%s present=%v, want %v", dir, err == nil, want)
		}
	}
	pruneRuntimes(removed...)
	for name, want := range runtimes {
		if _, err := os.Stat(filepath.Join(cache, name)); (err == nil) != want {
			t.Fatalf("%s present=%v, want %v", name, err == nil, want)
		}
	}
	if pruneReleases(current, unversioned) != nil {
		t.Fatal("without a versioned previous release nothing is pruned")
	}
	older := bundle("v0.0.9", "6666666666666666666666666666666666666666")
	pruneReleases(older, current)
	if _, err := os.Stat(older); err != nil {
		t.Fatal("the release being installed is never pruned, even when it is older")
	}

	plain := filepath.Join(home, "plain")
	writeTestFile(t, plain, "a user's own command")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(plain, link); err != nil {
		t.Fatal(err)
	}
	linkCLI(filepath.Join(current, "switchyard"))
	if readTestFile(t, link) != "a user's own command" {
		t.Fatal("a command that is not a release link must be left alone")
	}
}

func TestPruningKeepsARuntimeAnotherRunnerStillRuns(t *testing.T) {
	// The data directory is reached through a link, as /proc never reports.
	data := filepath.Join(t.TempDir(), "data")
	if err := os.Symlink(t.TempDir(), data); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", data)
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep is not available")
	}
	cache := filepath.Join(os.Getenv("XDG_DATA_HOME"), ".burrito")
	running := filepath.Join(cache, "yardmaster_erts-16.4_0.1.0+"+oldBuild)
	idle := filepath.Join(cache, "yardmaster_erts-16.4_0.1.0+"+newBuild)
	binary, err := os.ReadFile(sleep)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{running, idle} {
		if err := os.MkdirAll(filepath.Join(dir, "bin"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bin/sleep"), binary, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Another installation's runner runs from the first runtime.
	process := exec.Command(filepath.Join(running, "bin/sleep"), "30")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = process.Process.Kill(); _ = process.Wait() })
	pruneRuntimes(oldBuild, newBuild)
	if _, err := os.Stat(running); err != nil {
		t.Fatal("a runtime a running process uses is kept")
	}
	if _, err := os.Stat(idle); !os.IsNotExist(err) {
		t.Fatal("an unused runtime is removed")
	}
}

func TestFinishingAgainPrunesWhatAnInterruptionLeft(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	releases := filepath.Join(home, ".local/share/switchyard/releases")
	bundle := func(name, build string) string {
		dir := filepath.Join(releases, name, "switchyard")
		writeTestFile(t, filepath.Join(dir, "BUILD.txt"), "Switchyard "+build+"\n")
		writeTestFile(t, filepath.Join(dir, "switchyard"), "cli")
		return dir
	}
	oldest := bundle("v0.1.0", "3333333333333333333333333333333333333333")
	previous := bundle("v0.2.0", oldBuild)
	current := bundle("v0.3.0", newBuild)
	a := Installation{Root: t.TempDir()}
	writeTestFile(t, filepath.Join(a.Root, "BUILD.txt"), "Switchyard "+newBuild+"\n")
	tx, err := a.begin(transaction{From: oldBuild, To: newBuild, Bundle: current, Previous: previous})
	if err != nil {
		t.Fatal(err)
	}
	crashPoint = func(step string) {
		if step == "linked" {
			panic(simulatedCrash(step))
		}
	}
	func() {
		defer func() { crashPoint = func(string) {}; _ = recover() }()
		_ = a.finish(tx)
	}()
	if _, err := os.Stat(oldest); err != nil {
		t.Fatal("the interruption came before pruning")
	}
	if err := a.recover(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldest); !os.IsNotExist(err) {
		t.Fatal("finishing again prunes the older release")
	}
	if _, err := os.Stat(filepath.Join(a.Root, "work/upgrade")); !os.IsNotExist(err) {
		t.Fatal("the finished transaction is closed")
	}
}

func TestPruningKeepsAReleaseWhoseRuntimeIsStillRunning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep is not available")
	}
	releases := filepath.Join(home, ".local/share/switchyard/releases")
	bundle := func(name, build string) string {
		dir := filepath.Join(releases, name, "switchyard")
		writeTestFile(t, filepath.Join(dir, "BUILD.txt"), "Switchyard "+build+"\n")
		return dir
	}
	busy := "3333333333333333333333333333333333333333"
	oldest := bundle("v0.1.0", busy)
	previous := bundle("v0.2.0", oldBuild)
	current := bundle("v0.3.0", newBuild)
	runtime := filepath.Join(home, "data/.burrito/yardmaster_erts-16.4_0.1.0+"+busy)
	binary, err := os.ReadFile(sleep)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(runtime, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtime, "bin/sleep"), binary, 0700); err != nil {
		t.Fatal(err)
	}
	process := exec.Command(filepath.Join(runtime, "bin/sleep"), "30")
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Process.Kill(); _ = process.Wait() }()
	pruneReleases(current, previous)
	if _, err := os.Stat(filepath.Join(oldest, "BUILD.txt")); err != nil {
		t.Fatal("the release stays while its runtime is in use, so a later prune can find it")
	}
	_ = process.Process.Kill()
	_ = process.Wait()
	pruneReleases(current, previous)
	for _, gone := range []string{oldest, runtime} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Fatalf("a later prune removes %s", gone)
		}
	}
}

func TestServiceCommandsWaitForUpgrades(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	writeTestFile(t, filepath.Join(f.a.Root, "config.json"), "{}")
	unlock, err := f.a.lockUpgrade()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.Exclusive(); err == nil || !strings.Contains(err.Error(), "another switchyard upgrade is running") {
		t.Fatalf("a running upgrade blocks other service commands: %v", err)
	}
	unlock()
	crashUpgrade(t, f.a, UpgradeOptions{Bundle: f.bundle}, "applied")
	if _, err := f.a.Exclusive(); err == nil || !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("an unfinished upgrade blocks other service commands: %v", err)
	}
	if err := f.old.Upgrade(UpgradeOptions{Bundle: f.previous}); err != nil {
		t.Fatal(err)
	}
	release, err := f.a.Exclusive()
	if err != nil {
		t.Fatalf("other service commands run once the upgrade is resolved: %v", err)
	}
	release()
}

func TestReadBuildAcceptsOnlySwitchyardBuildRecords(t *testing.T) {
	dir := t.TempDir()
	for contents, ok := range map[string]bool{
		"Switchyard " + newBuild + "\nRunner release x\n": true,
		"Yardmaster " + newBuild + "\n":                   false,
		"Switchyard 1234\n":                               false,
	} {
		writeTestFile(t, filepath.Join(dir, "BUILD.txt"), contents)
		if build, err := readBuild(filepath.Join(dir, "BUILD.txt")); (err == nil) != ok || (ok && build != newBuild) {
			t.Fatalf("readBuild(%q) = %q, %v", contents, build, err)
		}
	}
}

// upgradeFixture is an installed old release plus an extracted new bundle, with
// stub services: systemd, scripts, the board and the runner dashboard.
type upgradeFixture struct {
	a        Installation // the new release's CLI
	old      Installation // the installed release's CLI
	bundle   string
	previous string
	calls    string
	running  *atomic.Int32
	busy     string       // while this file exists the runner reports a running task
	broken   *atomic.Bool // the runner reports an unreadable state
}

func newUpgradeFixture(t *testing.T, newCodexRunner string) upgradeFixture {
	t.Helper()
	home, bin := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	calls := filepath.Join(t.TempDir(), "calls")
	stub := "#!/bin/sh\n[ \"$2\" != show ] || { echo active; exit 0; }\nprintf '%s %s\\n' \"${0##*/}\" \"$*\" >> " + calls + "\n"
	for _, name := range []string{"systemctl", "codex"} {
		writeTestFile(t, filepath.Join(bin, name), stub)
		if err := os.Chmod(filepath.Join(bin, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	running, broken, busy := &atomic.Int32{}, &atomic.Bool{}, filepath.Join(t.TempDir(), "busy")
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{}`) }))
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if broken.Load() {
			fmt.Fprint(w, `{}`)
			return
		}
		count := running.Load()
		if exists(busy) {
			count++
		}
		fmt.Fprintf(w, `{"counts":{"running":%d,"retrying":0}}`, count)
	}))
	t.Cleanup(board.Close)
	t.Cleanup(runner.Close)

	root := t.TempDir()
	script := func(body string) string { return "#!/bin/sh\n" + body + "\n" }
	installed := map[string]string{
		".env":                      "SOURCE_REPO_URL=https://github.com/example/project\n",
		"WORKFLOW.md":               "---\ntracker:\n  kind: plane\n  provider:\n    workspace: example\n    projects:\n      - project_id: 12345678-1234-1234-1234-123456789abc\n        project_identifier: APP\n        repo: https://github.com/example/project\n---\nPrompt\n",
		"BUILD.txt":                 "Switchyard " + oldBuild + "\n",
		"bin/symphony":              "old runner",
		"scripts/plane":             script("printf 'plane %s\\n' \"$*\" >> " + calls),
		"scripts/backup":            script(`mkdir -p "$1/switchyard-test" && touch "$1/switchyard-test/database.dump" && printf %s "$SWITCHYARD_VERSION" > "$1/switchyard-test/source.txt"`),
		"scripts/codex-runner":      script("exit 0"),
		"deploy/switchyard.service": "[Service]\nDescription=old\nExecStart=@INSTALL_ROOT@/scripts/run\nEnvironment=PATH=%h/.local/share/mise/shims:%h/.local/bin:/usr/local/bin:/usr/bin:/bin\n",
	}
	for path, contents := range installed {
		writeTestFile(t, filepath.Join(root, path), contents)
	}
	for _, path := range []string{"scripts/plane", "scripts/backup", "scripts/codex-runner"} {
		if err := os.Chmod(filepath.Join(root, path), 0700); err != nil {
			t.Fatal(err)
		}
	}
	// The PATH command links to the previous release bundle, which shipped these files.
	previous := filepath.Join(home, ".local/share/switchyard/releases/old/switchyard")
	for path, contents := range installed {
		if !strings.HasSuffix(path, ".env") && path != "WORKFLOW.md" {
			writeTestFile(t, filepath.Join(previous, path), contents)
		}
	}
	writeTestFile(t, filepath.Join(previous, "switchyard"), "old cli")
	link := filepath.Join(home, ".local/bin/switchyard")
	if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(previous, "switchyard"), link); err != nil {
		t.Fatal(err)
	}
	a := Installation{Root: root, Settings: config.Settings{
		Port: board.Listener.Addr().(*net.TCPAddr).Port, RunnerPort: runner.Listener.Addr().(*net.TCPAddr).Port}}
	// The installed service came from the old template.
	unit, _, err := a.runnerUnit(installed["deploy/switchyard.service"], "")
	if err != nil {
		t.Fatal(err)
	}
	unitPath, err := config.UnitPath(root)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, unitPath, unit)

	oldAssets := fstest.MapFS{}
	for _, path := range []string{"scripts/plane", "scripts/backup", "scripts/codex-runner", "deploy/switchyard.service"} {
		oldAssets[path] = &fstest.MapFile{Data: []byte(installed[path]), Mode: 0700}
	}
	old := a
	old.Assets = oldAssets
	a.Assets = fstest.MapFS{
		"scripts/plane":             {Data: []byte(installed["scripts/plane"]), Mode: 0700},
		"scripts/backup":            {Data: []byte(installed["scripts/backup"]), Mode: 0700},
		"scripts/codex-runner":      {Data: []byte(script(newCodexRunner)), Mode: 0700},
		"deploy/switchyard.service": {Data: []byte(strings.Replace(installed["deploy/switchyard.service"], "Description=old", "Description=new", 1))},
	}
	bundle := filepath.Join(home, ".local/share/switchyard/releases/new/switchyard")
	writeTestFile(t, filepath.Join(bundle, "switchyard"), "new cli")
	writeTestFile(t, filepath.Join(bundle, "BUILD.txt"), "Switchyard "+newBuild+"\n")
	writeTestFile(t, filepath.Join(bundle, "bin/symphony"), "new runner")
	writeTestFile(t, filepath.Join(bundle, "licenses/NOTICE"), "notice")
	return upgradeFixture{a: a, old: old, bundle: bundle, previous: previous, calls: calls, running: running, busy: busy, broken: broken}
}

// leftovers lists what upgrades left in work/, apart from the lock.
func (f upgradeFixture) leftovers(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(f.a.Root, "work/upgrade*"))
	if err != nil {
		t.Fatal(err)
	}
	return slices.DeleteFunc(paths, func(path string) bool { return filepath.Base(path) == "upgrade.lock" })
}

// assertRelease checks that the installation is wholly one release and that
// the upgrade left nothing behind.
func (f upgradeFixture) assertRelease(t *testing.T, build, runner, description, cli string) {
	t.Helper()
	if got, _ := readBuild(filepath.Join(f.a.Root, "BUILD.txt")); got != build {
		t.Fatalf("build record = %q, want %q", got, build)
	}
	if got := readTestFile(t, filepath.Join(f.a.Root, "bin/symphony")); got != runner {
		t.Fatalf("runner = %q, want %q", got, runner)
	}
	if got := readTestFile(t, filepath.Join(f.a.Root, "deploy/switchyard.service")); !strings.Contains(got, description) {
		t.Fatalf("service template lacks %s:\n%s", description, got)
	}
	unitPath, _ := config.UnitPath(f.a.Root)
	if got := readTestFile(t, unitPath); !strings.Contains(got, description) {
		t.Fatalf("service lacks %s:\n%s", description, got)
	}
	if data, err := os.ReadFile(filepath.Join(f.a.Root, manifestName)); err == nil {
		var manifest releaseManifest
		if json.Unmarshal(data, &manifest) != nil || manifest.Build != build {
			t.Fatalf("the manifest records %q, not the installed build", manifest.Build)
		}
	}
	if target, err := filepath.EvalSymlinks(cliLink()); err != nil || target != cli {
		t.Fatalf("CLI link = %q, %v; want %q", target, err, cli)
	}
	if left := f.leftovers(t); len(left) != 0 {
		t.Fatalf("left behind %v", left)
	}
	news, _ := filepath.Glob(filepath.Join(f.a.Root, "*/*.new"))
	if len(news) != 0 {
		t.Fatalf("left release copies %v", news)
	}
}

func (f upgradeFixture) assertNew(t *testing.T) {
	t.Helper()
	f.assertRelease(t, newBuild, "new runner", "Description=new", filepath.Join(f.bundle, "switchyard"))
}

func (f upgradeFixture) assertOld(t *testing.T) {
	t.Helper()
	f.assertRelease(t, oldBuild, "old runner", "Description=old", filepath.Join(f.previous, "switchyard"))
}

// assertRunnerDisabled checks that the runner was last disabled, so a login
// cannot start it on files an upgrade is changing.
func (f upgradeFixture) assertRunnerDisabled(t *testing.T) {
	t.Helper()
	calls := readTestFile(t, f.calls)
	if last := strings.LastIndex(calls, "systemctl --user disable --now"); last < 0 || strings.Contains(calls[last:], "enable") {
		t.Fatalf("the runner is left enabled:\n%s", calls)
	}
}

// failAfterStart makes the new release fail verification after Plane started:
// its Plane script logs as before but fails the board check.
func (f upgradeFixture) failAfterStart() {
	f.a.Assets.(fstest.MapFS)["scripts/plane"] = &fstest.MapFile{Mode: 0700, Data: []byte("#!/bin/sh\nprintf 'plane %s\\n' \"$*\" >> " + f.calls + "\n[ \"$1\" != ps ]\n")}
}

// changePlaneImages makes the new release name another Plane image.
func (f upgradeFixture) changePlaneImages(t *testing.T) string {
	t.Helper()
	compose := filepath.Join(f.a.Root, "deploy/docker-compose.yml")
	writeTestFile(t, compose, "services:\n  api:\n    image: plane:1\n")
	writeTestFile(t, filepath.Join(f.previous, "deploy/docker-compose.yml"), "services:\n  api:\n    image: plane:1\n")
	f.a.Assets.(fstest.MapFS)["deploy/docker-compose.yml"] = &fstest.MapFile{Data: []byte("services:\n  api:\n    image: plane:2\n")}
	return compose
}

type simulatedCrash string

// crashUpgrade runs an upgrade that stops at step as if the process were killed.
func crashUpgrade(t *testing.T, a Installation, o UpgradeOptions, step string) {
	t.Helper()
	crashPoint = func(at string) {
		if at == step {
			panic(simulatedCrash(at))
		}
	}
	defer func() { crashPoint = func(string) {} }()
	crashed := func() (crashed bool) {
		defer func() { crashed = recover() == simulatedCrash(step) }()
		err := a.Upgrade(o)
		t.Logf("upgrade returned %v before %s", err, step)
		return false
	}()
	if !crashed {
		t.Fatalf("the upgrade never reached %s", step)
	}
}

func TestUpgradeBacksUpReplacesRestartsAndCleansUp(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err != nil {
		t.Fatal(err)
	}
	f.assertNew(t)
	calls := readTestFile(t, f.calls)
	if disable, backup := strings.Index(calls, "systemctl --user disable --now"), strings.Index(calls, "plane up -d plane-db"); disable < 0 || disable > backup {
		t.Fatalf("the runner is stopped and disabled right after the idle check, before the backup:\n%s", calls)
	}
	for _, want := range []string{"systemctl --user disable --now", "plane up -d plane-db", "systemctl --user daemon-reload", "systemctl --user enable --now", "plane up -d"} {
		if !strings.Contains(calls, want) {
			t.Fatalf("missing %q in:\n%s", want, calls)
		}
	}
	if _, err := os.Stat(f.previous); err != nil {
		t.Fatal("the previous release is kept for rollback")
	}
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err != nil {
		t.Fatalf("re-running on the same release is a no-op: %v", err)
	}
	f.assertNew(t)
}

func TestUpgradeIdentifiesInstallationsWithoutABuildRecord(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	if err := os.Remove(filepath.Join(f.a.Root, "BUILD.txt")); err != nil {
		t.Fatal(err)
	}
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err != nil {
		t.Fatal(err)
	}
	// The linked release with the same runner is the baseline, so untouched
	// files are replaced rather than kept beside a .new copy.
	f.assertNew(t)
}

// An upgrade killed at any step is finished or undone by running upgrade
// again, from the new release or from the installed one.
func TestUpgradeRecoversFromAnInterruptionAtEveryStep(t *testing.T) {
	steps := []string{"begun", "stopped", "backed-up", "applying", "applied", "unit", "started", "verified", "committed", "linked"}
	for _, step := range steps {
		t.Run(step+"/rerun new", func(t *testing.T) {
			f := newUpgradeFixture(t, "exit 0")
			crashUpgrade(t, f.a, UpgradeOptions{Bundle: f.bundle}, step)
			if slices.Contains([]string{"stopped", "backed-up", "applying", "applied", "unit"}, step) {
				f.assertRunnerDisabled(t)
			}
			if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err != nil {
				t.Fatal(err)
			}
			f.assertNew(t)
		})
		t.Run(step+"/rerun installed", func(t *testing.T) {
			f := newUpgradeFixture(t, "exit 0")
			crashUpgrade(t, f.a, UpgradeOptions{Bundle: f.bundle}, step)
			if err := f.old.Upgrade(UpgradeOptions{Bundle: f.previous}); err != nil {
				t.Fatal(err)
			}
			// After the commit point this finishes the upgrade, then moves back.
			f.assertOld(t)
		})
	}
}

// A rollback killed after restoring files is completed by the next run.
func TestUpgradeRecoversFromAnInterruptedRollback(t *testing.T) {
	f := newUpgradeFixture(t, "exit 1")
	crashUpgrade(t, f.a, UpgradeOptions{Bundle: f.bundle}, "restored")
	f.assertRunnerDisabled(t)
	if err := f.old.Upgrade(UpgradeOptions{Bundle: f.previous}); err != nil {
		t.Fatal(err)
	}
	f.assertOld(t)
}

func TestRollbackStopsPlaneWithThePreviousReleasesScripts(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	// The new release ships a broken Plane script, so it cannot start or stop Plane.
	f.a.Assets.(fstest.MapFS)["scripts/plane"] = &fstest.MapFile{Data: []byte("#!/bin/sh\nexit 1\n"), Mode: 0700}
	err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle})
	if err == nil || !strings.Contains(err.Error(), oldBuild[:7]+" was restored") {
		t.Fatalf("err = %v", err)
	}
	f.assertOld(t)
	if !strings.Contains(readTestFile(t, f.calls), "plane down --remove-orphans") {
		t.Fatal("Plane is stopped with the restored script")
	}
}

func TestUpgradeKeepsLinkedFilesAndRestoresALinkedService(t *testing.T) {
	f := newUpgradeFixture(t, "exit 1")
	// The operator links the service template and the runner service to managed copies.
	managed := t.TempDir()
	template := filepath.Join(f.a.Root, "deploy/switchyard.service")
	writeTestFile(t, filepath.Join(managed, "template"), readTestFile(t, template))
	unitPath, _ := config.UnitPath(f.a.Root)
	writeTestFile(t, filepath.Join(managed, "unit"), readTestFile(t, unitPath))
	for link, target := range map[string]string{template: filepath.Join(managed, "template"), unitPath: filepath.Join(managed, "unit")} {
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	files, err := f.a.releaseFiles(f.bundle)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := planChanges(f.a.Root, files, f.a.baseline(oldBuild, f.previous), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range changes {
		if change.path == "deploy/switchyard.service" && !change.customized {
			t.Fatal("a linked release file is kept as a customization")
		}
	}
	// A unit whose service changes is replaced, and rollback brings the link back.
	tx, err := f.a.begin(transaction{From: oldBuild, To: newBuild})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.change(unitPath, func(path string) error { return writeFile(path, []byte("replaced"), 0600) }); err != nil {
		t.Fatal(err)
	}
	restorePlan(t, tx)
	if target, err := os.Readlink(unitPath); err != nil || target != filepath.Join(managed, "unit") {
		t.Fatalf("the service link is restored: %q, %v", target, err)
	}
}

func TestUpgradeLeavesLinkedDirectoriesAlone(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	// The operator manages deploy/ elsewhere and links it in.
	managed := filepath.Join(t.TempDir(), "deploy")
	if err := os.Rename(filepath.Join(f.a.Root, "deploy"), managed); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(managed, filepath.Join(f.a.Root, "deploy")); err != nil {
		t.Fatal(err)
	}
	before := readTestFile(t, filepath.Join(managed, "switchyard.service"))
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(managed)
	if readTestFile(t, filepath.Join(managed, "switchyard.service")) != before || len(entries) != 1 {
		t.Fatalf("nothing is written into a linked directory: %v", entries)
	}
}

func TestKeptBackupsOfUpgradesInTheSameSecondDoNotCollide(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle, KeepBackup: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.old.Upgrade(UpgradeOptions{Bundle: f.previous, KeepBackup: true}); err != nil {
		t.Fatal(err)
	}
	if kept, _ := filepath.Glob(filepath.Join(f.a.Root, "work/upgrade-backup-*")); len(kept) != 2 {
		t.Fatalf("each upgrade keeps its own backup: %v", kept)
	}
}

func TestRollbackRestoresALinkedServiceThatDisablingRemoved(t *testing.T) {
	f := newUpgradeFixture(t, "exit 1")
	unitPath, _ := config.UnitPath(f.a.Root)
	managed := filepath.Join(t.TempDir(), "runner.service")
	writeTestFile(t, managed, readTestFile(t, unitPath))
	if err := os.Remove(unitPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(managed, unitPath); err != nil {
		t.Fatal(err)
	}
	// systemd removes a linked unit when it is disabled.
	bin := t.TempDir()
	writeTestFile(t, filepath.Join(bin, "systemctl"), "#!/bin/sh\n[ \"$2\" != show ] || { echo active; exit 0; }\nprintf 'systemctl %s\\n' \"$*\" >> "+f.calls+"\ncase \"$*\" in \"--user disable\"*) [ ! -L "+unitPath+" ] || rm "+unitPath+";; esac\n")
	if err := os.Chmod(filepath.Join(bin, "systemctl"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err == nil || !strings.Contains(err.Error(), oldBuild[:7]+" was restored") {
		t.Fatalf("err = %v", err)
	}
	if target, err := os.Readlink(unitPath); err != nil || target != managed {
		t.Fatalf("the linked service is restored: %q, %v", target, err)
	}
}

func TestUpgradeRestoresTheGeneratedServicesMode(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	// The service already matches the new template but became world-writable.
	template, _ := fs.ReadFile(f.a.Assets, "deploy/switchyard.service")
	unit, _, err := f.a.runnerUnit(string(template), "")
	if err != nil {
		t.Fatal(err)
	}
	unitPath, _ := config.UnitPath(f.a.Root)
	writeTestFile(t, unitPath, unit)
	if err := os.Chmod(unitPath, 0666); err != nil {
		t.Fatal(err)
	}
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(unitPath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("the service gets up's mode back: %v %v", info, err)
	}
}

func TestInstallingTheRunnerRecordsTheReleaseManifest(t *testing.T) {
	bundle := t.TempDir()
	writeTestFile(t, filepath.Join(bundle, "bin/symphony"), "runner")
	writeTestFile(t, filepath.Join(bundle, "BUILD.txt"), "Switchyard "+newBuild+"\n")
	writeTestFile(t, filepath.Join(bundle, "licenses/NOTICE"), "notice")
	a := Installation{Root: t.TempDir(), Assets: fstest.MapFS{"scripts/run": {Data: []byte("run")}}}
	if err := a.installRunner(filepath.Join(bundle, "bin/symphony")); err != nil {
		t.Fatal(err)
	}
	manifest := a.manifest(newBuild)
	if manifest.Files["scripts/run"] != digest([]byte("run")) || manifest.Files["bin/symphony"] != digest([]byte("runner")) {
		t.Fatalf("the first installation records what it shipped: %+v", manifest)
	}
}

func TestUpgradeKeepsTheBackupWhenAsked(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle, KeepBackup: true}); err != nil {
		t.Fatal(err)
	}
	kept, _ := filepath.Glob(filepath.Join(f.a.Root, "work/upgrade-backup-*"))
	if len(kept) != 1 {
		t.Fatalf("kept backups = %v", kept)
	}
	if got := readTestFile(t, filepath.Join(kept[0], "switchyard-test/source.txt")); got != oldBuild[:7] {
		t.Fatalf("the backup records the release it holds: %q", got)
	}
	if _, err := os.Stat(filepath.Join(f.a.Root, "work/upgrade")); !os.IsNotExist(err) {
		t.Fatal("only the backup is kept")
	}
}

func TestUpgradeFailureRestoresThePreviousRelease(t *testing.T) {
	// The new release's sign-in check fails, so starting the new release fails.
	f := newUpgradeFixture(t, "exit 1")
	err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle})
	if err == nil || !strings.Contains(err.Error(), oldBuild[:7]+" was restored") {
		t.Fatalf("err = %v", err)
	}
	f.assertOld(t)
	if strings.Count(readTestFile(t, f.calls), "systemctl --user enable --now") != 1 {
		t.Fatal("the restored release is started again")
	}
}

func TestARetriedMigratedRollbackStillNamesTheKeptSnapshot(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	f.changePlaneImages(t)
	f.failAfterStart()
	// The rollback dies after close moved the backup but before the journal went.
	crashPoint = func(step string) {
		if step == "backup moved" {
			panic(simulatedCrash(step))
		}
	}
	func() {
		defer func() { crashPoint = func(string) {}; _ = recover() }()
		_ = f.a.Upgrade(UpgradeOptions{Bundle: f.bundle})
	}()
	if _, err := os.Stat(filepath.Join(f.a.Root, "work/upgrade/journal")); err != nil {
		t.Fatal("the journal outlives the interruption")
	}
	if err := f.old.Upgrade(UpgradeOptions{Bundle: f.previous}); err == nil || !strings.Contains(err.Error(), "may have migrated") {
		t.Fatalf("err = %v", err)
	}
	kept, _ := filepath.Glob(filepath.Join(f.a.Root, "work/upgrade-backup-*/switchyard-test"))
	if len(kept) != 1 || !strings.Contains(readTestFile(t, f.a.migrationBlock()), kept[0]+" ") {
		t.Fatalf("the block names the kept snapshot %v:\n%s", kept, readTestFile(t, f.a.migrationBlock()))
	}
}

func TestRollbackWaitsForWorkTheNewRunnerClaimed(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	// The new runner claims a task, then the board check fails verification.
	f.a.Assets.(fstest.MapFS)["scripts/plane"] = &fstest.MapFile{Mode: 0700, Data: []byte("#!/bin/sh\nprintf 'plane %s\\n' \"$*\" >> " + f.calls + "\n[ \"$1\" != ps ] || { touch " + f.busy + "; exit 1; }\n")}
	err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle})
	if err == nil || !strings.Contains(err.Error(), "1 running") || !strings.Contains(err.Error(), "is not restored yet") {
		t.Fatalf("err = %v", err)
	}
	if readTestFile(t, filepath.Join(f.a.Root, "bin/symphony")) != "new runner" {
		t.Fatal("the new release keeps running its task")
	}
	if err := os.Remove(f.busy); err != nil {
		t.Fatal(err)
	}
	if err := f.old.Upgrade(UpgradeOptions{Bundle: f.previous}); err != nil {
		t.Fatal(err)
	}
	f.assertOld(t)
}

func TestRollbackWaitsUntilTheNewRunnerCanReportItsWork(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	crashUpgrade(t, f.a, UpgradeOptions{Bundle: f.bundle}, "started")
	f.broken.Store(true)
	err := f.old.Upgrade(UpgradeOptions{Bundle: f.previous})
	if err == nil || !strings.Contains(err.Error(), "cannot be confirmed idle") || !strings.Contains(err.Error(), "systemctl --user stop") {
		t.Fatalf("err = %v", err)
	}
	if readTestFile(t, filepath.Join(f.a.Root, "bin/symphony")) != "new runner" {
		t.Fatal("nothing is restored while the runner's work is unknown")
	}
	f.broken.Store(false)
	if err := f.old.Upgrade(UpgradeOptions{Bundle: f.previous}); err != nil {
		t.Fatal(err)
	}
	f.assertOld(t)
}

func TestRollbackLeavesPlaneStoppedWhenItsImagesChanged(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	compose := f.changePlaneImages(t)
	f.failAfterStart()
	err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle})
	if err == nil || !strings.Contains(err.Error(), "may have migrated its data") {
		t.Fatalf("err = %v", err)
	}
	if readTestFile(t, compose) != "services:\n  api:\n    image: plane:1\n" || readTestFile(t, filepath.Join(f.a.Root, "bin/symphony")) != "old runner" {
		t.Fatal("the previous release's files are restored")
	}
	kept, _ := filepath.Glob(filepath.Join(f.a.Root, "work/upgrade-backup-*"))
	if len(kept) != 1 || !strings.Contains(err.Error(), "restore the backup in "+filepath.Join(kept[0], "switchyard-test")+" ") {
		t.Fatalf("the restorable snapshot is kept and named: %v, %v", kept, err)
	}
	calls := readTestFile(t, f.calls)
	if strings.Count(calls, "systemctl --user enable --now") != 1 {
		t.Fatal("only the new release was started, not the previous one against data it may have migrated")
	}
	f.assertRunnerDisabled(t)
	// The block outlives the journal, so later commands refuse until it is
	// resolved, and it names the snapshot where it was kept.
	if err := f.old.Up(); err == nil || !strings.Contains(err.Error(), "may be migrated") || !strings.Contains(err.Error(), filepath.Join(kept[0], "switchyard-test")+" ") {
		t.Fatalf("up refuses while Plane's data may be migrated: %v", err)
	}
	if err := f.old.startBoard(); err == nil || !strings.Contains(err.Error(), "may be migrated") {
		t.Fatalf("every Plane start refuses: %v", err)
	}
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err == nil || !strings.Contains(err.Error(), "may be migrated") {
		t.Fatalf("upgrade refuses while Plane's data may be migrated: %v", err)
	}
	// An unreadable block still blocks.
	if err := os.Chmod(f.a.migrationBlock(), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(f.a.migrationBlock()); err == nil {
		t.Log("running with permission to read anything; skipping the unreadable case")
	} else if err := f.old.startBoard(); err == nil || !strings.Contains(err.Error(), "may be migrated") {
		t.Fatalf("an unreadable block refuses: %v", err)
	}
	if err := os.Remove(f.a.migrationBlock()); err != nil {
		t.Fatal(err)
	}
	if err := f.old.Up(); err != nil {
		t.Fatalf("up starts once the block is resolved: %v", err)
	}
}

func TestAFailedCheckBeforeStartingRestartsThePreviousRelease(t *testing.T) {
	// The new release changes Plane's images but fails its sign-in check, so no
	// new image ran and the previous release can start again.
	f := newUpgradeFixture(t, "exit 1")
	f.changePlaneImages(t)
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err == nil || !strings.Contains(err.Error(), oldBuild[:7]+" was restored") {
		t.Fatalf("err = %v", err)
	}
	if strings.Count(readTestFile(t, f.calls), "systemctl --user enable --now") != 1 {
		t.Fatal("the previous release is started again")
	}
}

func TestAnInterruptedRollbackStillLeavesMigratedPlaneStopped(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	f.changePlaneImages(t)
	f.failAfterStart()
	crashUpgrade(t, f.a, UpgradeOptions{Bundle: f.bundle}, "restored")
	// The Compose file is already restored, so only the journal knows.
	if err := f.old.Upgrade(UpgradeOptions{Bundle: f.previous}); err == nil || !strings.Contains(err.Error(), "may have migrated its data") {
		t.Fatalf("err = %v", err)
	}
	if strings.Count(readTestFile(t, f.calls), "systemctl --user enable --now") != 1 {
		t.Fatal("the previous release is not started against data the new one may have migrated")
	}
}

func TestRecoveryWaitsForWorkTheInterruptedReleaseStarted(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	crashUpgrade(t, f.a, UpgradeOptions{Bundle: f.bundle}, "started")
	f.running.Store(1)
	if err := f.old.Upgrade(UpgradeOptions{Bundle: f.previous}); err == nil || !strings.Contains(err.Error(), "1 running") {
		t.Fatalf("err = %v", err)
	}
	if readTestFile(t, filepath.Join(f.a.Root, "bin/symphony")) != "new runner" {
		t.Fatal("nothing is restored while the runner has work")
	}
	f.running.Store(0)
	if err := f.old.Upgrade(UpgradeOptions{Bundle: f.previous}); err != nil {
		t.Fatal(err)
	}
	f.assertOld(t)
}

func TestRollbackLeavesFilesWhenTheNewReleaseCannotBeStopped(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	crashUpgrade(t, f.a, UpgradeOptions{Bundle: f.bundle}, "applied")
	// The runner stays active and idle, but systemd will not stop it.
	bin := t.TempDir()
	writeTestFile(t, filepath.Join(bin, "systemctl"), "#!/bin/sh\ncase \"$*\" in *show*) echo active; exit 0;; *is-active*) exit 0;; esac\nexit 1\n")
	if err := os.Chmod(filepath.Join(bin, "systemctl"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	if err := f.old.Upgrade(UpgradeOptions{Bundle: f.previous}); err == nil || !strings.Contains(err.Error(), "could not stop the runner") {
		t.Fatalf("err = %v", err)
	}
	if readTestFile(t, filepath.Join(f.a.Root, "bin/symphony")) != "new runner" {
		t.Fatal("files stay in place while the new release may still run")
	}
	if _, err := os.Stat(filepath.Join(f.a.Root, "work/upgrade/journal")); err != nil {
		t.Fatal("the journal is kept for a later run")
	}
}

func TestRollbackRestartsTheServiceThatRanBeforeTheUpgrade(t *testing.T) {
	f := newUpgradeFixture(t, "exit 1")
	// A newer CLI's up already generated the service from the new template, so
	// the upgrade has nothing to change in it.
	template, _ := fs.ReadFile(f.a.Assets, "deploy/switchyard.service")
	unit, _, err := f.a.runnerUnit(string(template), "")
	if err != nil {
		t.Fatal(err)
	}
	unitPath, _ := config.UnitPath(f.a.Root)
	writeTestFile(t, unitPath, unit)
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err == nil || !strings.Contains(err.Error(), oldBuild[:7]+" was restored") {
		t.Fatalf("err = %v", err)
	}
	if readTestFile(t, unitPath) != unit || readTestFile(t, filepath.Join(f.a.Root, "bin/symphony")) != "old runner" {
		t.Fatal("rollback returns to the state before the upgrade, service included")
	}
	if strings.Count(readTestFile(t, f.calls), "systemctl --user enable --now") != 1 {
		t.Fatal("the previous release is started with that service")
	}
}

func TestRollbackKeepsAWorkingServiceDespiteACustomizedTemplate(t *testing.T) {
	f := newUpgradeFixture(t, "exit 1")
	template := filepath.Join(f.a.Root, "deploy/switchyard.service")
	writeTestFile(t, template, readTestFile(t, template)+"Broken=yes\n")
	unitPath, _ := config.UnitPath(f.a.Root)
	before := readTestFile(t, unitPath)
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err == nil || !strings.Contains(err.Error(), oldBuild[:7]+" was restored") {
		t.Fatalf("err = %v", err)
	}
	if readTestFile(t, unitPath) != before {
		t.Fatal("the working service is restored, not regenerated from the customized template")
	}
}

func TestRollbackRemovesAServiceTheUpgradeCreated(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	unitPath, _ := config.UnitPath(f.a.Root)
	if err := os.Remove(unitPath); err != nil {
		t.Fatal(err)
	}
	// up created the new release's service before the upgrade was interrupted.
	crashUpgrade(t, f.a, UpgradeOptions{Bundle: f.bundle}, "started")
	if !strings.Contains(readTestFile(t, unitPath), "Description=new") {
		t.Fatal("the new release created its service")
	}
	if err := f.old.Upgrade(UpgradeOptions{Bundle: f.previous}); err != nil {
		t.Fatalf("the restored release starts with its own service: %v", err)
	}
	f.assertOld(t)
}

func TestUpgradeRefusesAnUnreadableUpgradeRecord(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	writeTestFile(t, filepath.Join(f.a.Root, "work/upgrade/journal"), "not json\n")
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err == nil || !strings.Contains(err.Error(), "is unreadable") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(f.calls); err == nil {
		t.Fatal("nothing is stopped without a readable record")
	}
}

func TestUpgradeRemovesAWorkDirectoryWithoutAJournal(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	writeTestFile(t, filepath.Join(f.a.Root, "work/upgrade/backup/partial"), "")
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err != nil {
		t.Fatal(err)
	}
	f.assertNew(t)
}

func TestUpgradeRefusesWhileAnotherUpgradeHoldsTheLock(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	unlock, err := f.a.lockUpgrade()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err == nil || !strings.Contains(err.Error(), "another switchyard upgrade is running") {
		t.Fatalf("err = %v", err)
	}
	unlock()
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err != nil {
		t.Fatalf("the lock is released with its holder: %v", err)
	}
}

func TestUpgradeReplansFilesChangedDuringTheBackup(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	template := filepath.Join(f.a.Root, "deploy/switchyard.service")
	backup := filepath.Join(f.a.Root, "scripts/backup")
	writeTestFile(t, backup, readTestFile(t, backup)+"printf '# local note\\n' >> "+template+"\n")
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(readTestFile(t, template), "# local note\n") || !strings.Contains(readTestFile(t, template+".new"), "Description=new") {
		t.Fatal("a file customized during the backup is kept, with the release copy beside it")
	}
}

func TestARefusedUpgradeKeepsServiceEditsMadeDuringTheBackup(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	unitPath, _ := config.UnitPath(f.a.Root)
	backup := filepath.Join(f.a.Root, "scripts/backup")
	writeTestFile(t, backup, readTestFile(t, backup)+"printf 'MemoryMax=2G\\n' >> "+unitPath+"\n")
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err == nil || !strings.Contains(err.Error(), "differs from the one a Switchyard release generates") {
		t.Fatalf("err = %v", err)
	}
	if !strings.HasSuffix(readTestFile(t, unitPath), "MemoryMax=2G\n") {
		t.Fatal("the operator's edit survives the refusal")
	}
	if len(f.leftovers(t)) != 0 {
		t.Fatal("the refused upgrade cleans up")
	}
}

func TestOnlyALinkedServiceIsSnapshottedWithTheJournalHeader(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	unitPath, _ := config.UnitPath(f.a.Root)
	tx, err := f.a.begin(transaction{From: oldBuild, To: newBuild})
	if err != nil {
		t.Fatal(err)
	}
	if entries, _ := tx.entries(); len(entries) != 0 {
		t.Fatalf("a regular service is left to its own journal entries: %+v", entries)
	}
	if _, err := tx.close(false); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(t.TempDir(), "runner.service")
	if err := os.Rename(unitPath, managed); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(managed, unitPath); err != nil {
		t.Fatal(err)
	}
	if tx, err = f.a.begin(transaction{From: oldBuild, To: newBuild}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := tx.entries(); len(entries) != 1 || entries[0].Link != managed || !entries[0].IfMissing {
		t.Fatalf("a linked service is journaled with the header: %+v", entries)
	}
}

func TestUpgradeReplansFilesChangedWhileWaiting(t *testing.T) {
	idleRetry = time.Millisecond
	t.Cleanup(func() { idleRetry = 30 * time.Second })
	f := newUpgradeFixture(t, "exit 0")
	template := filepath.Join(f.a.Root, "deploy/switchyard.service")
	customized := readTestFile(t, template) + "# local note\n"
	f.running.Store(1)
	go func() {
		time.Sleep(20 * time.Millisecond)
		if err := os.WriteFile(template, []byte(customized), 0600); err == nil {
			f.running.Store(0)
		}
	}()
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle, Wait: true}); err != nil {
		t.Fatal(err)
	}
	if readTestFile(t, template) != customized || !strings.Contains(readTestFile(t, template+".new"), "Description=new") {
		t.Fatal("a file customized while waiting is kept, with the release copy beside it")
	}
}

func TestUpgradeStopsALoadedRunnerWhoseUnitFileWasRemoved(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	unitPath, _ := config.UnitPath(f.a.Root)
	if err := os.Remove(unitPath); err != nil {
		t.Fatal(err)
	}
	if err := f.a.stopRunner(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readTestFile(t, f.calls), "systemctl --user stop "+config.Name(f.a.Root)+".service") {
		t.Fatal("the runner is stopped by name even without its unit file")
	}
	// A unit that is neither loaded nor active cannot be stopped, which is fine
	// once systemd confirms it is inactive, but not when systemd cannot answer.
	bin := t.TempDir()
	answer := filepath.Join(bin, "answer")
	writeTestFile(t, filepath.Join(bin, "systemctl"), "#!/bin/sh\n[ \"$2\" = show ] && [ -e "+answer+" ] && { echo inactive; exit 0; }\nexit 5\n")
	if err := os.Chmod(filepath.Join(bin, "systemctl"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	if err := f.a.stopRunner(); err == nil {
		t.Fatal("a stop that systemd cannot confirm is refused")
	}
	writeTestFile(t, answer, "")
	if err := f.a.stopRunner(); err != nil {
		t.Fatalf("an inactive runner needs no stop: %v", err)
	}
}

func TestUpgradeRefusesAHandEditedServiceBeforeChangingAnything(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	unitPath, _ := config.UnitPath(f.a.Root)
	writeTestFile(t, unitPath, readTestFile(t, unitPath)+"MemoryMax=2G\n")
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err == nil || !strings.Contains(err.Error(), "differs from the one a Switchyard release generates") {
		t.Fatalf("err = %v", err)
	}
	if !strings.HasSuffix(readTestFile(t, unitPath), "MemoryMax=2G\n") || len(f.leftovers(t)) != 0 {
		t.Fatal("a refused upgrade keeps the service and makes no backup")
	}
	if _, err := os.Stat(f.calls); err == nil {
		t.Fatal("a refused upgrade does not touch the services")
	}
}

func TestUpgradeRefusesADanglingServiceLink(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	unitPath, _ := config.UnitPath(f.a.Root)
	if err := os.Remove(unitPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone"), unitPath); err != nil {
		t.Fatal(err)
	}
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err == nil || !strings.Contains(err.Error(), "is a link to a missing file") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(f.calls); err == nil || len(f.leftovers(t)) != 0 {
		t.Fatal("a refused upgrade touches nothing")
	}
}

func TestUpgradeRefusesAServiceFromACustomizedTemplate(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	template := filepath.Join(f.a.Root, "deploy/switchyard.service")
	writeTestFile(t, template, readTestFile(t, template)+"MemoryMax=2G\n")
	unit, _, err := f.a.runnerUnit(readTestFile(t, template), "")
	if err != nil {
		t.Fatal(err)
	}
	unitPath, _ := config.UnitPath(f.a.Root)
	writeTestFile(t, unitPath, unit)
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err == nil || !strings.Contains(err.Error(), "differs from the one a Switchyard release generates") {
		t.Fatalf("err = %v", err)
	}
	if readTestFile(t, unitPath) != unit || len(f.leftovers(t)) != 0 {
		t.Fatal("a refused upgrade keeps the service and makes no backup")
	}
}

func TestUpgradeRefusesBusyRunnerBeforeChangingAnything(t *testing.T) {
	f := newUpgradeFixture(t, "exit 0")
	f.running.Store(1)
	if err := f.a.Upgrade(UpgradeOptions{Bundle: f.bundle}); err == nil || !strings.Contains(err.Error(), "1 running") {
		t.Fatalf("err = %v", err)
	}
	if readTestFile(t, filepath.Join(f.a.Root, "bin/symphony")) != "old runner" || len(f.leftovers(t)) != 0 {
		t.Fatal("a refused upgrade changes nothing")
	}
	if _, err := os.Stat(f.calls); err == nil && strings.Contains(readTestFile(t, f.calls), "stop") {
		t.Fatal("a refused upgrade does not stop services")
	}
}
