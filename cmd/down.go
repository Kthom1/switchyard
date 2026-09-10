package cmd

import (
	"fmt"

	"github.com/Kthom1/switchyard/core"
)

func down(a core.Installation) error {
	if err := a.Down(); err != nil {
		return err
	}
	fmt.Println("Stopped. Tasks, attachments, configuration and workspaces are preserved.")
	return nil
}
