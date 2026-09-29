package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkflowUsesACPOnlyWithACommand(t *testing.T) {
	const front = "---\ntracker:\n  kind: plane\n  provider:\n    workspace: demo\n"
	for name, test := range map[string]struct {
		yaml string
		want bool
	}{
		"codex":   {"", false},
		"blank":   {"acp:\n  command: \" \"\n", false},
		"no-cmd":  {"acp:\n  read_timeout_ms: 1000\n", false},
		"command": {"acp:\n  command: '\"$SWITCHYARD_ROOT/scripts/claude-runner\" acp'\n", true},
	} {
		path := filepath.Join(t.TempDir(), "WORKFLOW.md")
		if err := os.WriteFile(path, []byte(front+test.yaml+"---\nPrompt\n"), 0600); err != nil {
			t.Fatal(err)
		}
		w, err := readWorkflow(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := w.usesACP(); got != test.want {
			t.Errorf("%s: usesACP() = %v, want %v", name, got, test.want)
		}
	}
}
