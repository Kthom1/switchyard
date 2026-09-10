package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const frontendRef = "41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f"
const showMeRef = "3c2629142c5d437428269b1b722b08c0b87f574d"

func (a Installation) InstallRecommended() error {
	var inventory struct {
		Installed []struct {
			ID string `json:"pluginId"`
		} `json:"installed"`
		Marketplaces []struct {
			Name string `json:"name"`
		} `json:"marketplaces"`
	}
	for _, args := range [][]string{{"plugin", "list", "--json"}, {"plugin", "marketplace", "list", "--json"}} {
		data, err := a.command(filepath.Join(a.Root, "scripts/codex-runner"), args...).Output()
		if err != nil {
			return fmt.Errorf("read configured Codex plugins: %w", err)
		}
		if err := json.Unmarshal(data, &inventory); err != nil {
			return fmt.Errorf("read configured Codex plugins: %w", err)
		}
	}
	if inventory.Installed == nil || inventory.Marketplaces == nil {
		return errors.New("Codex returned an unsupported plugin inventory")
	}
	installed, marketplaces := map[string]bool{}, map[string]bool{}
	for _, plugin := range inventory.Installed {
		installed[plugin.ID] = true
	}
	for _, marketplace := range inventory.Marketplaces {
		marketplaces[marketplace.Name] = true
	}
	for _, plugin := range []struct{ repo, ref, name string }{
		{"DietrichGebert/ponytail", "356918eba965ee1eac64bd3a7f0dd02108350de5", "ponytail@ponytail"},
		{"EveryInc/compound-engineering-plugin", "8df67793b9733d2220fa9a7fc37139931471af62", "compound-engineering@compound-engineering-plugin"},
	} {
		if installed[plugin.name] {
			fmt.Println("Keeping existing", plugin.name)
			continue
		}
		if !marketplaces[strings.SplitN(plugin.name, "@", 2)[1]] {
			if err := a.Codex([]string{"plugin", "marketplace", "add", plugin.repo, "--ref", plugin.ref}); err != nil {
				return err
			}
		}
		if err := a.Codex([]string{"plugin", "add", plugin.name}); err != nil {
			return err
		}
	}
	if err := a.installSkill("frontend-design", "anthropics/skills", frontendRef, "skills/frontend-design", ""); err != nil {
		return err
	}
	return a.installSkill("show-me", "humanlayer/skills", showMeRef, "plugins/show-me/skills/show-me", "LICENSE")
}
