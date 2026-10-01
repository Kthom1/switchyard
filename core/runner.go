package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func (a Installation) installRunner(runner string) error {
	if _, err := os.Stat(filepath.Join(a.Root, "bin/symphony")); errors.Is(err, os.ErrNotExist) {
		if runner == "" {
			binary, err := os.Executable()
			if err != nil {
				return err
			}
			binary, err = filepath.EvalSymlinks(binary)
			if err != nil {
				return err
			}
			runner = filepath.Join(filepath.Dir(binary), "bin/symphony")
		}
		source, err := filepath.Abs(runner)
		if err != nil {
			return err
		}
		bundle := os.DirFS(filepath.Dir(filepath.Dir(source)))
		if filepath.Base(source) != "symphony" || filepath.Base(filepath.Dir(source)) != "bin" {
			return errors.New("--runner must point to bin/symphony in an extracted release bundle")
		}
		if err := a.copyFiles(bundle, "licenses"); err != nil {
			return fmt.Errorf("runner release licenses: %w", err)
		}
		if err := a.copyFiles(bundle, "bin/symphony"); err != nil {
			return fmt.Errorf("extract the complete release archive beside switchyard, or use --runner: %w", err)
		}
		// The build record and manifest let switchyard upgrade identify this
		// release and tell its files from local changes later.
		if _, err := fs.Stat(bundle, "BUILD.txt"); err == nil {
			if err := a.copyFiles(bundle, "BUILD.txt"); err != nil {
				return err
			}
			if err := a.recordManifest(filepath.Dir(filepath.Dir(source))); err != nil {
				return err
			}
		}
	}
	return nil
}

// recordManifest records the shipped hash of every file this release installs,
// unless a manifest already exists.
func (a Installation) recordManifest(bundle string) error {
	build, err := readBuild(filepath.Join(bundle, "BUILD.txt"))
	if err != nil {
		return err
	}
	files, err := a.releaseFiles(bundle)
	if err != nil {
		return err
	}
	manifest, err := releaseManifestData(build, files)
	if err != nil {
		return err
	}
	return writeOnce(filepath.Join(a.Root, manifestName), manifest, 0600)
}
