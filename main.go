package main

import (
	"fmt"
	"os"

	"github.com/Kthom1/switchyard/cmd"
)

var version = "dev"

func main() {
	if err := cmd.Execute(os.Args[1:], assets, version); err != nil {
		fmt.Fprintln(os.Stderr, "switchyard:", err)
		os.Exit(1)
	}
}
