package core

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode"
)

func readAPIKey(fromStdin bool) (string, error) {
	var key string
	if fromStdin {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		key = strings.TrimSuffix(line, "\n")
	} else {
		// Ignore inherited shell startup/tracing options; read -s restores terminal echo.
		prompt := exec.Command("bash", "-p", "-c", `IFS= read -r -s -p "Plane API key: " key </dev/tty || exit; printf '\n' >&2; printf '%s' "$key"`)
		prompt.Stderr = os.Stderr
		data, err := prompt.Output()
		if err != nil {
			return "", errors.New("could not read the API key from the terminal; use --api-key-stdin for scripted setup")
		}
		key = string(data)
	}
	if key == "" || strings.IndexFunc(key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return "", errors.New("supply a non-empty API key without whitespace or control characters")
	}
	return key, nil
}
