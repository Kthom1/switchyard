package core

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

type ProjectOptions struct {
	Repo, Workspace, Project, Identifier string
	APIKeyStdin                          bool
	apiKey                               string
}

func (o *ProjectOptions) validate() error {
	if o.Workspace != "" || o.APIKeyStdin {
		if o.Repo == "" || o.Workspace == "" || o.Project == "" || o.Identifier == "" {
			return errors.New("provide --repo, --workspace, --project-id and --identifier together when connecting an existing Plane project")
		}
	}
	if o.Project != "" {
		if o.Repo == "" || o.Identifier == "" {
			return errors.New("provide --repo and --identifier with --project-id")
		}
		project := strings.ReplaceAll(strings.Trim(strings.TrimPrefix(o.Project, "urn:uuid:"), "{}"), "-", "")
		uuid, err := hex.DecodeString(project)
		if err != nil || len(uuid) != 16 {
			return errors.New("use a valid project UUID")
		}
		o.Project = fmt.Sprintf("%x-%x-%x-%x-%x", uuid[:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:])
	}
	if o.Workspace != "" {
		if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(o.Workspace) {
			return errors.New("Workspace must be a Plane slug")
		}
	}
	if o.Identifier != "" {
		if o.Repo == "" {
			return errors.New("provide --repo with --identifier")
		}
		if !regexp.MustCompile(`^[A-Z0-9_-]+$`).MatchString(o.Identifier) {
			return errors.New("use the Plane project's uppercase short identifier")
		}
	}
	if o.Repo == "" {
		return nil
	}
	if strings.IndexFunc(o.Repo, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return errors.New("Repository URL must not contain whitespace or invalid characters")
	}
	if regexp.MustCompile(`^git@[A-Za-z0-9.-]+:[A-Za-z0-9_./-]+$`).MatchString(o.Repo) {
		return nil
	}
	source, err := url.Parse(o.Repo)
	if err != nil || source.Scheme != "https" {
		return errors.New("Repository URL must use HTTPS or git@host:path")
	}
	if source.Hostname() == "" || source.User != nil || source.RawQuery != "" || source.Fragment != "" {
		return errors.New("Repository URL must not embed credentials or a query")
	}
	return nil
}
