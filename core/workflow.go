package core

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Project struct {
	ID         string `yaml:"project_id" json:"project_id"`
	Identifier string `yaml:"project_identifier" json:"project_identifier"`
	Repo       string `yaml:"repo" json:"repo"`
}

type workflow struct {
	document yaml.Node
	provider *yaml.Node
	body     string
	original []byte
}

// Keep the prompt byte-for-byte and edit the YAML tree so custom settings survive.
func readWorkflow(path string) (*workflow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	first, rest, ok := strings.Cut(string(data), "\n")
	if !ok || strings.TrimSpace(first) != "---" {
		return nil, errors.New("WORKFLOW.md must start with YAML front matter")
	}
	end := 0
	for {
		line, remaining, found := strings.Cut(rest[end:], "\n")
		if strings.TrimSpace(line) == "---" {
			var node yaml.Node
			if err := yaml.Unmarshal([]byte(rest[:end]), &node); err != nil {
				return nil, fmt.Errorf("invalid workflow YAML: %w", err)
			}
			var values map[string]any
			if err := node.Decode(&values); err != nil {
				return nil, fmt.Errorf("invalid workflow YAML: %w", err)
			}
			if len(node.Content) != 1 {
				return nil, errors.New("workflow must contain one YAML document")
			}
			tracker := field(node.Content[0], "tracker")
			provider := field(tracker, "provider")
			if tracker == nil || field(tracker, "kind") == nil || field(tracker, "kind").Value != "plane" || provider == nil || provider.Kind != yaml.MappingNode {
				return nil, errors.New("workflow must configure a Plane tracker provider")
			}
			return &workflow{document: node, provider: provider, body: remaining, original: data}, nil
		}
		if !found {
			return nil, errors.New("workflow YAML closing delimiter is missing")
		}
		end += len(line) + 1
	}
}

func field(node *yaml.Node, name string) *yaml.Node {
	if node != nil && node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			if node.Content[i].Value == name {
				return node.Content[i+1]
			}
		}
	}
	return nil
}

func setField(node *yaml.Node, name string, value any) error {
	if node == nil || node.Kind != yaml.MappingNode {
		return fmt.Errorf("workflow mapping missing for %s", name)
	}
	var encoded yaml.Node
	if err := encoded.Encode(value); err != nil {
		return err
	}
	if existing := field(node, name); existing != nil {
		*existing = encoded
	} else {
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, &encoded)
	}
	return nil
}

func removeField(node *yaml.Node, name string) {
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			node.Content = append(node.Content[:i], node.Content[i+2:]...)
			return
		}
	}
}

func (w *workflow) bytes() ([]byte, error) {
	var output bytes.Buffer
	output.WriteString("---\n")
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(&w.document); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	output.WriteString("---\n")
	output.WriteString(w.body)
	return output.Bytes(), nil
}

func (a Installation) workflowProjects(w *workflow) ([]Project, error) {
	if projects := field(w.provider, "projects"); projects != nil {
		var values []Project
		if projects.Kind != yaml.SequenceNode || projects.Decode(&values) != nil {
			return nil, errors.New("workflow projects must be a list of project mappings")
		}
		ids, identifiers := map[string]bool{}, map[string]bool{}
		for _, project := range values {
			options := ProjectOptions{Repo: project.Repo, Project: project.ID, Identifier: project.Identifier}
			if err := options.validate(); err != nil || project.ID == "" || options.Project != project.ID || ids[project.ID] || identifiers[project.Identifier] {
				return nil, errors.New("workflow contains invalid or duplicate project mappings; use canonical lowercase project UUIDs")
			}
			ids[project.ID], identifiers[project.Identifier] = true, true
		}
		return values, nil
	}
	// Existing installations keep their old environment until explicitly expanded.
	id, identifier := field(w.provider, "project_id"), field(w.provider, "project_identifier")
	if id == nil || identifier == nil {
		return nil, errors.New("workflow has no project mapping")
	}
	repo, err := a.runnerVariable("SOURCE_REPO_URL")
	if err != nil {
		return nil, err
	}
	if repo == "" {
		return nil, errors.New("legacy workflow is missing SOURCE_REPO_URL")
	}
	options := ProjectOptions{Repo: repo, Project: id.Value, Identifier: identifier.Value}
	if err := options.validate(); err != nil {
		return nil, fmt.Errorf("invalid legacy project mapping: %w", err)
	}
	if options.Project != id.Value {
		return nil, errors.New("legacy workflow must use a canonical lowercase project UUID")
	}
	return []Project{{ID: id.Value, Identifier: identifier.Value, Repo: repo}}, nil
}

func (a Installation) runnerVariable(name string) (string, error) {
	command := a.command("bash", "-p", "-c", `source "$1"; name=$2; printf '%s' "${!name}"`, "switchyard-config", filepath.Join(a.Root, ".env"), name)
	data, err := command.Output()
	if err != nil {
		return "", errors.New("could not read the saved runner environment")
	}
	return string(data), nil
}

func (w *workflow) save(path string) error {
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, w.original) {
		return errors.New("WORKFLOW.md changed while connecting the project; rerun the command")
	}
	data, err := w.bytes()
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".workflow-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), path)
}
