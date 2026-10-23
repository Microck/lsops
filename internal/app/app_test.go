package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Microck/lsops/internal/config"
)

func environment(t *testing.T) (string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("these integration fixtures use POSIX executable bits")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin with spaces")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("MANPATH", filepath.Join(root, "absent-man-pages"))
	t.Setenv("LSOPS_CONFIG", filepath.Join(root, "config.json"))
	return root, bin
}

func executable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	return path
}

func invoke(args ...string) (int, string, string) {
	var out, err bytes.Buffer
	code := Run(args, &out, &err, "test")
	return code, out.String(), err.String()
}

func TestInventoryDoesNotExecuteAndRespectsPathPrecedence(t *testing.T) {
	root, bin := environment(t)
	marker := filepath.Join(root, "executed")
	first := filepath.Join(bin, "project-tool")
	if err := os.WriteFile(first, []byte("#!/bin/sh\nprintf unsafe > '"+marker+"'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(root, "other-bin")
	if err := os.MkdirAll(second, 0755); err != nil {
		t.Fatal(err)
	}
	shadowed := executable(t, second, "project-tool")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+second)
	code, out, diagnostic := invokeDiscovery("--json")
	if code != 0 {
		t.Fatalf("code %d: %s", code, diagnostic)
	}
	var got inventory
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 1 || got.Tools[0].Path != first || len(got.Tools[0].Shadowed) != 1 || got.Tools[0].Shadowed[0] != shadowed {
		t.Fatalf("unexpected inventory: %+v", got)
	}
	if got.Tools[0].Description != "" || got.Tools[0].Source != "unknown" {
		t.Fatalf("unknown tool was assigned a description: %+v", got.Tools[0])
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("discovered executable ran: %v", err)
	}
}

func TestCuratedCatalogueIncludesOnlyRegisteredCommands(t *testing.T) {
	_, bin := environment(t)
	executable(t, bin, "git")
	executable(t, bin, "unregistered")
	code, out, diagnostic := invoke("--json")
	if code != 0 {
		t.Fatal(diagnostic)
	}
	var got inventory
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got.Tools) != 0 || got.Mode != "curated" || got.Summary.Discovered != 0 {
		t.Fatalf("default enumerated PATH: %s, %v", out, err)
	}
	code, _, diagnostic = invoke("add", "my-search", "Search internal documents")
	if code != 0 {
		t.Fatal(diagnostic)
	}
	_, out, _ = invoke("--json")
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "my-search" || got.Tools[0].Available || !got.Tools[0].Registered || got.Tools[0].Description != "Search internal documents" {
		t.Fatalf("configured CLI missing or availability wrong: %s", out)
	}
	_, out, _ = invoke()
	if !strings.Contains(out, "unavailable in this environment") {
		t.Fatal(out)
	}
	executable(t, bin, "my-search")
	_, out, _ = invoke("--json")
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 1 || !got.Tools[0].Available || got.Tools[0].Source != "config" {
		t.Fatal(out)
	}
	code, _, diagnostic = invoke("remove", "my-search")
	if code != 0 {
		t.Fatal(diagnostic)
	}
	_, out, _ = invoke("--names")
	if out != "" {
		t.Fatalf("remove retained a CLI or fell back to discovery: %q", out)
	}
	_, out, _ = invokeDiscovery("--names")
	if !strings.Contains(out, "my-search") || !strings.Contains(out, "unregistered") {
		t.Fatal(out)
	}
}

func TestCuratedExplicitPathAndLegacyEntries(t *testing.T) {
	root, _ := environment(t)
	custom := filepath.Join(root, "outside PATH")
	if err := os.MkdirAll(custom, 0755); err != nil {
		t.Fatal(err)
	}
	path := executable(t, custom, "different-executable")
	if code, _, diagnostic := invoke("add", "acme", "Query internal data", "--path", path); code != 0 {
		t.Fatal(diagnostic)
	}
	if code, _, diagnostic := invoke("describe", "acme", "Updated description"); code != 0 {
		t.Fatal(diagnostic)
	}
	_, out, _ := invoke("--json")
	var got inventory
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 1 || !got.Tools[0].Available || got.Tools[0].Path != path || got.Tools[0].HelpHint != shellQuote(path)+" --help" || got.Tools[0].Description != "Updated description" {
		t.Fatal(out)
	}
	_, out, _ = invoke()
	if !strings.Contains(out, displayName(path)) {
		t.Fatalf("explicit invocation path hidden: %s", out)
	}
	if err := os.WriteFile(os.Getenv("LSOPS_CONFIG"), []byte(`{"version":1,"tools":{"ls":{"include":true},"git":{"description":"My Git workflow"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, out, _ = invoke("--json")
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "git" || got.Summary.MissingDescription != 1 {
		t.Fatal(out)
	}
}

func invokeDiscovery(args ...string) (int, string, string) {
	return invoke(append([]string{"--discover"}, args...)...)
}

func TestBaselineFilteringAndExplicitMatch(t *testing.T) {
	_, bin := environment(t)
	executable(t, bin, "ls")
	executable(t, bin, "unfamiliar-tool")
	_, out, _ := invokeDiscovery("--names")
	if out != "unfamiliar-tool\n" {
		t.Fatalf("default output: %q", out)
	}
	_, out, _ = invokeDiscovery("--names", "--all")
	if out != "ls\nunfamiliar-tool\n" {
		t.Fatalf("all output: %q", out)
	}
	_, out, _ = invokeDiscovery("--names", "ls")
	if out != "ls\n" {
		t.Fatalf("explicit matching baseline: %q", out)
	}
}

func TestLimitAndSummaryAccountForEveryCommand(t *testing.T) {
	_, bin := environment(t)
	for _, name := range []string{"aaa-tool", "bbb-tool", "ccc-tool", "ls"} {
		executable(t, bin, name)
	}
	code, out, diagnostic := invokeDiscovery("--json", "--limit", "2")
	if code != 0 {
		t.Fatal(diagnostic)
	}
	var got inventory
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Summary.Discovered != 4 || got.Summary.Shown != 2 || got.Summary.OmittedByLimit != 1 || got.Summary.HiddenSystem != 1 {
		t.Fatalf("unexpected summary: %+v", got.Summary)
	}
	_, out, _ = invokeDiscovery("--limit", "2")
	if !strings.Contains(out, "1 additional matches omitted") {
		t.Fatal(out)
	}
}

func TestContextLimitPrioritizesDescribedCommands(t *testing.T) {
	_, bin := environment(t)
	executable(t, bin, "aaa-unknown")
	executable(t, bin, "zzz-search")
	if code, _, diagnostic := invoke("describe", "zzz-search", "Search online"); code != 0 {
		t.Fatal(diagnostic)
	}
	_, out, _ := invokeDiscovery("--context", "--limit", "1")
	if !strings.Contains(out, "zzz-search") || strings.Contains(out, "aaa-unknown") || !strings.Contains(out, "1 additional matches omitted") {
		t.Fatal(out)
	}
}

func TestHelpHintPreservesExecutableIdentity(t *testing.T) {
	_, bin := environment(t)
	path := executable(t, bin, "my  tool")
	_, out, _ := invokeDiscovery("--json")
	var got inventory
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 1 || got.Tools[0].HelpHint != shellQuote(path)+" --help" || got.Tools[0].HelpShell != "posix" {
		t.Fatalf("help hint points at a different command: %s", out)
	}
	hint, shell := nativeHelp("go", `C:\tools\a'b\go.exe`, "go help", "windows")
	if hint != `& 'C:\tools\a''b\go.exe' help` || shell != "powershell" {
		t.Fatalf("invalid PowerShell hint: %q %q", hint, shell)
	}
}

func TestWindowsOverridesPreferExactThenStableName(t *testing.T) {
	tools := map[string]config.Tool{"Git": {Description: "upper"}, "git": {Description: "lower"}}
	names := []string{"Git", "git"}
	for i := 0; i < 50; i++ {
		if got := lookupOverride(tools, names, "git", "windows"); got.Description != "lower" {
			t.Fatalf("exact match overridden: %+v", got)
		}
		if got := lookupOverride(tools, names, "GIT", "windows"); got.Description != "upper" {
			t.Fatalf("unstable fallback: %+v", got)
		}
	}
}

func TestFilteredKnownAndHiddenOverrides(t *testing.T) {
	_, bin := environment(t)
	executable(t, bin, "my-search")
	executable(t, bin, "unknown-tool")
	executable(t, bin, "secret-tool")
	data := `{"version":1,"tools":{"my-search":{"description":"Search documents"},"secret-tool":{"description":"Search secrets","hide":true}}}`
	if err := os.WriteFile(os.Getenv("LSOPS_CONFIG"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	_, out, _ := invoke("--names", "--all", "SEARCH")
	if out != "my-search\n" {
		t.Fatalf("case insensitive filter or hidden override: %q", out)
	}
	_, out, _ = invoke("--names", "--known")
	if out != "my-search\n" {
		t.Fatalf("known filter: %q", out)
	}
}

func TestControlsInNamesCannotForgeInventoryLines(t *testing.T) {
	_, bin := environment(t)
	executable(t, bin, "tool\nforged\x1b[31m")
	code, out, diagnostic := invokeDiscovery("--names")
	if code != 0 {
		t.Fatal(diagnostic)
	}
	if strings.Count(out, "\n") != 1 || strings.ContainsRune(out, '\x1b') {
		t.Fatalf("unsafe rendering: %q", out)
	}
}

func TestReservedNameCanBeListed(t *testing.T) {
	_, bin := environment(t)
	executable(t, bin, "describe")
	code, out, diagnostic := invokeDiscovery("--names", "list", "describe")
	if code != 0 || out != "describe\n" {
		t.Fatalf("%d %q %q", code, out, diagnostic)
	}
}

func TestInvalidOptionsAndMalformedConfig(t *testing.T) {
	environment(t)
	for _, args := range [][]string{{"--limit", "-1"}, {"--json", "--names"}, {"--unknown"}, {"describe"}, {"forget"}} {
		if code, _, _ := invoke(args...); code != 2 {
			t.Fatalf("%v returned %d", args, code)
		}
	}
	if err := os.WriteFile(os.Getenv("LSOPS_CONFIG"), []byte("not JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := invoke(); code != 1 || out != "" {
		t.Fatalf("malformed config silently accepted: %d %s", code, out)
	}
}

func TestEmptyPathProducesValidEmptyJSON(t *testing.T) {
	environment(t)
	t.Setenv("PATH", "")
	code, out, diagnostic := invoke("--json")
	if code != 0 {
		t.Fatal(diagnostic)
	}
	var got inventory
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Tools == nil || got.Warnings == nil {
		t.Fatalf("invalid empty schema: %s: %v", out, err)
	}
}

func TestHelpAndVersionWorkWithoutDiscovering(t *testing.T) {
	environment(t)
	t.Setenv("LSOPS_CONFIG", "/not/a/real/config")
	code, out, _ := invoke("--version")
	if code != 0 || out != "lsops test\n" {
		t.Fatalf("version: %d %q", code, out)
	}
	code, out, _ = invoke("--help")
	if code != 0 || !strings.Contains(out, "never executes") {
		t.Fatalf("help: %d %q", code, out)
	}
}
