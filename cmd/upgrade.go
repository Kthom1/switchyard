package cmd

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Kthom1/switchyard/core"
)

func upgrade(a core.Installation, args []string) error {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)
	wait := flags.Bool("wait", false, "wait for running or retrying tasks to finish instead of refusing")
	keepBackup := flags.Bool("keep-backup", false, "keep the pre-upgrade backup after a successful upgrade")
	flags.Usage = func() {
		fmt.Println("Usage: switchyard upgrade [--wait] [--keep-backup]\n\nRun from a newly extracted release to move this installation to that release.\nRefuses a hand-edited runner service or running or retrying tasks, stops the\nrunner, backs up, replaces release-owned files (keeping customized ones and\nwriting the release copy beside them as .new), restarts, verifies, and records the\nrelease; on failure it restores the previous release. After a successful upgrade\nit deletes the backup, points ~/.local/bin/switchyard at this release, and removes\nreleases with older version numbers than the previous one, with their runtimes.\nAfter an interruption, run it again from any release: it first finishes or undoes\nthe interrupted upgrade.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: switchyard upgrade [--wait] [--keep-backup]")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if executable, err = filepath.EvalSymlinks(executable); err != nil {
		return err
	}
	return a.Upgrade(core.UpgradeOptions{Bundle: filepath.Dir(executable), Wait: *wait, KeepBackup: *keepBackup})
}
