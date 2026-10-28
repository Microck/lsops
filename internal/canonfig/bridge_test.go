package canonfig

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Microck/lsops/internal/config"
)

func cfg(tools map[string]config.Tool) config.Config { return config.Config{Version: 1, Tools: tools} }
func put(t *testing.T, p string, c config.Config) {
	t.Helper()
	b, e := encode(c)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func mustSync(t *testing.T, o Options) Result {
	t.Helper()
	r, e := Sync(o)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Conflicts) > 0 {
		t.Fatal(r.Conflicts)
	}
	return r
}
func fixture(t *testing.T) Options {
	d := t.TempDir()
	return Options{Config: filepath.Join(d, "local.json"), Bundle: filepath.Join(d, "shared.json"), Apply: true}
}
func TestSyncPreservesLocalAndLifecycle(t *testing.T) {
	o := fixture(t)
	local := config.Tool{Description: "mine", Path: filepath.Join(t.TempDir(), "local"), Help: "help"}
	put(t, o.Config, cfg(map[string]config.Tool{"local": local, "same": {Description: "same"}}))
	put(t, o.Bundle, cfg(map[string]config.Tool{"shared": {Description: "one", InstallURL: "https://www.npmjs.com/package/lsops"}, "same": {Description: "same"}}))
	preview := o
	preview.Apply = false
	r := mustSync(t, preview)
	if len(r.Added) != 1 || r.Applied {
		t.Fatal(r)
	}
	if _, e := os.Stat(o.Config + ".canonfig.json"); !errors.Is(e, os.ErrNotExist) {
		t.Fatal(e)
	}
	r = mustSync(t, o)
	if !r.Applied {
		t.Fatal(r)
	}
	c, e := config.Load(o.Config)
	if e != nil || c.Tools["local"] != local {
		t.Fatal(c, e)
	}
	// Local paths, guidance outside portable metadata and project trust survive.
	shared := c.Tools["shared"]
	shared.Path = filepath.Join(t.TempDir(), "bin")
	shared.Help = "local help"
	c.Tools["shared"] = shared
	c.Projects = []string{t.TempDir()}
	put(t, o.Config, c)
	put(t, o.Bundle, cfg(map[string]config.Tool{"shared": {Description: "two"}}))
	r = mustSync(t, o)
	if len(r.Updated) != 1 {
		t.Fatal(r)
	}
	c, _ = config.Load(o.Config)
	if c.Tools["shared"].Path != shared.Path || c.Tools["shared"].Help != "local help" || len(c.Projects) != 1 || c.Tools["same"].Description != "same" {
		t.Fatal(c)
	}
	put(t, o.Bundle, cfg(map[string]config.Tool{}))
	r = mustSync(t, o)
	if len(r.Removed) != 1 {
		t.Fatal(r)
	}
	c, _ = config.Load(o.Config)
	if c.Tools["shared"].Description != "" || c.Tools["shared"].Path != shared.Path || c.Tools["local"] != local {
		t.Fatal(c)
	}
	receipt, _ := os.ReadFile(o.Config + ".canonfig.json.last-transaction.json")
	r = mustSync(t, o)
	if r.Applied {
		t.Fatal("idle write")
	}
	after, _ := os.ReadFile(o.Config + ".canonfig.json.last-transaction.json")
	if !bytes.Equal(receipt, after) {
		t.Fatal("idle changed receipt")
	}
}
func TestConflictsBlockWholeBatch(t *testing.T) {
	for _, name := range []string{"same name", "case difference", "local edit", "local delete"} {
		t.Run(name, func(t *testing.T) {
			o := fixture(t)
			put(t, o.Bundle, cfg(map[string]config.Tool{"a": {Description: "source"}}))
			if strings.HasPrefix(name, "local") {
				mustSync(t, o)
			}
			current := cfg(map[string]config.Tool{"a": {Description: "local"}})
			if name == "case difference" {
				current = cfg(map[string]config.Tool{"A": {Description: "source"}})
			}
			if name == "local delete" {
				current = cfg(map[string]config.Tool{})
			}
			put(t, o.Config, current)
			put(t, o.Bundle, cfg(map[string]config.Tool{"a": {Description: "source"}, "new": {Description: "new"}}))
			before, _ := os.ReadFile(o.Config)
			r, e := Sync(o)
			if e != nil || len(r.Conflicts) == 0 || r.Applied {
				t.Fatal(r, e)
			}
			after, _ := os.ReadFile(o.Config)
			if !bytes.Equal(before, after) {
				t.Fatal("partial write")
			}
		})
	}
}
func TestCaptureOnlyPortableMetadata(t *testing.T) {
	o := fixture(t)
	c := cfg(map[string]config.Tool{"npm": {Description: "package", Why: "why", When: "when", Path: filepath.Join(t.TempDir(), "npm"), Help: "private", Include: true, InstallURL: "https://www.npmjs.com/package/lsops"}, "hidden": {Description: "hidden", Hide: true}})
	c.Projects = []string{t.TempDir()}
	put(t, o.Config, c)
	r, e := Capture(o)
	if e != nil || !r.Applied {
		t.Fatal(r, e)
	}
	b, _ := os.ReadFile(o.Bundle)
	out, e := decode(b, true)
	if e != nil || len(out.Tools) != 1 || len(out.Projects) != 0 || out.Tools["npm"] != project(c.Tools["npm"]) {
		t.Fatal(out, e)
	}
	r, e = Capture(o)
	if e != nil || r.Changed || r.Applied {
		t.Fatal(r, e)
	}
}
func TestRefuseMissingInvalidAndPrivateInput(t *testing.T) {
	inputs := []string{`{"version":1}`, `{"version":1,"tools":null}`, `{"version":1,"tools":[],"tools":{}}`, "", `null`, `{"version":1,"tools":{"x":{"description":"x","path":"/private"}}}`, `{"version":1,"tools":{"x":{"description":"x","hide":true}}}`, `{"version":1,"tools":{"x":{"description":"x","install_url":"https://example.com/a.sh","install_manager":"sh"}}}`, `{"version":1,"tools":{"x":{"description":"x"},"X":{"description":"x"}}}`, `{"version":1,"tools":{},"projects":["/private"]}`, `{"version":2,"tools":{}}`, `{"version":1,"tools":{}} {}`, strings.Repeat(" ", limit+1)}
	for i, input := range inputs {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			o := fixture(t)
			if input != "" {
				if e := os.WriteFile(o.Bundle, []byte(input), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := Sync(o); e == nil {
				t.Fatal("accepted invalid input")
			}
			if _, e := os.Stat(o.Config); !errors.Is(e, os.ErrNotExist) {
				t.Fatal(e)
			}
		})
	}
}
func TestReservedPathsAndAliases(t *testing.T) {
	for _, suffix := range []string{"", ".updates.json", ".update-lock", ".canonfig.json", ".canonfig.json.pending", ".canonfig.json.lock", ".canonfig.json.last-transaction.json"} {
		t.Run(suffix, func(t *testing.T) {
			o := fixture(t)
			o.Bundle = o.Config + suffix
			if _, e := Capture(o); e == nil {
				t.Fatal("accepted reserved path")
			}
		})
	}
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			o := fixture(t)
			put(t, o.Config, cfg(map[string]config.Tool{}))
			var e error
			if kind == "symlink" {
				e = os.Symlink(o.Config, o.Bundle)
			} else {
				e = os.Link(o.Config, o.Bundle)
			}
			if e != nil {
				t.Skip(e)
			}
			if _, e = Capture(o); e == nil {
				t.Fatal("accepted alias")
			}
		})
	}
}
func TestInterruptedSyncFailsClosedWithBothBackups(t *testing.T) {
	o := fixture(t)
	put(t, o.Config, cfg(map[string]config.Tool{"mine": {Description: "mine"}}))
	put(t, o.Bundle, cfg(map[string]config.Tool{"a": {Description: "source"}}))
	before, _ := os.ReadFile(o.Config)
	calls := 0
	_, e := syncWith(o, func(p string, b, old []byte) error {
		calls++
		if calls == 2 {
			return errors.New("injected state write failure")
		}
		return replace(p, b, old)
	})
	if e == nil {
		t.Fatal("expected failure")
	}
	pending := o.Config + ".canonfig.json.pending"
	journal, e := os.ReadFile(pending)
	if e != nil || !bytes.Contains(journal, []byte(`"state_before": null`)) || !bytes.Contains(journal, []byte(`\"mine\"`)) {
		t.Fatal(string(journal), e)
	}
	if _, e = Sync(o); e == nil || !strings.Contains(e.Error(), "interrupted") {
		t.Fatal(e)
	}
	// Explicit operator recovery: restore both snapshots (state absent originally).
	if e = os.WriteFile(o.Config, before, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(pending); e != nil {
		t.Fatal(e)
	}
	mustSync(t, o)
	if _, e = os.Stat(o.Config + ".canonfig.json.last-transaction.json"); e != nil {
		t.Fatal(e)
	}
}
func TestWrongOwnershipAndLocks(t *testing.T) {
	o := fixture(t)
	put(t, o.Bundle, cfg(map[string]config.Tool{"a": {Description: "source"}}))
	mustSync(t, o)
	alternate := o
	alternate.Bundle = filepath.Join(filepath.Dir(o.Bundle), "other.json")
	put(t, alternate.Bundle, cfg(map[string]config.Tool{}))
	if _, e := Sync(alternate); e == nil {
		t.Fatal("accepted wrong bundle")
	}
	for _, suffix := range []string{".update-lock", ".canonfig.json.lock"} {
		if e := os.Mkdir(o.Config+suffix, 0700); e != nil {
			t.Fatal(e)
		}
		if _, e := Sync(o); e == nil {
			t.Fatal("ignored lock")
		}
		if e := os.Remove(o.Config + suffix); e != nil {
			t.Fatal(e)
		}
	}
}
func TestLargeReceiptDoesNotBlockNextSync(t *testing.T) {
	o := fixture(t)
	tools := map[string]config.Tool{}
	for i := 0; i < 2500; i++ {
		tools[fmtName(i)] = config.Tool{Description: strings.Repeat("a", 200)}
	}
	put(t, o.Bundle, cfg(tools))
	mustSync(t, o)
	for n := range tools {
		tools[n] = config.Tool{Description: strings.Repeat("b", 200)}
	}
	put(t, o.Bundle, cfg(tools))
	mustSync(t, o)
	stat, e := os.Stat(o.Config + ".canonfig.json.last-transaction.json")
	if e != nil || stat.Size() <= limit {
		t.Fatal(stat, e)
	}
	for n := range tools {
		tools[n] = config.Tool{Description: strings.Repeat("c", 200)}
	}
	put(t, o.Bundle, cfg(tools))
	mustSync(t, o)
}
func fmtName(i int) string { return fmt.Sprintf("tool-%d", i) }

func TestCaseVariantJSONFieldsCannotDeleteOwnedTools(t *testing.T) {
	o := fixture(t)
	put(t, o.Bundle, cfg(map[string]config.Tool{"a": {Description: "source"}}))
	mustSync(t, o)
	before, _ := os.ReadFile(o.Config)
	for _, input := range []string{
		`{"version":1,"tools":{"a":{"description":"source"}},"Tools":null}`,
		`{"version":1,"Version":1,"tools":{}}`,
		`{"version":1,"tools":{"a":{"description":"source","Description":"changed"}}}`,
		`{"version":1,"tools":{"a":{"install_sha256":"","inſtall_sha256":""}}}`,
	} {
		if e := os.WriteFile(o.Bundle, []byte(input), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := Sync(o); e == nil {
			t.Fatalf("accepted ambiguous input: %s", input)
		}
		after, _ := os.ReadFile(o.Config)
		if !bytes.Equal(before, after) {
			t.Fatal("ambiguous input mutated config")
		}
	}
	put(t, o.Bundle, cfg(map[string]config.Tool{"a": {Description: "source"}}))
	state := o.Config + ".canonfig.json"
	raw, _ := os.ReadFile(state)
	raw = bytes.Replace(raw, []byte(`"owned":`), []byte(`"Owned":null,"owned":`), 1)
	if e := os.WriteFile(state, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Sync(o); e == nil {
		t.Fatal("accepted ambiguous ownership receipt")
	}
}
