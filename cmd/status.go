package cmd

import (
	"fmt"

	"github.com/Kthom1/switchyard/core"
)

func status(a core.Installation) error {
	err := a.Status()
	fmt.Println("Board:", a.Settings.WebURL)
	fmt.Printf("Runner: http://localhost:%d\n", a.Settings.RunnerPort)
	return err
}
