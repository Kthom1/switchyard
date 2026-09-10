package core

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func prerequisites() error {
	var missing []string
	for _, tool := range []string{"bash", "docker", "git", "gh", "codex", "systemctl", "journalctl"} {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("install prerequisites first: %s (Docker: https://docs.docker.com/engine/install/; Compose 2.24.4+)", strings.Join(missing, ", "))
	}
	return nil
}

func (a Installation) doctor() error {
	for _, args := range [][]string{{"docker", "compose", "version"}, {"docker", "info", "--format", "{{.ServerVersion}}"}, {"gh", "auth", "status"}} {
		if err := a.run(args[0], args[1:]...); err != nil {
			return err
		}
	}
	if err := a.command("systemctl", "--user", "show-environment").Run(); err != nil {
		return errors.New("a systemd user session is required; sign in on the runner machine and retry")
	}
	return nil
}
