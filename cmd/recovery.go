package cmd

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Kthom1/switchyard/core"
)

func backup(a core.Installation, args []string, version string) error {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)
	flags.Usage = func() {
		fmt.Println("Usage: switchyard backup [DIRECTORY]\n\nStops services, saves database, attachments and configuration, and leaves only\nPostgres running. Default directory: work/backups inside SWITCHYARD_HOME.\nRun switchyard up when you are ready to resume work.")
	}
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return err
	}
	if flags.NArg() > 1 {
		return errors.New("usage: switchyard backup [DIRECTORY]")
	}
	directory := flags.Arg(0)
	if directory != "" {
		var err error
		directory, err = filepath.Abs(directory)
		if err != nil {
			return err
		}
	}
	if err := a.Backup(directory, version); err != nil {
		return err
	}
	fmt.Println("Backup finished. Services remain stopped except Postgres. Run switchyard up to resume.")
	return nil
}

func restore(a *core.Installation, args []string) error {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)
	runner := flags.String("runner", "", "path to bin/symphony in the matching extracted release bundle")
	flags.Usage = func() {
		fmt.Println("Usage: SWITCHYARD_HOME=/empty/destination switchyard restore [--runner PATH] BACKUP_DIRECTORY\n\nRestore a trusted CLI backup into an empty destination using its matching release.\nOnly Postgres starts. Review the restored settings before starting Plane or the runner.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("usage: switchyard restore [--runner PATH] BACKUP_DIRECTORY")
	}
	if err := a.Restore(flags.Arg(0), *runner); err != nil {
		return err
	}
	fmt.Println("Restored to:", a.Root)
	fmt.Println("Review origins, ports and custom workflow paths before starting services. See", filepath.Join(a.Root, "docs/backup.md"))
	return nil
}
