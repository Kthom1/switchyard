package main

import (
	"fmt"
	"io/fs"
	"testing"

	"go.yaml.in/yaml/v3"
)

// Plane's daily cleanup runs in its workers, so they must receive the
// retention that keeps its API activity log from growing for two weeks.
func TestPlaneKeepsItsAPIActivityLogForOneDay(t *testing.T) {
	data, err := fs.ReadFile(assets, "deploy/docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			Environment map[string]any `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		t.Fatal(err)
	}
	for _, service := range []string{"api", "worker", "beat-worker"} {
		if got := fmt.Sprint(compose.Services[service].Environment["API_ACTIVITY_LOG_RETENTION_DAYS"]); got != "${API_ACTIVITY_LOG_RETENTION_DAYS:-1}" {
			t.Fatalf("%s retention = %q", service, got)
		}
	}
}
