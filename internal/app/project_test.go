package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectCatalogueCLIAndLiveHook(t *testing.T) {
	root := t.TempDir()
	global := filepath.Join(root, "global.json")
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(project, ".lsops.json")
	run := func(path string, args ...string) string {
		t.Helper()
		code, out, err := invoke(append([]string{"--config", path}, args...)...)
		if code != 0 {
			t.Fatalf("%v: %d %s", args, code, err)
		}
		return out
	}
	run(global, "add", "sample", "global guidance")
	run(local, "add", "sample", "project guidance", "--why", "project reason")
	run(global, "project", "add", project)
	if out := run(global, "project", "list"); !strings.Contains(out, "project") {
		t.Fatal(out)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	render := []string{"hook", "render", "--harness", "claude", "--event", "SessionStart", "--mode", "output"}
	if out := run(global, render...); !strings.Contains(out, "project guidance") || !strings.Contains(out, "project reason") {
		t.Fatal(out)
	}
	run(local, "add", "sample", "fresh project guidance")
	if out := run(global, render...); !strings.Contains(out, "fresh project guidance") {
		t.Fatal(out)
	}
	if out := run(global, "--json"); !strings.Contains(out, "\"project_config\"") {
		t.Fatal(out)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if out := run(global, render...); !strings.Contains(out, "global guidance") || strings.Contains(out, "project reason") {
		t.Fatal(out)
	}
	run(global, "project", "remove", project)
	if out := run(global, "project", "list"); out != "" {
		t.Fatal(out)
	}
}
