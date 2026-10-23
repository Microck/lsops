package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestProjectSelectionAndIsolation(t *testing.T) {
	root := t.TempDir()
	global := filepath.Join(root, "global.json")
	project := filepath.Join(root, "project")
	nested := filepath.Join(project, "nested")
	sibling := filepath.Join(root, "project-other")
	for _, dir := range []string{nested, sibling} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(dir, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, ProjectFile), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(project, `{"tools":{"global":{"description":"project version","why":"local reason"},"hidden":{"hide":true},"local":{"description":"local only"}}}`)
	write(nested, `{"tools":{"nested":{"description":"nested only"}}}`)
	if err := SetTool(global, "global", "global version", ""); err != nil {
		t.Fatal(err)
	}
	if err := SetTool(global, "hidden", "global visible", ""); err != nil {
		t.Fatal(err)
	}
	base, _ := Load(global)
	got, path, err := ForDirectory(base, nested)
	if err != nil || path != "" || got.Tools["global"].Description != "global version" {
		t.Fatalf("untrusted overlay loaded: %+v %s %v", got, path, err)
	}
	if err := SetProject(global, project, true); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(global)
	if err := SetProject(global, project, true); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(global)
	if string(before) != string(after) {
		t.Fatal("non-idempotent registration")
	}
	base, _ = Load(global)
	got, path, err = ForDirectory(base, nested)
	if err != nil || path == "" || got.Tools["global"].Description != "project version" || !got.Tools["hidden"].Hide || got.Tools["local"].Description == "" {
		t.Fatalf("overlay failed: %+v %s %v", got, path, err)
	}
	if _, ok := got.Tools["nested"]; ok {
		t.Fatal("unregistered nested overlay loaded")
	}
	if base.Tools["global"].Description != "global version" {
		t.Fatal("base mutated")
	}
	got, path, err = ForDirectory(base, sibling)
	if err != nil || path != "" || got.Tools["global"].Description != "global version" {
		t.Fatal("prefix sibling selected")
	}
	if err := SetProject(global, nested, true); err != nil {
		t.Fatal(err)
	}
	base, _ = Load(global)
	got, _, err = ForDirectory(base, nested)
	if err != nil || got.Tools["nested"].Description == "" || got.Tools["global"].Description != "global version" {
		t.Fatal("nearest registered root not selected")
	}
	if err := SetProject(global, nested, false); err != nil {
		t.Fatal(err)
	}
	if err := SetProject(global, project, false); err != nil {
		t.Fatal(err)
	}
	base, _ = Load(global)
	if len(base.Projects) != 0 {
		t.Fatal("remove failed")
	}
}

func TestProjectInvalidAndSymlinkedCatalogues(t *testing.T) {
	root := t.TempDir()
	global := filepath.Join(t.TempDir(), "global.json")
	if err := SetProject(global, root, true); err == nil {
		t.Fatal("accepted missing local file")
	}
	path := filepath.Join(root, ProjectFile)
	if err := os.WriteFile(path, []byte(`{"tools":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SetProject(global, root, true); err != nil {
		t.Fatal(err)
	}
	base, _ := Load(global)
	if err := os.WriteFile(path, []byte("{invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ForDirectory(base, root); err == nil {
		t.Fatal("malformed overlay silently ignored")
	}
	if runtime.GOOS == "windows" {
		return
	} // Symlink privileges vary on Windows.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(global, path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ForDirectory(base, root); err == nil {
		t.Fatal("symlinked overlay accepted")
	}
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	_, selected, err := ForDirectory(base, link)
	if err != nil || selected != "" {
		t.Fatalf("cwd symlink escaped root: %s %v", selected, err)
	}
}
