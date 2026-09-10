package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"runtime"

	"github.com/Kthom1/switchyard/config"
	"github.com/Kthom1/switchyard/core"
)

const help = `Switchyard: a task board and native Codex runner.

Usage: switchyard <command> [options]

  init     Set up this installation and its local board (init --help)
  project  Add or list project-to-repository connections (project add --help)
  up       Start Plane and the runner; wait until both are ready
  status   Show service health and local URLs
  logs     Show recent logs (logs [plane|runner] [--follow])
  down     Stop services; keep all data, configuration and task work
  backup   Stop writers and save database, attachments and configuration
  restore  Recover a trusted backup into an empty installation
  codex    Run your installed Codex with its existing configuration
  version  Print the build version

Run on Linux x86-64 with Docker Compose, Git, gh, Codex, Bash and a
systemd user session. Configuration and task work live in ~/.switchyard; override
with SWITCHYARD_HOME. Tasks and attachments live in Docker volumes. Backup saves
board data and private configuration; retain unfinished task workspaces separately.
`

func Execute(args []string, assets fs.FS, version string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(help)
		return nil
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Println(version)
		return nil
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return errors.New("this release supports Linux x86-64")
	}
	root, err := config.BaseDir()
	if err != nil {
		return err
	}
	app := core.Installation{Root: root, Assets: assets}
	if args[0] == "init" {
		return initialize(&app, args[1:])
	}
	if args[0] == "restore" {
		return restore(&app, args[1:])
	}
	app.Settings, err = config.Load(root)
	if err != nil {
		return err
	}
	switch args[0] {
	case "project":
		return projects(app, args[1:])
	case "up", "down", "status":
		if len(args) != 1 {
			return fmt.Errorf("usage: switchyard %s", args[0])
		}
		switch args[0] {
		case "up":
			return up(app)
		case "down":
			return down(app)
		default:
			return status(app)
		}
	case "backup":
		return backup(app, args[1:], version)
	case "logs":
		return logs(app, args[1:])
	case "codex":
		return app.Codex(args[1:])
	default:
		return fmt.Errorf("unknown command %q; run switchyard --help", args[0])
	}
}
