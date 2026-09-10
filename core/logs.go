package core

import "github.com/Kthom1/switchyard/config"

func (a Installation) Logs(target string, follow bool) error {
	if target == "plane" {
		flags := []string{"logs", "--tail", "100"}
		if follow {
			flags = append(flags, "--follow")
		}
		return a.compose(flags...)
	}
	flags := []string{"--user", "-u", config.Name(a.Root) + ".service", "--no-pager", "-n", "100"}
	if follow {
		flags = append(flags, "--follow")
	}
	return a.run("journalctl", flags...)
}
