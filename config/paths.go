package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func BaseDir() (string, error) {
	root := os.Getenv("SWITCHYARD_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".switchyard")
	}
	if !filepath.IsAbs(root) {
		return "", errors.New("SWITCHYARD_HOME must be an absolute path")
	}
	if strings.ContainsAny(root, "\n\r") {
		return "", errors.New("SWITCHYARD_HOME cannot contain line breaks")
	}
	root = filepath.Clean(root)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return root, nil
}

func Name(root string) string {
	sum := sha256.Sum256([]byte(root))
	return fmt.Sprintf("switchyard-%x", sum[:6])
}

func UnitPath(root string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config/systemd/user", Name(root)+".service"), nil
}
