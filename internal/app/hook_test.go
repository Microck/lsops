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

// Every integration path points into this test's temporary directory, including
// environment overrides that might otherwise select a real harness config.
func hookTestEnvironment(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	cfg := filepath.Join(root, "registry with spaces.json")
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude-home"))
	t.Setenv("CODEX_HOME", filepath.Join(root, "codex-home"))
	t.Setenv("COPILOT_HOME", filepath.Join(root, "copilot-home"))
	t.Setenv("OPENCODE_CONFIG_DIR", "")
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("PI_CONFIG_DIR", "")
	t.Setenv("LSOPS_CONFIG", cfg)
	return root, cfg
}

func hookInvoke(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr, "hook-test")
	return code, stdout.String(), stderr.String()
}

func TestHookRenderAdaptersContainExactCurrentList(t *testing.T) {
	_, cfg := hookTestEnvironment(t)
	if err := config.SetTool(cfg, "hook-test-search", "Search current project documents", ""); err != nil {
		t.Fatal(err)
	}
	code, expected, diagnostic := hookInvoke()
	if code != 0 {
		t.Fatal(diagnostic)
	}
	cases := []struct {
		harness, event, key string
		nested              bool
	}{
		{"", "", "", false},
		{"claude", "SessionStart", "additionalContext", true},
		{"codex", "SessionStart", "additionalContext", true},
		{"gemini", "SessionStart", "additionalContext", true},
		{"gemini", "BeforeAgent", "additionalContext", true},
		{"cursor", "sessionStart", "additional_context", false},
		{"copilot", "sessionStart", "additionalContext", false},
		{"opencode", "context", "", false},
		{"omp", "context", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.harness+tc.event, func(t *testing.T) {
			args := []string{"hook", "render", "--mode", "output", "--managed-lsops-hook"}
			if tc.harness != "" {
				args = append(args, "--harness", tc.harness, "--event", tc.event)
			}
			code, output, diagnostic := hookInvoke(args...)
			if code != 0 {
				t.Fatalf("code %d: %s", code, diagnostic)
			}
			content := output
			if tc.key != "" {
				var envelope map[string]json.RawMessage
				if err := json.Unmarshal([]byte(output), &envelope); err != nil {
					t.Fatalf("not a JSON protocol response: %s: %v", output, err)
				}
				if tc.nested {
					var specific map[string]json.RawMessage
					if err := json.Unmarshal(envelope["hookSpecificOutput"], &specific); err != nil {
						t.Fatal(err)
					}
					var event string
					if err := json.Unmarshal(specific["hookEventName"], &event); err != nil || event != tc.event {
						t.Fatalf("wrong event: %q, %v", event, err)
					}
					envelope = specific
				}
				if err := json.Unmarshal(envelope[tc.key], &content); err != nil {
					t.Fatal(err)
				}
			}
			if content != expected {
				t.Fatalf("hook changed list text\ngot: %q\nwant: %q", content, expected)
			}
			if strings.Contains(content, "Before choosing") || strings.Contains(content, "Refresh with") {
				t.Fatalf("output mode added instructions: %s", content)
			}
		})
	}
}

func TestHookOutputRefreshesAfterRegistryChangesWithoutExecutingTools(t *testing.T) {
	root, cfg := hookTestEnvironment(t)
	marker := filepath.Join(root, "unexpected-execution")
	registered := filepath.Join(root, "registered-tool")
	if runtime.GOOS == "windows" {
		registered += ".cmd"
	}
	if err := os.WriteFile(registered, []byte("#!/bin/sh\nprintf executed > "+shellQuote(marker)+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := config.SetTool(cfg, "first-tool", "First description", registered); err != nil {
		t.Fatal(err)
	}
	args := []string{"hook", "render", "--mode", "output", "--harness", "claude", "--event", "SessionStart"}
	code, first, diagnostic := hookInvoke(args...)
	if code != 0 || !strings.Contains(first, "First description") {
		t.Fatalf("initial render: %d %s %s", code, first, diagnostic)
	}
	if err := config.SetTool(cfg, "first-tool", "Updated description", ""); err != nil {
		t.Fatal(err)
	}
	if err := config.SetTool(cfg, "second-tool", "A newly registered tool", ""); err != nil {
		t.Fatal(err)
	}
	code, second, diagnostic := hookInvoke(args...)
	if code != 0 || !strings.Contains(second, "Updated description") || !strings.Contains(second, "second-tool") || strings.Contains(second, "First description") {
		t.Fatalf("stale render: %d %s %s", code, second, diagnostic)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("render executed a configured command: %v", err)
	}
}

func TestHookInstructionsQuotePathsAndDoNotLoadCatalogue(t *testing.T) {
	_, cfg := hookTestEnvironment(t)
	if err := os.WriteFile(cfg, []byte("intentionally invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	code, output, diagnostic := hookInvoke("hook", "render")
	if code != 0 || !strings.Contains(output, "Before choosing command-line tools") || !strings.Contains(output, "native help") {
		t.Fatalf("instructions unnecessarily loaded config: %d %s %s", code, output, diagnostic)
	}
	if strings.Contains(output, "--limit 60") || !strings.Contains(output, cfg) {
		t.Fatalf("incorrect default instructions: %s", output)
	}
	posixExecutable := "/a b/ls'ops$(touch marker)"
	posixConfig := "/cfg `not a command`/a'b.json"
	text := hookInstructions(posixExecutable, posixConfig, "linux", 7)
	expected := shellQuote(posixExecutable) + " --config " + shellQuote(posixConfig) + " --limit 7"
	if !strings.Contains(text, expected) {
		t.Fatalf("unsafe POSIX instruction: %s", text)
	}
	text = hookInstructions(`C:\Program Files\ls'ops.exe`, `C:\Users\A $name\a'b.json`, "windows", 0)
	expected = `& 'C:\Program Files\ls''ops.exe' '--config' 'C:\Users\A $name\a''b.json'`
	if !strings.Contains(text, expected) || strings.Contains(text, "--limit") {
		t.Fatalf("unsafe PowerShell instruction: %s", text)
	}
	code, output, _ = hookInvoke("hook", "render", "--mode", "output", "--harness", "gemini")
	if code != 1 || output != "" {
		t.Fatalf("invalid registry produced hook JSON: %d %s", code, output)
	}
}

func TestHookOutputLimitDoesNotInheritDiscoveryOrContext(t *testing.T) {
	_, cfg := hookTestEnvironment(t)
	for _, name := range []string{"aaa-registered", "bbb-registered"} {
		if err := config.SetTool(cfg, name, "Explicit description", ""); err != nil {
			t.Fatal(err)
		}
	}
	code, output, diagnostic := hookInvoke("--discover", "--context", "hook", "render", "--mode", "output", "--limit", "1")
	if code != 0 || !strings.Contains(output, "aaa-registered") || strings.Contains(output, "bbb-registered") || !strings.Contains(output, "1 additional matches omitted") {
		t.Fatalf("wrong limited catalogue: %d %s %s", code, output, diagnostic)
	}
	if strings.Contains(output, "CLI commands discovered") || strings.Contains(output, "Refresh with") {
		t.Fatalf("output inherited non-curated options: %s", output)
	}
}

func TestHookProjectInstallStatusAndRemovalStayIsolated(t *testing.T) {
	root, _ := hookTestEnvironment(t)
	project := filepath.Join(root, "project with spaces")
	settings := filepath.Join(project, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0700); err != nil {
		t.Fatal(err)
	}
	original := `{"theme":"existing","hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo another-hook"}]}]}}`
	if err := os.WriteFile(settings, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	code, output, diagnostic := hookInvoke("hook", "install", "claude", "--dir", project, "--mode=output", "--limit", "2")
	if code != 0 || !strings.Contains(output, "project scope") {
		t.Fatalf("install: %d %s %s", code, output, diagnostic)
	}
	data, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) || !bytes.Contains(data, []byte("another-hook")) || !bytes.Contains(data, []byte("--managed-lsops-hook")) || !bytes.Contains(data, []byte("output")) {
		t.Fatalf("install did not preserve other hooks: %s", data)
	}
	code, output, diagnostic = hookInvoke("hook", "status", "claude", "--dir", project)
	if code != 0 || !strings.Contains(output, "claude: configured") {
		t.Fatalf("status: %d %s %s", code, output, diagnostic)
	}
	code, output, diagnostic = hookInvoke("hook", "uninstall", "claude", "--dir", project)
	if code != 0 || !strings.Contains(output, "Removed") {
		t.Fatalf("uninstall: %d %s %s", code, output, diagnostic)
	}
	data, err = os.ReadFile(settings)
	if err != nil || bytes.Contains(data, []byte("--managed-lsops-hook")) || !bytes.Contains(data, []byte("another-hook")) || !bytes.Contains(data, []byte("existing")) {
		t.Fatalf("uninstall removed other settings: %s: %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "settings.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("project operation touched user config: %v", err)
	}
}

func TestHookUserHomesAndPreviewCompatibility(t *testing.T) {
	root, cfg := hookTestEnvironment(t)
	code, output, diagnostic := hookInvoke("hook", "claude")
	if code != 0 || !json.Valid([]byte(output)) || !strings.Contains(output, "instructions") || !strings.Contains(output, "--managed-lsops-hook") {
		t.Fatalf("legacy preview: %d %s %s", code, output, diagnostic)
	}
	if _, err := os.Stat(cfg); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview wrote the registry: %v", err)
	}
	userSettings := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "settings.json")
	if _, err := os.Stat(userSettings); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview wrote harness settings: %v", err)
	}
	code, _, diagnostic = hookInvoke("hook", "install", "claude")
	if code != 0 {
		t.Fatal(diagnostic)
	}
	if _, err := os.Stat(userSettings); err != nil {
		t.Fatalf("CLAUDE_CONFIG_DIR was ignored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "settings.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("installer also wrote default home: %v", err)
	}
	parsed, _, err := parseHookOptions([]string{"--mode=output", "--scope", "project", "--limit=0"}, options{configPath: cfg})
	if err != nil {
		t.Fatal(err)
	}
	opts, err := integrationOptions(parsed, false)
	if err != nil || opts.Executable != "" || opts.Limit != 0 || opts.CodexHome != os.Getenv("CODEX_HOME") || opts.CopilotHome != os.Getenv("COPILOT_HOME") || opts.OpenCodeHome != filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode") {
		t.Fatalf("integration options: %+v: %v", opts, err)
	}
}

func TestHookOpenCodeHomePreservesFilesystemPathAndOverride(t *testing.T) {
	root, cfg := hookTestEnvironment(t)
	h := hookOptions{scope: "user", mode: "instructions", configPath: cfg}
	override := root + string(os.PathSeparator) + "custom" + string(os.PathSeparator) + ".."
	t.Setenv("OPENCODE_CONFIG_DIR", override)
	opts, err := integrationOptions(h, false)
	if err != nil || opts.OpenCodeHome != override {
		t.Fatalf("OpenCode override lost precedence or literal path: %+v, %v", opts, err)
	}
	t.Setenv("OPENCODE_CONFIG_DIR", "")
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("PI_CONFIG_DIR", "")
	if runtime.GOOS == "windows" {
		t.Skip("filesystem symlink fixture needs Unix symlink permissions")
	}
	real := filepath.Join(root, "real")
	child := filepath.Join(real, "child")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(child, link); err != nil {
		t.Fatal(err)
	}
	xdg := link + "/.."
	t.Setenv("XDG_CONFIG_HOME", xdg)
	opts, err = integrationOptions(h, false)
	if err != nil || opts.OpenCodeHome != xdg+"/opencode" {
		t.Fatalf("XDG symlink traversal was cleaned: %+v, %v", opts, err)
	}
	code, output, diagnostic := hookInvoke("hook", "install", "opencode")
	if code != 0 {
		t.Fatalf("OpenCode install: %d %s %s", code, output, diagnostic)
	}
	if _, err := os.Stat(filepath.Join(real, "opencode", "plugins", "lsops.js")); err != nil {
		t.Fatalf("plugin did not reach the filesystem-selected XDG directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "opencode", "plugins", "lsops.js")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("plugin was installed at a lexically cleaned path: %v", err)
	}
}

func TestHookRejectsUnsupportedModesTargetsAndEvents(t *testing.T) {
	root, _ := hookTestEnvironment(t)
	cases := [][]string{
		{"hook"},
		{"hook", "other"},
		{"hook", "install"},
		{"hook", "install", "unsupported"},
		{"hook", "preview", "all"},
		{"hook", "render", "extra"},
		{"hook", "render", "--mode", "live"},
		{"hook", "render", "--harness", "unsupported"},
		{"hook", "render", "--event", "SessionStart"},
		{"hook", "render", "--harness", "gemini", "--event", "PreCompress"},
		{"hook", "render", "--harness", "cursor", "--event", "postCompact"},
		{"hook", "render", "--limit", "-1"},
		{"hook", "render", "--mode"},
		{"hook", "install", "claude", "--scope", "workspace"},
		{"hook", "install", "claude", "--scope", "user", "--dir", root},
		{"hook", "install", "claude", "--harness", "claude"},
		{"hook", "targets", "extra"},
	}
	for _, args := range cases {
		code, output, diagnostic := hookInvoke(args...)
		if code != 2 || output != "" {
			t.Errorf("%v: code %d, output %q, diagnostic %q", args, code, output, diagnostic)
		}
	}
}

func TestOMPAgentDirectoryOverrides(t *testing.T) {
	root, _ := hookTestEnvironment(t)
	h, _, err := parseHookOptions([]string{}, options{configPath: filepath.Join(root, "catalog.json")})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CONFIG_DIR", filepath.Join(root, "omp root"))
	opts, err := integrationOptions(h, false)
	if err != nil || opts.OMPHome != filepath.Join(root, "omp root", "agent") {
		t.Fatalf("root override %+v %v", opts, err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "profile agent"))
	opts, err = integrationOptions(h, false)
	if err != nil || opts.OMPHome != filepath.Join(root, "profile agent") {
		t.Fatalf("agent override %+v %v", opts, err)
	}
}
