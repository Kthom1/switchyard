package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func (a Installation) installSkill(name, repo, ref, sourcePath, license string) error {
	codex, err := codexHome()
	if err != nil {
		return err
	}
	target := filepath.Join(codex, "skills", name)
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	for _, path := range []string{target, filepath.Join(home, ".agents/skills", name)} {
		if info, err := os.Stat(filepath.Join(path, "SKILL.md")); err == nil && info.Mode().IsRegular() {
			fmt.Println("Keeping existing", name, "at", path)
			return nil
		}
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("skill path already exists: %s", target)
		}
		return err
	}
	work := filepath.Join(a.Root, "work")
	if err := os.MkdirAll(work, 0700); err != nil {
		return err
	}
	checkout, err := os.MkdirTemp(work, name+"-source-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(checkout)
	checkoutArgs := []string{"-C", checkout, "-c", "core.hooksPath=/dev/null", "checkout", "--quiet", "FETCH_HEAD", "--", sourcePath}
	if license != "" {
		checkoutArgs = append(checkoutArgs, license)
		license = filepath.Join(checkout, license)
	}
	for _, args := range [][]string{
		{"-C", checkout, "init", "--quiet"},
		{"-C", checkout, "fetch", "--quiet", "--depth", "1", "https://github.com/" + repo + ".git", ref},
		checkoutArgs,
	} {
		if err := a.run("git", args...); err != nil {
			return err
		}
	}
	return copySkill(filepath.Join(checkout, sourcePath), target, license)
}

func copySkill(source, target, license string) error {
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("skill path already exists: %s", target)
		}
		return err
	}
	if info, err := os.Lstat(source); err != nil || !info.IsDir() {
		return errors.New("skill source must be a directory")
	}
	sourceFS := os.DirFS(source)
	if info, err := fs.Stat(sourceFS, "SKILL.md"); err != nil || !info.Mode().IsRegular() {
		return errors.New("skill source needs a regular SKILL.md file")
	}
	if err := fs.WalkDir(sourceFS, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported skill entry: %s", path)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(target), ".skill-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := os.CopyFS(stage, sourceFS); err != nil {
		return err
	}
	if license != "" {
		if info, err := os.Lstat(license); err != nil || !info.Mode().IsRegular() {
			return errors.New("skill license must be a regular file")
		}
		data, err := os.ReadFile(license)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(stage, "LICENSE"), data, 0644); err != nil {
			return err
		}
	}
	// Publish only after the complete skill and its license have been copied.
	return os.Rename(stage, target)
}
