package cmd

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/Kthom1/switchyard/core"
)

func projects(a core.Installation, args []string) error {
	if len(args) == 1 && args[0] == "list" {
		values, err := a.Projects()
		if err != nil {
			return err
		}
		if len(values) == 0 {
			fmt.Println("No repositories connected. Run: switchyard project add --repo URL")
			return nil
		}
		output := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(output, "PROJECT\tPLANE PROJECT ID\tREPOSITORY")
		for _, value := range values {
			fmt.Fprintf(output, "%s\t%s\t%s\n", value.Identifier, value.ID, value.Repo)
		}
		return output.Flush()
	}
	if len(args) == 0 || args[0] != "add" {
		return errors.New("usage: switchyard project list | project add --repo URL [--project-id UUID --identifier PREFIX]")
	}
	flags := flag.NewFlagSet("project add", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)
	var options core.ProjectOptions
	flags.StringVar(&options.Repo, "repo", "", "repository URL")
	flags.StringVar(&options.Workspace, "workspace", "", "Plane workspace slug for a manual first connection")
	flags.StringVar(&options.Project, "project-id", "", "connect an existing project in this board's workspace")
	flags.StringVar(&options.Identifier, "identifier", "", "project prefix (default: repository name); required with --project-id")
	flags.BoolVar(&options.APIKeyStdin, "api-key-stdin", false, "read the Plane API key from stdin instead of a hidden prompt for a manual first connection")
	flags.Usage = func() {
		fmt.Fprint(os.Stdout, "Usage: switchyard project add --repo URL [options]\n\nConnect a repository to a project on this installation's local board.\nUse --project-id and --identifier for an existing project. For the first\nconnection to a manually configured board, also provide --workspace; the\nPlane API key is prompted privately, or read with --api-key-stdin.\nProject access and its identifier are checked before saving. Later connections\nreuse the saved workspace and credential. Existing mappings are preserved.\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args[1:]); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected arguments; run switchyard project add --help")
	}
	if options.Repo != "" && options.Workspace != "" && options.Project != "" && options.Identifier != "" && !a.RunnerConfigured() && !options.APIKeyStdin && !interactiveTerminal() {
		return errors.New("use --api-key-stdin to supply the Plane API key for a scripted connection")
	}
	if err := a.AddProject(options); err != nil {
		return err
	}
	if err := projects(a, []string{"list"}); err != nil {
		return err
	}
	fmt.Println("The runner uses your configured Codex. Start it with: switchyard up")
	return nil
}
