package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Kthom1/switchyard/config"
)

// This file holds what an upgrade knows about releases: which files a release
// installs, which installed files were customized, and where releases live.

const manifestName = "release-manifest.json"

// A release manifest records each release-owned file's shipped hash, so later
// upgrades can tell untouched files from local customization.
type releaseManifest struct {
	Build string            `json:"build"`
	Files map[string]string `json:"files"`
}

type releaseFile struct {
	data []byte
	mode fs.FileMode
}

type fileChange struct {
	path       string
	file       releaseFile
	customized bool
	retired    bool // shipped by the installed release but not by the new one
	linked     bool // beneath a linked directory, so left alone entirely
}

var embedLine = regexp.MustCompile(`(?m)^//go:embed (.+)$`)

var releaseVersion = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

var runtimeName = regexp.MustCompile(`^(?:symphony|yardmaster)_erts-[^_]+_0\.1\.0\+([0-9a-f]{40})$`)

func (a Installation) releaseFiles(bundle string) (map[string]releaseFile, error) {
	files := map[string]releaseFile{}
	err := fs.WalkDir(a.Assets, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(a.Assets, path)
		if err != nil {
			return err
		}
		mode := fs.FileMode(0600)
		if strings.HasPrefix(path, "scripts/") {
			mode = 0700
		}
		files[path] = releaseFile{data, mode}
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = fs.WalkDir(os.DirFS(bundle), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !runnerOwned(path) {
			return err
		}
		data, err := os.ReadFile(filepath.Join(bundle, path))
		if err != nil {
			return err
		}
		mode := fs.FileMode(0600)
		if path == "bin/symphony" {
			mode = 0700
		}
		files[path] = releaseFile{data, mode}
		return nil
	})
	if _, ok := files["bin/symphony"]; err == nil && !ok {
		err = errors.New("the release bundle has no bin/symphony runner")
	}
	return files, err
}

// The runner, its build record and its notices are never customized locally.
func runnerOwned(path string) bool {
	return path == "bin/symphony" || path == "BUILD.txt" || strings.HasPrefix(path, "licenses/")
}

// baseline returns the hash each file had when the current release installed
// it, from the release manifest or else the previous release bundle.
func (a Installation) baseline(oldBuild, previous string) func(string) string {
	manifest := a.manifest(oldBuild)
	return func(path string) string {
		if hash, ok := manifest.Files[path]; ok {
			return hash
		}
		if previous != "" {
			if data, err := os.ReadFile(filepath.Join(previous, path)); err == nil {
				return digest(data)
			}
		}
		return ""
	}
}

// manifest returns the installed release's manifest, or an empty one when it
// is missing or records another build.
func (a Installation) manifest(build string) releaseManifest {
	var manifest releaseManifest
	if data, err := os.ReadFile(filepath.Join(a.Root, manifestName)); err == nil {
		if json.Unmarshal(data, &manifest) != nil || manifest.Build != build {
			manifest = releaseManifest{}
		}
	}
	return manifest
}

// shippedFiles lists the files the installed release installed: from its
// manifest, or for releases before manifests, from the previous bundle's
// embedded asset list and runner files.
func (a Installation) shippedFiles(build, previous string) []string {
	if files := a.manifest(build).Files; len(files) > 0 || previous == "" {
		return slices.Sorted(maps.Keys(files))
	}
	source, err := os.ReadFile(filepath.Join(previous, "assets.go"))
	if err != nil {
		return nil
	}
	shipped := map[string]bool{}
	for _, match := range embedLine.FindAllStringSubmatch(string(source), -1) {
		for _, pattern := range strings.Fields(match[1]) {
			paths, _ := fs.Glob(os.DirFS(previous), pattern)
			for _, path := range paths {
				_ = fs.WalkDir(os.DirFS(previous), path, func(path string, entry fs.DirEntry, err error) error {
					if err == nil && !entry.IsDir() {
						shipped[path] = true
					}
					return nil
				})
			}
		}
	}
	_ = fs.WalkDir(os.DirFS(previous), ".", func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && runnerOwned(path) {
			shipped[path] = true
		}
		return nil
	})
	return slices.Sorted(maps.Keys(shipped))
}

func planChanges(root string, files map[string]releaseFile, baseline func(string) string, shipped []string) ([]fileChange, error) {
	var changes []fileChange
	for _, path := range slices.Sorted(maps.Keys(files)) {
		file := files[path]
		if linkedDirectory(root, path) {
			// Writing here would change the operator's linked copy.
			if installed, err := os.ReadFile(filepath.Join(root, path)); err != nil || !bytes.Equal(installed, file.data) {
				changes = append(changes, fileChange{path: path, file: file, customized: true, linked: true})
			}
			continue
		}
		info, installed, err := readInstalled(filepath.Join(root, path))
		if errors.Is(err, os.ErrNotExist) {
			changes = append(changes, fileChange{path: path, file: file})
			continue
		} else if err != nil {
			return nil, err
		}
		same := bytes.Equal(installed, file.data)
		switch {
		// A link is the operator's, whatever it points to.
		case info.Mode()&fs.ModeSymlink != 0 && !runnerOwned(path):
			if !same {
				changes = append(changes, fileChange{path: path, file: file, customized: true})
			}
		// Identical contents still need the release's permissions.
		case same && info.Mode().Perm() == file.mode:
		case same || runnerOwned(path) || baseline(path) == digest(installed):
			changes = append(changes, fileChange{path: path, file: file})
		default:
			changes = append(changes, fileChange{path: path, file: file, customized: true})
		}
	}
	for _, path := range shipped {
		if _, ok := files[path]; ok || linkedDirectory(root, path) {
			continue
		}
		info, installed, err := readInstalled(filepath.Join(root, path))
		if errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		link := info.Mode()&fs.ModeSymlink != 0
		customized := !runnerOwned(path) && (link || baseline(path) != digest(installed))
		changes = append(changes, fileChange{path: path, retired: true, customized: customized})
	}
	return changes, nil
}

// linkedDirectory reports whether a directory between root and path is a
// symbolic link, which makes everything beneath it the operator's.
func linkedDirectory(root, path string) bool {
	dir := root
	parts := strings.Split(filepath.Dir(path), string(filepath.Separator))
	for _, part := range parts {
		if part == "." || part == "" {
			continue
		}
		dir = filepath.Join(dir, part)
		if info, err := os.Lstat(dir); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			return true
		}
	}
	return false
}

// readInstalled describes an installed path without following a link, and
// reads its contents through one; a dangling link reads as empty.
func readInstalled(path string) (fs.FileInfo, []byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil && info.Mode()&fs.ModeSymlink != 0 {
		return info, nil, nil
	}
	return info, data, err
}

// checkUnit refuses a runner service that Switchyard did not generate, as up
// does, before anything changes. The previous or new release's template may have
// generated it, or the installed template when it is not customized.
func (a Installation) checkUnit(previous string, baseline func(string) string) error {
	path, err := config.UnitPath(a.Root)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	existing, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("the runner service %s is a link to a missing file; remove it or restore its target, then rerun upgrade", path)
	} else if err != nil {
		return err
	}
	const name = "deploy/switchyard.service"
	var templates [][]byte
	if data, err := fs.ReadFile(a.Assets, name); err == nil {
		templates = append(templates, data)
	}
	if data, err := os.ReadFile(filepath.Join(previous, name)); previous != "" && err == nil {
		templates = append(templates, data)
	}
	if data, err := os.ReadFile(filepath.Join(a.Root, name)); err == nil && digest(data) == baseline(name) {
		templates = append(templates, data)
	}
	for _, template := range templates {
		if unit, _, err := a.runnerUnit(string(template), string(existing)); err == nil && unit == string(existing) {
			return nil
		}
	}
	return fmt.Errorf("the runner service %s differs from the one a Switchyard release generates, and switchyard up refuses it too; restore it or remove the stopped service, then rerun upgrade", path)
}

func readBuild(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(string(data), "\n")
	build, ok := strings.CutPrefix(line, "Switchyard ")
	if !ok || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(build) {
		return "", fmt.Errorf("%s is not a Switchyard build record", path)
	}
	return build, nil
}

func cliLink() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local/bin/switchyard")
}

// installedRelease identifies the installed build and, when available, the
// release bundle it came from: the one the PATH command links to, confirmed by
// the recorded build or, for installations without a build record, by an
// identical runner.
func (a Installation) installedRelease() (build, bundle string) {
	build, _ = readBuild(filepath.Join(a.Root, "BUILD.txt"))
	target, err := filepath.EvalSymlinks(cliLink())
	if err != nil {
		return build, ""
	}
	linked := filepath.Dir(target)
	linkedBuild, err := readBuild(filepath.Join(linked, "BUILD.txt"))
	if err != nil {
		return build, ""
	}
	if build == "" && sameFile(filepath.Join(a.Root, "bin/symphony"), filepath.Join(linked, "bin/symphony")) {
		build = linkedBuild
	}
	if linkedBuild != build {
		return build, ""
	}
	return build, linked
}

func sameFile(first, second string) bool {
	a, err := os.ReadFile(first)
	if err != nil {
		return false
	}
	b, err := os.ReadFile(second)
	return err == nil && bytes.Equal(a, b)
}

// linkCLI points the documented PATH link at the new release when it is a
// link to a release bundle; any other command on PATH is left alone. It fails
// only when it should repoint the link and cannot.
func linkCLI(executable string) error {
	if !exists(executable) {
		fmt.Println("Left the switchyard command unchanged;", executable, "is missing")
		return nil
	}
	link := cliLink()
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		fmt.Println("Run this release's CLI from", executable)
		return nil
	}
	target, err := filepath.EvalSymlinks(link)
	if _, buildErr := readBuild(filepath.Join(filepath.Dir(target), "BUILD.txt")); err != nil || buildErr != nil {
		fmt.Println("Left", link, "unchanged; run this release's CLI from", executable)
		return nil
	}
	if current, err := filepath.EvalSymlinks(executable); err == nil && current == target {
		return nil
	}
	temporary := link + ".upgrade"
	_ = os.Remove(temporary)
	if err := os.Symlink(executable, temporary); err == nil {
		err = os.Rename(temporary, link)
	}
	if err != nil {
		return fmt.Errorf("could not point %s at %s: %w", link, executable, err)
	}
	fmt.Println("Pointed", link, "at", executable)
	return nil
}

func parseVersion(name string) ([]int, bool) {
	match := releaseVersion.FindStringSubmatch(name)
	if match == nil {
		return nil, false
	}
	version := make([]int, 3)
	for i, part := range match[1:] {
		if _, err := fmt.Sscan(part, &version[i]); err != nil {
			return nil, false
		}
	}
	return version, true
}

// pruneReleases removes bundles in the releases directory whose version
// directory names are older than the previous release's, each after its runner
// runtimes, and returns their builds. Bundles it cannot place by version are
// kept.
func pruneReleases(bundle, previous string) []string {
	releases := filepath.Dir(filepath.Dir(bundle))
	if filepath.Base(releases) != "releases" || previous == "" || filepath.Dir(filepath.Dir(previous)) != releases {
		return nil
	}
	cutoff, ok := parseVersion(filepath.Base(filepath.Dir(previous)))
	if !ok {
		return nil
	}
	entries, err := os.ReadDir(releases)
	if err != nil {
		return nil
	}
	target := filepath.Base(filepath.Dir(bundle))
	var removed []string
	for _, entry := range entries {
		version, ok := parseVersion(entry.Name())
		if !entry.IsDir() || !ok || slices.Compare(version, cutoff) >= 0 || entry.Name() == target {
			continue
		}
		build, err := readBuild(filepath.Join(releases, entry.Name(), "switchyard/BUILD.txt"))
		if err != nil {
			continue
		}
		// The bundle's build record is the only link to its runtime, so the
		// bundle stays until its runtimes are gone and a later prune can retry.
		if !pruneRuntimes(build) {
			fmt.Println("Kept older release", entry.Name()+" until its runner runtime can be removed")
			continue
		}
		if err := os.RemoveAll(filepath.Join(releases, entry.Name())); err == nil {
			fmt.Println("Removed older release", entry.Name())
			removed = append(removed, build)
		}
	}
	return removed
}

// pruneRuntimes removes the extracted runner runtimes of the given builds and
// reports whether none of them remains.
func pruneRuntimes(builds ...string) bool {
	if len(builds) == 0 {
		return true
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		data = filepath.Join(home, ".local/share")
	}
	cache := filepath.Join(data, ".burrito")
	entries, err := os.ReadDir(cache)
	if errors.Is(err, os.ErrNotExist) {
		return true
	} else if err != nil {
		return false
	}
	removed := true
	running := runningExecutables()
	for _, entry := range entries {
		match := runtimeName.FindStringSubmatch(entry.Name())
		if !entry.IsDir() || match == nil || !slices.Contains(builds, match[1]) {
			continue
		}
		// The cache is shared by every installation of this account. A runner
		// still running from a runtime keeps it; a stopped one extracts it again.
		dir := filepath.Join(cache, entry.Name())
		// /proc reports executables with links resolved.
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			removed = false
			continue
		}
		if slices.ContainsFunc(running, func(exe string) bool { return strings.HasPrefix(exe, resolved+string(filepath.Separator)) }) {
			fmt.Println("Kept runner runtime", entry.Name()+", which a running process still uses")
			removed = false
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			removed = false
		} else {
			fmt.Println("Removed older runner runtime", entry.Name())
		}
	}
	return removed
}

// runningExecutables lists the executables of this account's running
// processes, as far as /proc shows them.
func runningExecutables() []string {
	procs, _ := filepath.Glob("/proc/[0-9]*/exe")
	var executables []string
	for _, proc := range procs {
		if exe, err := os.Readlink(proc); err == nil {
			executables = append(executables, strings.TrimSuffix(exe, " (deleted)"))
		}
	}
	return executables
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func short(build string) string {
	if len(build) < 7 {
		return "an unrecorded build"
	}
	return build[:7]
}

func writeFile(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".upgrade-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(data)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
