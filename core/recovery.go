package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kthom1/switchyard/config"
)

// Backup stops writers and leaves only Postgres running, including on dump failure.
func (a Installation) Backup(directory, version string) error {
	if !a.RunnerConfigured() {
		return errors.New("connect a repository with switchyard project add before backing up")
	}
	if err := a.Down(); err != nil {
		return err
	}
	if err := a.compose("up", "-d", "plane-db"); err != nil {
		return err
	}
	args := []string{"SWITCHYARD_VERSION=" + version, "SWITCHYARD_CODEX_HOME=" + filepath.Join(a.Root, "work/codex"), filepath.Join(a.Root, "scripts/backup")}
	if directory != "" {
		args = append(args, directory)
	}
	return a.run("env", args...)
}

// Restore installs into an empty home; it never creates or starts a runner service.
func (a *Installation) Restore(backup, runner string) error {
	var err error
	backup, err = filepath.Abs(backup)
	if err != nil {
		return err
	}
	a.Settings, err = config.Load(filepath.Join(backup, "config"))
	if err != nil {
		return fmt.Errorf("backup must contain valid CLI config.json: %w", err)
	}
	entries, err := os.ReadDir(a.Root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(entries) != 0 {
		return errors.New("restore requires an empty SWITCHYARD_HOME; use a new destination")
	}
	if err := os.MkdirAll(a.Root, 0700); err != nil {
		return err
	}
	a.Root, err = filepath.EvalSymlinks(a.Root)
	if err != nil {
		return err
	}
	unit, err := config.UnitPath(a.Root)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(unit); err == nil {
		return errors.New("restore refuses an existing runner service; use a new destination or remove the old stopped service")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := a.copyFiles(a.Assets, "."); err != nil {
		return err
	}
	if err := a.installRunner(runner); err != nil {
		return err
	}
	taskRoot := filepath.Join(a.Root, "workspaces")
	check := a.command(filepath.Join(a.Root, "scripts/task-workspace"), "root")
	check.Env = append(check.Env, "SYMPHONY_WORKSPACE_ROOT="+taskRoot)
	if err := check.Run(); err != nil {
		return fmt.Errorf("restore workspace must be outside source checkouts: %w", err)
	}
	if err := a.run("env", "SWITCHYARD_CODEX_HOME="+filepath.Join(a.Root, "work/codex"), filepath.Join(a.Root, "scripts/restore"), backup); err != nil {
		return err
	}
	// Override only the generated workspace location; custom workflow paths need operator review.
	env, err := os.OpenFile(filepath.Join(a.Root, ".env"), os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(env, "\nSYMPHONY_WORKSPACE_ROOT='%s'\n", strings.ReplaceAll(taskRoot, "'", "'\"'\"'"))
	closeErr := env.Close()
	if err != nil {
		return err
	}
	return closeErr
}
