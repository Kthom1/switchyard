package cmd

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kthom1/switchyard/config"
	"github.com/Kthom1/switchyard/core"
)

func initialize(a *core.Installation, args []string) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)
	port := flags.Int("port", 8090, "local Plane port")
	runnerPort := flags.Int("runner-port", 8091, "local runner dashboard port")
	webURL := flags.String("web-url", "", "browser origin (default http://localhost:<port>)")
	runner := flags.String("runner", "", "path to bin/symphony from a runner release bundle")
	recommended := flags.Bool("recommended", false, "skip the walkthrough and install the recommended plugins and skills")
	flags.Bool("clean", false, "skip the walkthrough without installing optional plugins or skills")
	flags.Usage = func() {
		fmt.Fprint(os.Stdout, "Usage: switchyard init [options]\n\nSet up this installation and its local board. In a terminal, init walks through\nsetup with Recommended selected by default. Use --recommended or --clean to\nskip the walkthrough. Fresh setup creates local Plane accounts and an\nunconnected project; login details are saved in the private plane.json file.\nExisting board data and accounts are preserved.\n\nConnect repositories afterward with: switchyard project add --repo URL\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected arguments; run switchyard init --help")
	}
	var recommendedSet, cleanSet bool
	flags.Visit(func(f *flag.Flag) {
		recommendedSet = recommendedSet || f.Name == "recommended"
		cleanSet = cleanSet || f.Name == "clean"
	})
	if recommendedSet && cleanSet {
		return errors.New("choose either --recommended or --clean")
	}
	interactive := !recommendedSet && !cleanSet && interactiveTerminal()
	if *port < 1 || *port > 65535 || *runnerPort < 1 || *runnerPort > 65535 || *port == *runnerPort {
		return errors.New("choose two different ports between 1 and 65535")
	}
	if *webURL == "" {
		*webURL = fmt.Sprintf("http://localhost:%d", *port)
	}
	a.Settings = config.Settings{Port: *port, RunnerPort: *runnerPort, WebURL: *webURL}
	if _, err := os.Stat(filepath.Join(a.Root, "config.json")); err == nil {
		a.Settings, err = config.Load(a.Root)
		if err != nil {
			return err
		}
		var conflict string
		flags.Visit(func(f *flag.Flag) {
			if (f.Name == "port" && *port != a.Settings.Port) || (f.Name == "runner-port" && *runnerPort != a.Settings.RunnerPort) || (f.Name == "web-url" && *webURL != a.Settings.WebURL) {
				conflict = f.Name
			}
		})
		if conflict != "" {
			return fmt.Errorf("--%s differs from this installation; existing configuration is preserved", conflict)
		}
	}
	a.Settings.WebURL = strings.TrimRight(a.Settings.WebURL, "/")
	if err := a.Settings.Validate(); err != nil {
		return err
	}
	if err := a.Init(*runner); err != nil {
		return err
	}
	if interactive {
		var err error
		*recommended, err = chooseSetup(bufio.NewReader(os.Stdin))
		if err != nil {
			return err
		}
	}
	if *recommended {
		fmt.Println("Installing Ponytail, Compound Engineering, Frontend Design, and ShowMe for your configured Codex. See", filepath.Join(a.Root, "docs/recommended.md"), "for skill discovery and Ponytail hook controls.")
		if err := a.InstallRecommended(); err != nil {
			return err
		}
	}
	fmt.Println("Board:", a.Settings.WebURL)
	if _, err := os.Stat(filepath.Join(a.Root, "plane.json")); err == nil {
		fmt.Println("Local board login:", filepath.Join(a.Root, "plane.json"), "(private file; owner_email and owner_password)")
	}
	if !a.RunnerConfigured() {
		if _, err := os.Stat(filepath.Join(a.Root, "plane.json")); os.IsNotExist(err) {
			fmt.Println("Existing Plane accounts are preserved. For the first connection, also supply --workspace, --project-id, --identifier and --api-key-stdin.")
		}
	}
	fmt.Println("Connect a repository with: switchyard project add --repo REPOSITORY_URL")
	return nil
}
