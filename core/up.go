package core

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Kthom1/switchyard/config"
)

func (a Installation) Up() error {
	if !a.RunnerConfigured() {
		return errors.New("connect a repository with switchyard project add first")
	}
	path, err := config.UnitPath(a.Root)
	if err != nil {
		return err
	}
	template, err := fs.ReadFile(a.Assets, "deploy/switchyard.service")
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	unitExists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	unit, home, err := a.runnerUnit(string(template), string(existing))
	if err != nil {
		return err
	}
	if unitExists && string(existing) != unit {
		return fmt.Errorf("service configuration differs at %s; stop the runner and remove that unit file to recreate it", path)
	}
	login := a.command(filepath.Join(a.Root, "scripts/codex-runner"), "login", "status")
	if home != "" {
		login.Env = append(login.Env, "CODEX_HOME="+home)
	}
	if err := login.Run(); err != nil {
		if home != "" {
			return fmt.Errorf("Codex is not signed in for CODEX_HOME %s; run codex login with that CODEX_HOME", home)
		}
		return errors.New("Codex is not signed in; run codex login (or switchyard codex login)")
	}
	if err := a.startBoard(); err != nil {
		return err
	}
	if err := writeOnce(path, []byte(unit), 0600); err != nil {
		return err
	}
	if err := a.run("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if err := a.command("systemctl", "--user", "is-active", "--quiet", config.Name(a.Root)+".service").Run(); err != nil {
		if err := checkPort("runner", a.Settings.RunnerPort); err != nil {
			return err
		}
	}
	if err := a.run("systemctl", "--user", "enable", "--now", config.Name(a.Root)+".service"); err != nil {
		return err
	}
	if err := waitHTTP(fmt.Sprintf("http://127.0.0.1:%d/api/v1/state", a.Settings.RunnerPort), time.Minute); err != nil {
		return fmt.Errorf("runner is not ready; run switchyard logs runner: %w", err)
	}
	if err := a.run("systemctl", "--user", "is-active", "--quiet", config.Name(a.Root)+".service"); err != nil {
		return fmt.Errorf("runner failed; run switchyard logs runner: %w", err)
	}
	return nil
}

// Preserve the service's selected home when up is called from another terminal.
func (a Installation) runnerUnit(template, existing string) (unit, home string, err error) {
	name, home, err := codexEnvironment()
	if err != nil {
		return "", "", err
	}
	if home == "" {
		home, err = codexHome()
		if err != nil {
			return "", "", err
		}
	}
	pathLine := "Environment=" + systemdQuote("PATH="+os.Getenv("PATH"))
	for _, line := range strings.Split(existing, "\n") {
		if strings.HasPrefix(line, `Environment="PATH=`) {
			pathLine = line
		}
		if name == "" && (strings.HasPrefix(line, `Environment="CODEX_HOME=`) || strings.HasPrefix(line, `Environment="SWITCHYARD_CODEX_HOME=`)) {
			value, decodeErr := strconv.Unquote(strings.TrimPrefix(line, "Environment="))
			if decodeErr != nil {
				return "", "", decodeErr
			}
			home = strings.ReplaceAll(strings.SplitN(value, "=", 2)[1], "%%", "%")
		}
	}
	// An explicit native value also overrides a different user-manager environment.
	pathLine += "\nEnvironment=" + systemdQuote("CODEX_HOME="+home)
	unit = strings.ReplaceAll(template, "@INSTALL_ROOT@", systemdQuote(a.Root)[1:len(systemdQuote(a.Root))-1])
	unit = strings.ReplaceAll(unit, "@WORKING_DIRECTORY@", strings.ReplaceAll(a.Root, "%", "%%")+"/.")
	unit = strings.Replace(unit, "Environment=PATH=%h/.local/share/mise/shims:%h/.local/bin:/usr/local/bin:/usr/bin:/bin", pathLine, 1)
	return unit, home, nil
}

func checkPort(service string, port int) error {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("%s port %d is already in use: %w", service, port, err)
	}
	return listener.Close()
}

func systemdQuote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "\n", `\n`, "\r", `\r`).Replace(value) + `"`
}
