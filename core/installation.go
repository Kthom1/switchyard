package core

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/Kthom1/switchyard/config"
)

type Installation struct {
	Root     string
	Settings config.Settings
	Assets   fs.FS
}

func (a Installation) command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Dir = a.Root
	cmd.Env = append(os.Environ(), "SWITCHYARD_COMPOSE_PROJECT="+config.Name(a.Root), "SWITCHYARD_PLANE_ENV="+filepath.Join(a.Root, ".env.plane"), "SWITCHYARD_RUNNER_ENV="+filepath.Join(a.Root, ".env"), "SWITCHYARD_WORKFLOW="+filepath.Join(a.Root, "WORKFLOW.md"), "SWITCHYARD_PLANE_PORT="+strconv.Itoa(a.Settings.Port))
	if name, home, err := codexEnvironment(); err != nil {
		cmd.Err = err
	} else if name != "" {
		cmd.Env = append(cmd.Env, name+"="+home)
	}
	return cmd
}

func (a Installation) run(name string, args ...string) error {
	cmd := a.command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(name), err)
	}
	return nil
}

func (a Installation) script(name string, args ...string) error {
	return a.run(filepath.Join(a.Root, "scripts", name), args...)
}

func (a Installation) compose(args ...string) error { return a.script("plane", args...) }

func (a Installation) Codex(args []string) error {
	return a.script("codex-runner", args...)
}

func (a Installation) RunnerConfigured() bool {
	return exists(filepath.Join(a.Root, ".env")) && exists(filepath.Join(a.Root, "WORKFLOW.md"))
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }
