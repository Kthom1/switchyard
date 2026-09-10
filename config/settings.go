package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

type Settings struct {
	Port       int    `json:"port"`
	RunnerPort int    `json:"runner_port"`
	WebURL     string `json:"web_url"`
}

func Load(root string) (Settings, error) {
	var settings Settings
	data, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		return settings, fmt.Errorf("run switchyard init first: %w", err)
	}
	if err = json.Unmarshal(data, &settings); err != nil {
		return settings, fmt.Errorf("invalid config.json: %w", err)
	}
	return settings, settings.Validate()
}

func (c Settings) Validate() error {
	if c.Port < 1 || c.Port > 65535 || c.RunnerPort < 1 || c.RunnerPort > 65535 || c.Port == c.RunnerPort {
		return errors.New("choose two different ports between 1 and 65535")
	}
	u, err := url.Parse(c.WebURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.IndexFunc(c.WebURL, unicode.IsSpace) >= 0 {
		return errors.New("--web-url must be an HTTP(S) origin without credentials or a path")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return errors.New("invalid --web-url port")
		}
	}
	return nil
}
