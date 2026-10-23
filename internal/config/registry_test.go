package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSetToolRegistersWithoutPATHOrExistingExecutable(t *testing.T) {
	t.Setenv("PATH", "")
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	executablePath := filepath.Join(dir, "not-installed", "future-cli")
	if err := SetTool(path, "future-cli", "  Query a future internal service  ", executablePath); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Tool{Description: "Query a future internal service", Path: executablePath}); cfg.Tools["future-cli"] != want {
		t.Fatalf("registered tool = %#v, want %#v", cfg.Tools["future-cli"], want)
	}
	if _, err := os.Stat(executablePath); !os.IsNotExist(err) {
		t.Fatalf("registration touched executable path: %v", err)
	}
	if err := SetTool(path, "another-future-cli", "Another uninstalled command", ""); err != nil {
		t.Fatalf("registration without path failed: %v", err)
	}
}

func TestSetToolRequiresDescriptionWithoutChangingConfig(t *testing.T) {
	for _, description := range []string{"", "   ", "\u00a0"} {
		for _, method := range []string{"SetTool", "SetDescription"} {
			t.Run(method+"/"+description, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "config.json")
				original := `{"version":1,"tools":{"example":{"description":"Keep this"}}}`
				writeFixture(t, path, original)
				var err error
				if method == "SetTool" {
					err = SetTool(path, "example", description, "")
				} else {
					err = SetDescription(path, "example", description)
				}
				if err == nil || !strings.Contains(err.Error(), "description must not be empty") {
					t.Fatalf("empty description accepted or wrong error: %v", err)
				}
				if got := readFixture(t, path); got != original {
					t.Fatal("invalid registration changed the config")
				}
			})
		}
	}
	path := filepath.Join(t.TempDir(), "missing", "config.json")
	if err := SetTool(path, "example", "", ""); err == nil {
		t.Fatal("empty description accepted for a new config")
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("invalid registration created a directory: %v", err)
	}
}

func TestSetToolPreservesSettingsAndDescriptionAliasPreservesPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := Config{Version: 1, Tools: map[string]Tool{
		"example": {Description: "Old", Path: "/old/bin/example", Help: "example help", Hide: true},
		"other":   {Description: "Other", Include: true},
	}}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, path, string(data))
	if err := SetTool(path, "example", "New", `C:\Tools\example.exe`); err != nil {
		t.Fatal(err)
	}
	want := Config{Version: 1, Tools: map[string]Tool{
		"example": {Description: "New", Path: `C:\Tools\example.exe`, Help: "example help", Hide: true},
		"other":   {Description: "Other", Include: true},
	}}
	cfg, err := Load(path)
	if err != nil || !reflect.DeepEqual(cfg, want) {
		t.Fatalf("updated config = %#v, err=%v; want %#v", cfg, err, want)
	}
	if err := SetTool(path, "example", "Updated again", ""); err != nil {
		t.Fatal(err)
	}
	if err := SetDescription(path, "example", "Changed with alias"); err != nil {
		t.Fatal(err)
	}
	want.Tools["example"] = Tool{Description: "Changed with alias", Path: `C:\Tools\example.exe`, Help: "example help", Hide: true}
	cfg, err = Load(path)
	if err != nil || !reflect.DeepEqual(cfg, want) {
		t.Fatalf("description alias changed other settings: %#v, err=%v", cfg, err)
	}
}

func TestLoadRetainsLegacyEntriesWithoutRewriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{
  "tools": {
    "described": {"description":"An existing description"},
    "legacy-hidden": {"hide":true},
    "legacy-included": {"include":true},
    "legacy-help": {"help":"legacy-help usage"}
  }
}`
	writeFixture(t, path, original)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != 1 || len(cfg.Tools) != 4 {
		t.Fatalf("legacy records lost: %#v", cfg)
	}
	if got := readFixture(t, path); got != original {
		t.Fatal("loading legacy config rewrote it")
	}
	if err := SetTool(path, "new-command", "New registration", ""); err != nil {
		t.Fatal(err)
	}
	updated, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 1 || len(updated.Tools) != 5 {
		t.Fatalf("legacy records lost on edit: %#v", updated)
	}
	for name, want := range cfg.Tools {
		if updated.Tools[name] != want {
			t.Fatalf("legacy record %q changed: %#v", name, updated.Tools[name])
		}
	}
}

func TestLoadAndSetToolAcceptPortableAbsolutePaths(t *testing.T) {
	paths := []string{
		"/opt/tools/example",
		"/some path/'$literal'/example ",
		`C:\Program Files\Example\example.exe`,
		`c:/Tools/Example/example.exe`,
		`\\server\share\tools\example.exe`,
		`\\?\C:\Tools\example.exe`,
		`\\?\UNC\server\share\tools\example.exe`,
	}
	for _, executablePath := range paths {
		t.Run(executablePath, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := SetTool(path, "example", "A registered tool", executablePath); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Tools["example"].Path; got != executablePath {
				t.Fatalf("literal path changed: got %q, want %q", got, executablePath)
			}
		})
	}
}

func TestLoadAndSetToolRejectInvalidPaths(t *testing.T) {
	paths := map[string]string{
		"relative command":         "example",
		"relative directory":       "bin/example",
		"dot relative":             "./bin/example",
		"parent relative":          "../example",
		"home expansion":           "~/bin/example",
		"environment expansion":    "$HOME/bin/example",
		"blank":                    " ",
		"drive only":               "C:",
		"drive relative":           `C:example.exe`,
		"drive root relative":      `\example.exe`,
		"UNC server without share": `\\server`,
		"UNC empty share":          `\\server\`,
		"nul":                      "/bin/example\x00",
		"newline":                  "/bin/example\nnext",
		"line separator":           "/bin/example\u2028next",
		"overlong":                 "/" + strings.Repeat("a", 4096),
	}
	for name, executablePath := range paths {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			original := `{"version":1,"tools":{"example":{"description":"Keep this"}}}`
			writeFixture(t, path, original)
			if err := SetTool(path, "example", "New description", executablePath); err == nil {
				t.Fatalf("SetTool accepted invalid path %q", executablePath)
			}
			if got := readFixture(t, path); got != original {
				t.Fatal("invalid path update changed the original config")
			}
			data, err := json.Marshal(Config{Version: 1, Tools: map[string]Tool{
				"example": {Description: "A registered tool", Path: executablePath},
			}})
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, path, string(data))
			if _, err := Load(path); err == nil {
				t.Fatalf("Load accepted invalid path %q", executablePath)
			}
		})
	}
}

func TestPathLengthCountsCharactersAndInvalidUTF8IsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	executablePath := "/" + strings.Repeat("界", maxPathLength-1)
	if err := SetTool(path, "example", "A registered tool", executablePath); err != nil {
		t.Fatalf("exact character limit rejected: %v", err)
	}
	cfg, err := Load(path)
	if err != nil || cfg.Tools["example"].Path != executablePath {
		t.Fatalf("path did not round-trip: %v", err)
	}
	if err := SetTool(path, "example", "A registered tool", "/invalid/\xff"); err == nil {
		t.Fatal("invalid UTF-8 path accepted")
	}
}
