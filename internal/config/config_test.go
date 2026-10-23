package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDefaultPath(t *testing.T) {
	t.Run("XDG override", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", dir)
		got, err := DefaultPath()
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(dir, "lsops", "config.json"); got != want {
			t.Fatalf("DefaultPath() = %q, want %q", got, want)
		}
	})
	t.Run("native default", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		dir, err := os.UserConfigDir()
		if err != nil {
			t.Skipf("native config directory unavailable: %v", err)
		}
		got, err := DefaultPath()
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(dir, "lsops", "config.json"); got != want {
			t.Fatalf("DefaultPath() = %q, want %q", got, want)
		}
	})
}

func TestLoadMissingDoesNotCreateAnything(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	path := filepath.Join(dir, "config.json")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != 1 || cfg.Tools == nil || len(cfg.Tools) != 0 {
		t.Fatalf("missing config = %#v", cfg)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("Load created a directory, stat error = %v", err)
	}
}

func TestLoadNormalizesTextAndPreservesSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFixture(t, path, `{
	  "tools": {
	    "parallel-cli": {"description":"  Search and research  ","help":"  parallel-cli --help  ","include":true},
	    "internal tool": {"hide":true}
	  }
	}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Version: 1, Tools: map[string]Tool{
		"parallel-cli":  {Description: "Search and research", Help: "parallel-cli --help", Include: true},
		"internal tool": {Hide: true},
	}}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("Load() = %#v, want %#v", cfg, want)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	tests := map[string]string{
		"empty file":               "",
		"malformed":                `{"tools":`,
		"second document":          `{} {}`,
		"trailing garbage":         `{} garbage`,
		"unknown top-level field":  `{"toools":{}}`,
		"unknown tool field":       `{"tools":{"rg":{"descripton":"Search"}}}`,
		"unsupported version":      `{"version":2}`,
		"explicit zero version":    `{"version":0}`,
		"null version":             `{"version":null}`,
		"string version":           `{"version":"1"}`,
		"null document":            `null`,
		"array document":           `[]`,
		"empty name":               `{"tools":{"":{}}}`,
		"blank name":               `{"tools":{"  ":{}}}`,
		"path name":                `{"tools":{"/usr/bin/rg":{}}}`,
		"backslash in name":        `{"tools":{"bin\\rg":{}}}`,
		"dot name":                 `{"tools":{"..":{}}}`,
		"newline in name":          `{"tools":{"tool\nnext":{}}}`,
		"nul in name":              `{"tools":{"tool\u0000":{}}}`,
		"hide and include":         `{"tools":{"rg":{"hide":true,"include":true}}}`,
		"description newline":      `{"tools":{"rg":{"description":"Search\ncode"}}}`,
		"description tab":          `{"tools":{"rg":{"description":"Search\tcode"}}}`,
		"leading newline":          `{"tools":{"rg":{"description":"\nSearch"}}}`,
		"help escape":              `{"tools":{"rg":{"help":"\u001b[31m"}}}`,
		"help line separator":      `{"tools":{"rg":{"help":"first\u2028second"}}}`,
		"help paragraph separator": `{"tools":{"rg":{"help":"first\u2029second"}}}`,
		"invalid UTF-8":            "{\"tools\":{\"rg\":{\"description\":\"\xff\"}}}",
		"long description":         `{"tools":{"rg":{"description":"` + strings.Repeat("x", 241) + `"}}}`,
		"long help":                `{"tools":{"rg":{"help":"` + strings.Repeat("x", 501) + `"}}}`,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			writeFixture(t, path, data)
			if _, err := Load(path); err == nil {
				t.Fatalf("Load accepted invalid configuration: %q", data)
			}
		})
	}
}

func TestLoadLimitsCharactersNotUTF8Bytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Config{Version: 1, Tools: map[string]Tool{
		"example": {Description: strings.Repeat("é", 240), Help: strings.Repeat("界", 500)},
	}}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, path, string(data))
	if _, err := Load(path); err != nil {
		t.Fatalf("valid character counts rejected: %v", err)
	}
}

func TestLoadFileSizeLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFixture(t, path, "{}"+strings.Repeat(" ", maxFileSize-2))
	if _, err := Load(path); err != nil {
		t.Fatalf("exactly 1 MiB rejected: %v", err)
	}
	writeFixture(t, path, "{}"+strings.Repeat(" ", maxFileSize-1))
	if _, err := Load(path); err == nil {
		t.Fatal("file exceeding 1 MiB accepted")
	}
}

func TestSetDescriptionPreservesOtherOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeFixture(t, path, `{"tools":{"parallel-cli":{"description":"old","help":"parallel-cli --help","include":true},"internal tool":{"description":"Keep me","hide":true}}}`)
	if err := SetDescription(path, "parallel-cli", "  Search the web  "); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Version: 1, Tools: map[string]Tool{
		"parallel-cli":  {Description: "Search the web", Help: "parallel-cli --help", Include: true},
		"internal tool": {Description: "Keep me", Hide: true},
	}}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("updated config = %#v, want %#v", cfg, want)
	}
	assertNoTemporaryFiles(t, dir)
}

func TestSetDescriptionCreatesPrivateConfig(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "lsops")
	path := filepath.Join(dir, "config.json")
	if err := SetDescription(path, "some tool", "A command with a space in its name"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools["some tool"].Description != "A command with a space in its name" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	if runtime.GOOS != "windows" {
		for path, want := range map[string]os.FileMode{path: 0600, dir: 0700, filepath.Dir(dir): 0700} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != want {
				t.Errorf("mode of %q = %04o, want %04o", path, got, want)
			}
		}
	}
	assertNoTemporaryFiles(t, dir)
}

func TestInvalidEditsLeaveExistingFileUntouched(t *testing.T) {
	valid := `{"version":1,"tools":{"rg":{"description":"original"}}}`
	tests := []struct {
		name     string
		original string
		edit     func(string) error
	}{
		{"malformed set", `{"tools":`, func(p string) error { return SetDescription(p, "rg", "new") }},
		{"malformed forget", `{"tools":`, func(p string) error { _, err := Forget(p, "rg"); return err }},
		{"unknown field", `{"tools":{},"extra":1}`, func(p string) error { return SetDescription(p, "rg", "new") }},
		{"invalid name", valid, func(p string) error { return SetDescription(p, "bin/rg", "new") }},
		{"invalid description", valid, func(p string) error { return SetDescription(p, "rg", "new\nline") }},
		{"overlong description", valid, func(p string) error { return SetDescription(p, "rg", strings.Repeat("x", 241)) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			writeFixture(t, path, test.original)
			if err := test.edit(path); err == nil {
				t.Fatal("edit unexpectedly succeeded")
			}
			if got := readFixture(t, path); got != test.original {
				t.Fatalf("failed edit changed original file: %q", got)
			}
			assertNoTemporaryFiles(t, dir)
		})
	}
}

func TestOversizedRewriteLeavesOriginalUntouched(t *testing.T) {
	// A compact, valid file can exceed the cap after pretty-printing. Refuse
	// that edit without losing any of the existing overrides.
	cfg := emptyConfig()
	for i := 0; i < 6000; i++ {
		cfg.Tools[fmt.Sprintf("tool-%04d", i)] = Tool{Description: strings.Repeat("x", 130)}
	}
	original, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(original) >= maxFileSize {
		t.Fatalf("test fixture is unexpectedly too large: %d", len(original))
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeFixture(t, path, string(original))
	if err := SetDescription(path, "new-tool", "new description"); err == nil {
		t.Fatal("oversized rewrite unexpectedly succeeded")
	}
	if got := readFixture(t, path); got != string(original) {
		t.Fatal("oversized rewrite changed the original file")
	}
	assertNoTemporaryFiles(t, dir)
}

func TestEditsRefuseSymlinkDestinations(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	link := filepath.Join(dir, "config.json")
	original := `{"version":1,"tools":{"rg":{"description":"original"}}}`
	writeFixture(t, target, original)
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Load(link); err != nil {
		t.Fatalf("reading a symlink should work: %v", err)
	}
	if err := SetDescription(link, "rg", "replacement"); err == nil {
		t.Fatal("SetDescription replaced a symlink")
	}
	if _, err := Forget(link, "rg"); err == nil {
		t.Fatal("Forget replaced a symlink")
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink changed: info=%v, err=%v", info, err)
	}
	if got := readFixture(t, target); got != original {
		t.Fatal("symlink target changed")
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := SetDescription(link, "rg", "replacement"); err == nil {
		t.Fatal("SetDescription replaced a dangling symlink")
	}
	assertNoTemporaryFiles(t, dir)
}

func TestForget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := `{"tools":{"rg":{"description":"remove"},"fd":{"description":"keep","help":"fd --help","include":true}}}`
	writeFixture(t, path, original)
	removed, err := Forget(path, "absent")
	if err != nil || removed {
		t.Fatalf("Forget absent = %v, %v", removed, err)
	}
	if got := readFixture(t, path); got != original {
		t.Fatal("forgetting absent entry rewrote file")
	}
	removed, err = Forget(path, "rg")
	if err != nil || !removed {
		t.Fatalf("Forget existing = %v, %v", removed, err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Version: 1, Tools: map[string]Tool{
		"fd": {Description: "keep", Help: "fd --help", Include: true},
	}}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("remaining config = %#v, want %#v", cfg, want)
	}
	removed, err = Forget(path, "fd")
	if err != nil || !removed {
		t.Fatalf("Forget last = %v, %v", removed, err)
	}
	cfg, err = Load(path)
	if err != nil || cfg.Tools == nil || len(cfg.Tools) != 0 || cfg.Version != 1 {
		t.Fatalf("empty remaining config = %#v, %v", cfg, err)
	}
	assertNoTemporaryFiles(t, dir)
}

func TestForgetMissingDoesNotCreateAnything(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	removed, err := Forget(filepath.Join(dir, "config.json"), "rg")
	if err != nil || removed {
		t.Fatalf("Forget missing = %v, %v", removed, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("Forget created a directory, stat error = %v", err)
	}
}

func assertNoTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".lsops-config-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files left behind: %v", matches)
	}
}
