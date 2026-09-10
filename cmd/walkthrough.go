package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func interactiveTerminal() bool {
	check := exec.Command("bash", "-p", "-c", "test -t 0 && test -t 1")
	check.Stdin, check.Stdout = os.Stdin, os.Stdout
	return check.Run() == nil
}

func chooseSetup(reader *bufio.Reader) (bool, error) {
	fmt.Println("\nChoose your setup:")
	fmt.Println("  1. Recommended: Ponytail, Compound Engineering, Frontend Design and ShowMe.")
	fmt.Println("     Skills for simpler code, planning and review, frontend design, and visual explanations.")
	fmt.Println("  2. Clean: no optional plugins or skills.")
	for {
		answer, err := readAnswer(reader, "Setup [1/2] (default 1): ")
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "2", "clean":
			return false, nil
		case "", "1", "recommended":
			return true, nil
		default:
			fmt.Println("Choose 1 for Recommended or 2 for Clean.")
		}
	}
}

func readAnswer(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("setup input ended; rerun init to continue: %w", err)
	}
	return strings.TrimSpace(answer), nil
}
