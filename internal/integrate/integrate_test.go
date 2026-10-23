package integrate

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"
)

func testOptions(t *testing.T) Options {
	t.Helper()
	dir := t.TempDir()
	return Options{
		Executable: filepath.Join(dir, "bin with 'quotes'", "lsops"),
		ConfigPath: filepath.Join(dir, "registry with 'quotes'.json"),
		Home:       filepath.Join(dir, "home"), ProjectDir: filepath.Join(dir, "project"),
		Mode: "instructions", Scope: "user", GOOS: "linux",
	}
}

func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0640); err != nil {
		t.Fatal(err)
	}
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func foreignDocument(target string) map[string]any {
	entry := map[string]any{"type": "command", "command": "foreign-agent-helper", "extra": "keep"}
	event := "SessionStart"
	if nestedHooks(target) {
		entry = map[string]any{"matcher": "startup", "hooks": []any{entry}, "other": "retain"}
	} else {
		event = "sessionStart"
	}
	document := map[string]any{
		"custom": map[string]any{"large": json.Number("900719925474099312345"), "array": []any{true, nil, "keep"}},
		"hooks":  map[string]any{event: []any{entry}},
	}
	if target == "cursor" || target == "copilot" {
		document["version"] = json.Number("1")
	}
	if target == "claude" {
		document["disableAllHooks"] = true
	}
	if target == "gemini" {
		document["hooksConfig"] = map[string]any{"enabled": false, "disabled": []any{"lsops", "other"}}
		hooks := document["hooks"].(map[string]any)
		hooks["enabled"], hooks["disabled"], hooks["notifications"] = false, []any{"old-hook"}, true
	}
	return document
}

func TestJSONInstallUpdateUninstallPreservesForeignSettings(t *testing.T) {
	for _, target := range []string{"claude", "gemini", "cursor", "codex", "copilot"} {
		t.Run(target, func(t *testing.T) {
			options := testOptions(t)
			path, err := destination(target, options)
			if err != nil {
				t.Fatal(err)
			}
			original, _ := json.Marshal(foreignDocument(target))
			original = append([]byte(" \n"), original...)
			writeFixture(t, path, original)
			result, err := Install(target, options)
			if err != nil || !result.Changed || !result.Installed || len(result.Paths) != 2 {
				t.Fatalf("first install = %+v, %v", result, err)
			}
			backup := result.Paths[1]
			if !bytes.Equal(readFixture(t, backup), original) {
				t.Fatal("backup did not preserve exact original bytes")
			}
			if strings.HasSuffix(backup, ".json") || strings.HasSuffix(backup, ".js") || strings.HasSuffix(backup, ".ts") {
				t.Fatalf("backup could be discovered as live configuration: %s", backup)
			}
			installed := readFixture(t, path)
			if !bytes.Contains(installed, []byte("900719925474099312345")) || !bytes.Contains(installed, []byte("foreign-agent-helper")) {
				t.Fatal("foreign settings or numeric precision were lost")
			}
			if target == "claude" && !strings.Contains(strings.Join(result.Notes, " "), "disableAllHooks") {
				t.Fatal("missing disabled hook notice")
			}
			if target == "gemini" && !strings.Contains(strings.Join(result.Notes, " "), "hooksConfig.enabled") {
				t.Fatal("missing disabled hook notice")
			}
			status, err := Status(target, options)
			if err != nil || !status.Installed || status.Changed {
				t.Fatalf("installed status = %+v, %v", status, err)
			}
			again, err := Install(target, options)
			if err != nil || again.Changed || !again.Installed || len(again.Paths) != 1 {
				t.Fatalf("reinstall was not a no-op: %+v, %v", again, err)
			}
			if !bytes.Equal(installed, readFixture(t, path)) {
				t.Fatal("no-op reinstall changed file bytes")
			}
			options.Mode = "output"
			options.Limit = 7
			options.Executable += "-new"
			updated, err := Install(target, options)
			if err != nil || !updated.Changed || !updated.Installed {
				t.Fatalf("update = %+v, %v", updated, err)
			}
			if !bytes.Equal(readFixture(t, updated.Paths[1]), installed) {
				t.Fatal("update backup did not preserve previous installation")
			}
			document, err := parseSettings(readFixture(t, path), path)
			if err != nil {
				t.Fatal(err)
			}
			wantManaged := 1
			if target == "gemini" {
				wantManaged = 2
			}
			if count := managedCount(target, document); count != wantManaged {
				t.Fatalf("managed hooks after update = %d, want %d", count, wantManaged)
			}
			removed, err := Uninstall(target, options)
			if err != nil || !removed.Changed || removed.Installed {
				t.Fatalf("uninstall = %+v, %v", removed, err)
			}
			remaining := readFixture(t, path)
			if !bytes.Contains(remaining, []byte("foreign-agent-helper")) || bytes.Contains(remaining, []byte(managedFlag)) {
				t.Fatal("uninstall removed a foreign hook or retained a managed one")
			}
			again, err = Uninstall(target, Options{Home: options.Home})
			if err != nil || again.Changed || again.Installed || !bytes.Equal(remaining, readFixture(t, path)) {
				t.Fatalf("second uninstall = %+v, %v", again, err)
			}
			status, err = Status(target, Options{Home: options.Home})
			if err != nil || status.Installed {
				t.Fatalf("removed status = %+v, %v", status, err)
			}
			if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".lsops-integration-*")); len(leftovers) != 0 {
				t.Fatalf("temporary files remain: %v", leftovers)
			}
		})
	}
}

func managedCount(target string, document map[string]any) int {
	count := 0
	hooks, _ := document["hooks"].(map[string]any)
	for event, value := range hooks {
		if hookMetadata(target, event) {
			continue
		}
		for _, value := range value.([]any) {
			entry := value.(map[string]any)
			if nestedHooks(target) {
				for _, child := range entry["hooks"].([]any) {
					if managedHook(target, child.(map[string]any)) {
						count++
					}
				}
			} else if managedHook(target, entry) {
				count++
			}
		}
	}
	return count
}

func TestMixedGroupOnlyRemovesManagedChild(t *testing.T) {
	options := testOptions(t)
	path, _ := destination("claude", options)
	original := []byte(`{"hooks":{"SessionStart":[{"matcher":"resume","extra":"keep","hooks":[{"type":"command","command":"old-lsops","args":["--config","/registry.json","hook","render","--harness","claude","--managed-lsops-hook"]},{"type":"command","command":"foreign"}]}],"Stop":[{"hooks":[{"type":"command","command":"foreign-stop"}]}]}}`)
	writeFixture(t, path, original)
	result, err := Uninstall("claude", options)
	if err != nil || !result.Changed {
		t.Fatalf("uninstall = %+v, %v", result, err)
	}
	document, _ := parseSettings(readFixture(t, path), path)
	hooks := document["hooks"].(map[string]any)
	group := hooks["SessionStart"].([]any)[0].(map[string]any)
	if group["extra"] != "keep" || group["matcher"] != "resume" || len(group["hooks"].([]any)) != 1 {
		t.Fatalf("mixed group was damaged: %#v", group)
	}
	if _, ok := hooks["Stop"]; !ok {
		t.Fatal("unrelated event was removed")
	}
}

func TestMalformedSettingsAreUntouched(t *testing.T) {
	tests := []struct{ name, target, source string }{
		{"comments", "claude", "{ // comment\n\"hooks\": {}}"},
		{"trailing-comma", "claude", `{"hooks":{},}`},
		{"duplicate", "claude", `{"custom":{"x":1,"x":2}}`},
		{"trailing-document", "claude", `{} {}`},
		{"null", "claude", `null`},
		{"array", "claude", `[]`},
		{"empty", "claude", ``},
		{"bad-hooks", "claude", `{"hooks":[]}`},
		{"bad-event", "claude", `{"hooks":{"SessionStart":{}}}`},
		{"bad-group", "claude", `{"hooks":{"SessionStart":[{"hooks":"bad"}]}}`},
		{"bad-child", "claude", `{"hooks":{"SessionStart":[{"hooks":[false]}]}}`},
		{"cursor-version", "cursor", `{"version":2,"hooks":{}}`},
		{"copilot-version", "copilot", `{"version":0,"hooks":{}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := testOptions(t)
			path, _ := destination(test.target, options)
			original := []byte(test.source)
			writeFixture(t, path, original)
			for _, action := range []func(string, Options) (Result, error){Install, Uninstall, Status} {
				if _, err := action(test.target, options); err == nil {
					t.Fatal("malformed settings were accepted")
				}
				if !bytes.Equal(readFixture(t, path), original) {
					t.Fatal("malformed settings were modified")
				}
			}
			if matches, _ := filepath.Glob(path + ".lsops-backup-*"); len(matches) != 0 {
				t.Fatal("rejected settings created a backup")
			}
		})
	}
}

func TestMissingStatusUninstallAndPreviewDoNotWrite(t *testing.T) {
	options := testOptions(t)
	for _, target := range Targets() {
		for _, action := range []func(string, Options) (Result, error){Status, Uninstall} {
			result, err := action(target.Name, options)
			if err != nil || result.Changed || result.Installed {
				t.Fatalf("missing %s = %+v, %v", target.Name, result, err)
			}
		}
		if _, err := Preview(target.Name, options); err != nil {
			t.Fatalf("preview %s: %v", target.Name, err)
		}
	}
	if _, err := os.Stat(options.Home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only actions created home directory: %v", err)
	}
}

func TestSymlinkAndNonregularDestinationsAreRejected(t *testing.T) {
	for _, variant := range []string{"file", "dangling", "directory"} {
		t.Run(variant, func(t *testing.T) {
			options := testOptions(t)
			path, _ := destination("claude", options)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			real := filepath.Join(t.TempDir(), "real.json")
			if variant == "file" {
				writeFixture(t, real, []byte("{}"))
			}
			if variant == "directory" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Symlink(real, path); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			for _, action := range []func(string, Options) (Result, error){Install, Uninstall, Status} {
				if _, err := action("claude", options); err == nil {
					t.Fatal("nonregular destination accepted")
				}
			}
			if variant == "file" && string(readFixture(t, real)) != "{}" {
				t.Fatal("symlink target was modified")
			}
		})
	}
}

func TestOptimisticReplacementRejectsChangedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	writeFixture(t, path, []byte(`{"changed":true}`))
	if _, err := replaceManagedFile(path, []byte(`{}`), []byte(`{"ours":true}`), true, false); err == nil {
		t.Fatal("stale merge overwrote newer settings")
	}
	if string(readFixture(t, path)) != `{"changed":true}` {
		t.Fatal("changed settings were not preserved")
	}
	if files, _ := os.ReadDir(filepath.Dir(path)); len(files) != 1 {
		t.Fatalf("failed replacement left files: %v", files)
	}
}

func TestTargetPathsAndExplicitHomes(t *testing.T) {
	options := testOptions(t)
	options.ClaudeHome = filepath.Join(t.TempDir(), "claude-custom")
	options.CodexHome = filepath.Join(t.TempDir(), "codex-custom")
	options.CopilotHome = filepath.Join(t.TempDir(), "copilot-custom")
	user := map[string]string{
		"claude":  filepath.Join(options.ClaudeHome, "settings.json"),
		"gemini":  filepath.Join(options.Home, ".gemini", "settings.json"),
		"cursor":  filepath.Join(options.Home, ".cursor", "hooks.json"),
		"codex":   filepath.Join(options.CodexHome, "hooks.json"),
		"copilot": filepath.Join(options.CopilotHome, "hooks", "lsops.json"),
	}
	for target, want := range user {
		got, err := destination(target, options)
		if err != nil || got != want {
			t.Fatalf("%s user path = %q, %v; want %q", target, got, err, want)
		}
	}
	options.Scope = "project"
	project := map[string]string{
		"claude": ".claude/settings.json", "gemini": ".gemini/settings.json", "cursor": ".cursor/hooks.json",
		"codex": ".codex/hooks.json", "copilot": ".github/hooks/lsops.json",
	}
	for target, suffix := range project {
		want := filepath.Join(options.ProjectDir, filepath.FromSlash(suffix))
		got, err := destination(target, options)
		if err != nil || got != want {
			t.Fatalf("%s project path = %q, %v; want %q", target, got, err, want)
		}
	}
}

func TestPreviewSchemasAndLimitZero(t *testing.T) {
	options := testOptions(t)
	for _, target := range []string{"claude", "gemini", "cursor", "codex", "copilot"} {
		t.Run(target, func(t *testing.T) {
			data, err := Preview(target, options)
			if err != nil {
				t.Fatal(err)
			}
			document, err := parseSettings(data, "preview")
			if err != nil || validateHookDocument(target, document) != nil {
				t.Fatalf("invalid preview: %s; %v", data, err)
			}
			if !hasManagedHooks(target, document) {
				t.Fatal("preview lacks managed hook")
			}
			hooks := document["hooks"].(map[string]any)
			event := "SessionStart"
			if target == "cursor" || target == "copilot" {
				event = "sessionStart"
			}
			entry := hooks[event].([]any)[0].(map[string]any)
			if nestedHooks(target) {
				entry = entry["hooks"].([]any)[0].(map[string]any)
			}
			if target == "claude" || target == "copilot" {
				field := "command"
				if target == "copilot" {
					field = "exec"
				}
				if entry[field] != options.Executable {
					t.Fatalf("not a direct executable: %#v", entry)
				}
				args := entry["args"].([]any)
				if args[len(args)-3] != "--limit" || args[len(args)-2] != "0" || args[len(args)-1] != managedFlag {
					t.Fatalf("wrong limit or marker: %#v", args)
				}
			} else if !strings.Contains(entry["command"].(string), "'--limit' '0'") {
				t.Fatal("zero limit was not preserved")
			}
			if target == "gemini" {
				if _, ok := hooks["BeforeAgent"]; !ok {
					t.Fatal("Gemini next-turn refresh is missing")
				}
				if entry["timeout"].(json.Number).String() != "10000" {
					t.Fatal("Gemini timeout must be in milliseconds")
				}
			}
			if target == "codex" && !strings.HasPrefix(entry["commandWindows"].(string), "powershell.exe -NoProfile -NonInteractive -EncodedCommand ") {
				t.Fatal("Codex is missing its explicit Windows shell command")
			}
		})
	}
}

func TestPOSIXCommandPreservesArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell test")
	}
	path := filepath.Join(t.TempDir(), "lsops '$ odd")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\0' \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	args := []string{"", "space here", "a'b", "$(false)", "`false`", "double\"quote", "αβ", managedFlag}
	output, err := exec.Command("sh", "-c", quotedCommand(path, args, false)).Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00")
	if !reflect.DeepEqual(got, args) {
		t.Fatalf("argv changed: got %#v, want %#v", got, args)
	}
}

func TestWindowsEncodedCommandPreservesLiteralPaths(t *testing.T) {
	executable := `C:\tools with 'quotes'\lsops.exe`
	args := []string{"--config", `C:\Users\a & b\$(literal).json`, "αβ", managedFlag}
	command := windowsEncodedCommand(executable, args)
	prefix := "powershell.exe -NoProfile -NonInteractive -EncodedCommand "
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(command, prefix))
	if err != nil || len(data)%2 != 0 {
		t.Fatalf("invalid encoded command: %v", err)
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	script := string(utf16.Decode(units))
	want := `& 'C:\tools with ''quotes''\lsops.exe' '--config' 'C:\Users\a & b\$(literal).json' 'αβ' '--managed-lsops-hook'; exit $LASTEXITCODE`
	if script != want {
		t.Fatalf("PowerShell script = %q, want %q", script, want)
	}
}

func TestGeminiEnvironmentInterpolationCannotChangeLiteralPaths(t *testing.T) {
	// Gemini resolves these patterns in settings before invoking the shell.
	placeholder := regexp.MustCompile(`\$(?:(\w+)|\{([^}]+?)(?::-([^}]*))?\})`)
	options := testOptions(t)
	options.Executable = filepath.Join(t.TempDir(), "lsops $HOME's tool")
	options.ConfigPath = filepath.Join(t.TempDir(), "${HOME}-$GEMINI_PROJECT_DIR.json")
	if runtime.GOOS != "windows" {
		if err := os.WriteFile(options.Executable, []byte("#!/bin/sh\nprintf '%s\\0' \"$@\"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, goos := range []string{"linux", "windows"} {
		options.GOOS = goos
		data, err := Preview("gemini", options)
		if err != nil {
			t.Fatal(err)
		}
		document, _ := parseSettings(data, "preview")
		group := document["hooks"].(map[string]any)["SessionStart"].([]any)[0].(map[string]any)
		hook := group["hooks"].([]any)[0].(map[string]any)
		command := hook["command"].(string)
		if match := placeholder.FindString(command); match != "" {
			t.Fatalf("Gemini could expand %q in %s command", match, goos)
		}
		if !managedHook("gemini", hook) {
			t.Fatalf("Gemini %s command no longer recognizes its managed marker", goos)
		}
		words, ok := commandWords(command)
		want := append([]string{options.Executable}, hookArgs("gemini", "SessionStart", options)...)
		if !ok || !reflect.DeepEqual(words, want) {
			t.Fatalf("Gemini %s argv changed: %#v; want %#v", goos, words, want)
		}
		if goos == "linux" && runtime.GOOS != "windows" {
			output, err := exec.Command("sh", "-c", command).Output()
			if err != nil {
				t.Fatal(err)
			}
			got := strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00")
			if !reflect.DeepEqual(got, want[1:]) {
				t.Fatalf("POSIX Gemini argv = %#v; want %#v", got, want[1:])
			}
		}
	}
}

func TestMarkerDoesNotMatchLongerFlagOrUnrelatedHookTypes(t *testing.T) {
	for _, hook := range []map[string]any{
		{"command": "tool --managed-lsops-hook-extra"},
		{"command": "tool prefix--managed-lsops-hook"},
		{"name": "lsops", "command": "foreign"},
		{"type": "prompt", "args": []any{managedFlag}},
		{"command": "printf '%s' '--managed-lsops-hook'"},
		{"command": "foreign", "args": []any{managedFlag}},
		{"command": "foreign", "args": []any{"--config", "/config", "hook", "render", "--harness", "gemini", managedFlag}},
	} {
		if managedHook("claude", hook) {
			t.Fatalf("foreign hook classified as managed: %#v", hook)
		}
	}
}

func TestLiteralSymlinkParentPathIsNotCleaned(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix symlink path semantics")
	}
	dir := t.TempDir()
	physical := filepath.Join(dir, "physical")
	child := filepath.Join(physical, "child")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	shortcut := filepath.Join(dir, "shortcut")
	if err := os.Symlink(child, shortcut); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	options := testOptions(t)
	options.Home = shortcut + "/.."
	options.ConfigPath = shortcut + "/../registry.json"
	result, err := Install("claude", options)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Paths[0], "/shortcut/../") {
		t.Fatalf("destination path was cleaned: %q", result.Paths[0])
	}
	actual := filepath.Join(physical, ".claude", "settings.json")
	data := readFixture(t, actual)
	if !strings.Contains(string(data), "/shortcut/../registry.json") {
		t.Fatal("registry path was cleaned in the installed hook")
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("settings were written to the lexically cleaned destination")
	}
}

func TestOpenCodeManagedFileLifecycleAndForeignRefusal(t *testing.T) {
	options := testOptions(t)
	path, _ := destination("opencode", options)
	writeFixture(t, path, []byte("// user plugin\nexport default {};\n"))
	if _, err := Install("opencode", options); err == nil {
		t.Fatal("foreign plugin was overwritten")
	}
	if _, err := Uninstall("opencode", options); err == nil {
		t.Fatal("foreign plugin was deleted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	installed, err := Install("opencode", options)
	if err != nil || !installed.Changed || !installed.Installed {
		t.Fatalf("install = %+v, %v", installed, err)
	}
	first := readFixture(t, path)
	again, err := Install("opencode", options)
	if err != nil || again.Changed {
		t.Fatalf("second install = %+v, %v", again, err)
	}
	removed, err := Uninstall("opencode", Options{Home: options.Home})
	if err != nil || !removed.Changed || removed.Installed || len(removed.Paths) != 2 {
		t.Fatalf("uninstall = %+v, %v", removed, err)
	}
	if !bytes.Equal(first, readFixture(t, removed.Paths[1])) {
		t.Fatal("plugin uninstall backup changed bytes")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("managed plugin was not removed")
	}
}
