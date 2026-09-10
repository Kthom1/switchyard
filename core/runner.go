package core

import (
	"errors"
	"fmt"
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
	}
	return nil
}
