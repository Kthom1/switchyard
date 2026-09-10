package cmd

import (
	"errors"

	"github.com/Kthom1/switchyard/core"
)

func logs(a core.Installation, args []string) error {
	target := "runner"
	follow := false
	for _, arg := range args {
		switch arg {
		case "plane", "runner":
			target = arg
		case "--follow", "-f":
			follow = true
		default:
			return errors.New("usage: switchyard logs [plane|runner] [--follow]")
		}
	}
	return a.Logs(target, follow)
}
