package core

import "github.com/Kthom1/switchyard/config"

func (a Installation) Down() error {
	path, err := config.UnitPath(a.Root)
	if err != nil {
		return err
	}
	if exists(path) {
		if err := a.run("systemctl", "--user", "disable", "--now", config.Name(a.Root)+".service"); err != nil {
			return err
		}
	}
	return a.compose("down")
}
