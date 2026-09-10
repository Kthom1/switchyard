package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapPlanePreservesCredentialsAfterFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name, value string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), mode); err != nil {
			t.Fatal(err)
		}
	}
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	write("scripts/bootstrap-plane.py", "# fixed provisioning code", 0600)
	write("scripts/plane", "#!/bin/sh\n/bin/cat > received.json\nprintf '%s\\n' \"$@\" > arguments\ntest ! -f fail\n", 0700)
	write("fail", "", 0600)
	a := Installation{Root: root}
	if _, err := a.bootstrapPlane(); err == nil {
		t.Fatal("failed provisioning must report failure")
	}
	saved := read("plane.json")
	if saved != read("received.json") {
		t.Fatal("provisioning must receive the saved identity through stdin")
	}
	if err := os.Remove(filepath.Join(root, "fail")); err != nil {
		t.Fatal(err)
	}
	settings, err := a.bootstrapPlane()
	if err != nil {
		t.Fatal(err)
	}
	if read("plane.json") != saved || read("received.json") != saved {
		t.Fatal("retry must preserve the original credentials")
	}
	if len(settings.Password) != 64 || len(settings.APIKey) != len("plane_api_")+64 {
		t.Fatal("fresh credentials must contain independent random secrets")
	}
	for _, secret := range []string{settings.Password, settings.APIKey} {
		if strings.Contains(read("arguments"), secret) {
			t.Fatal("credentials must not appear in process arguments")
		}
	}
	if err := os.Chmod(filepath.Join(root, "plane.json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.bootstrapPlane(); err == nil {
		t.Fatal("setup must refuse publicly readable credentials")
	}
}
