package core

import (
	"os"
	"path/filepath"
)

// Resolve explicit homes before commands change directory to the installation.
func codexEnvironment() (name, value string, err error) {
	name, value = "CODEX_HOME", os.Getenv("CODEX_HOME")
	if value == "" {
		name, value = "SWITCHYARD_CODEX_HOME", os.Getenv("SWITCHYARD_CODEX_HOME")
	}
	if value == "" {
		return "", "", nil
	}
	value, err = filepath.Abs(value)
	return
}

func codexHome() (string, error) {
	_, home, err := codexEnvironment()
	if err != nil || home != "" {
		return home, err
	}
	home, err = os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(home, ".codex"))
}
