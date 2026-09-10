package cmd

import (
	"bufio"
	"strings"
	"testing"
)

func TestChooseSetupDefaultsToRecommendedAndRetriesInvalidAnswers(t *testing.T) {
	for _, test := range []struct {
		name, input string
		want        bool
		wantError   bool
	}{
		{"default", "\nnext\n", true, false},
		{"recommended", " ReCoMmEnDeD \nnext\n", true, false},
		{"clean", "2\nnext\n", false, false},
		{"clean name", " CLEAN \nnext\n", false, false},
		{"invalid then clean", "maybe\n0\nclean\nnext\n", false, false},
		{"input closed", "", false, true},
		{"partial input closed", "1", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(test.input))
			got, err := chooseSetup(reader)
			if test.wantError {
				if err == nil || !strings.Contains(err.Error(), "rerun init to continue") {
					t.Fatalf("got %v, want resumable input error", err)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("got %v, %v; want %v", got, err, test.want)
			}
			if next, err := readAnswer(reader, ""); err != nil || next != "next" {
				t.Fatalf("setup choice consumed the next answer: %q, %v", next, err)
			}
		})
	}
}
