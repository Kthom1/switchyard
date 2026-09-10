package cmd

import (
	"fmt"

	"github.com/Kthom1/switchyard/core"
)

func up(a core.Installation) error {
	if err := a.Up(); err != nil {
		return err
	}
	fmt.Println("Board:", a.Settings.WebURL)
	fmt.Printf("Runner: http://localhost:%d\n", a.Settings.RunnerPort)
	return nil
}
