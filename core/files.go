package core

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Publish complete files without replacing anything from an earlier installation.
func writeOnce(path string, data []byte, mode fs.FileMode) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	err := writeNew(path, data, mode)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	return err
}

// Refuse an existing file, including one created while this file was staged.
func writeNew(path string, data []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".install-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(data)
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Link(file.Name(), path)
}

func (a Installation) copyFiles(source fs.FS, directory string) error {
	return fs.WalkDir(source, directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(source, path)
		if err != nil {
			return err
		}
		mode := fs.FileMode(0600)
		if strings.HasPrefix(path, "scripts/") || path == "bin/symphony" {
			mode = 0700
		}
		return writeOnce(filepath.Join(a.Root, path), data, mode)
	})
}
