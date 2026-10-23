package integrate

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenCodePathScopes(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "user home")
	project := filepath.Join(root, "project")
	custom := filepath.Join(root, "custom config")
	literalHome := home + string(os.PathSeparator) + "link" + string(os.PathSeparator) + ".."
	for _, tc := range []struct {
		name string
		opts Options
		want string
	}{
		{"user", Options{Scope: "user", Home: home}, filepath.Join(home, ".config", "opencode", "plugins", "lsops.js")},
		{"custom user", Options{Scope: "user", Home: home, OpenCodeHome: custom}, filepath.Join(custom, "plugins", "lsops.js")},
		{"literal home", Options{Scope: "user", Home: literalHome}, literalHome + string(os.PathSeparator) + filepath.Join(".config", "opencode", "plugins", "lsops.js")},
		{"project", Options{Scope: "project", Home: home, ProjectDir: project, OpenCodeHome: custom}, filepath.Join(project, ".opencode", "plugins", "lsops.js")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := openCodePath(tc.opts)
			if err != nil || got != tc.want {
				t.Fatalf("openCodePath = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	if _, err := openCodePath(Options{Scope: "user", OpenCodeHome: "relative"}); err == nil {
		t.Fatal("accepted a relative OpenCode home")
	}
}

func TestOpenCodeContentArguments(t *testing.T) {
	root := t.TempDir()
	opts := Options{
		Executable: filepath.Join(root, "lsops' $x; `quoted`"),
		ConfigPath: filepath.Join(root, "registry's file.json"),
		Mode:       "output",
		Limit:      0,
	}
	data, err := openCodeContent(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte(openCodeMarker+"\n")) {
		t.Fatal("plugin is missing its managed header")
	}
	// The runtime test below validates these literals by importing the module.
	// Windows previews must preserve native absolute paths even on Unix hosts.
	windows := opts
	windows.GOOS = "windows"
	windows.Executable = `C:\Tools With Spaces\lsops.exe`
	windows.ConfigPath = `C:\Users\person\registry.json`
	if _, err := openCodeContent(windows); err != nil {
		t.Fatalf("Windows preview: %v", err)
	}
	for _, tc := range []struct {
		name string
		edit func(*Options)
	}{
		{"relative executable", func(o *Options) { o.Executable = "lsops" }},
		{"relative config", func(o *Options) { o.ConfigPath = "registry.json" }},
		{"NUL", func(o *Options) { o.ConfigPath += "\x00" }},
		{"invalid UTF-8", func(o *Options) { o.Executable += "\xff" }},
		{"unknown mode", func(o *Options) { o.Mode = "other" }},
		{"negative limit", func(o *Options) { o.Limit = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := opts
			tc.edit(&invalid)
			if _, err := openCodeContent(invalid); err == nil {
				t.Fatal("accepted invalid plugin options")
			}
		})
	}
}

func TestOpenCodePluginBothAPIs(t *testing.T) {
	node := openCodeNode(t)
	root := t.TempDir()
	sessionDir := filepath.Join(root, "session with spaces")
	if err := os.Mkdir(sessionDir, 0700); err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Executable: filepath.Join(root, "lsops' $x; `quoted`"),
		ConfigPath: filepath.Join(root, "registry's file.json"),
		Mode:       "instructions",
		Limit:      0,
	}
	plugin := openCodeWritePlugin(t, root, opts)
	script := `import assert from "node:assert/strict";
import childProcess from "node:child_process";
import { syncBuiltinESMExports } from "node:module";
import { pathToFileURL } from "node:url";

const calls = [];
const warnings = [];
let generation = 1;
let fail = false;
childProcess.execFile = (executable, args, options, callback) => {
  calls.push({ executable, args, options });
  queueMicrotask(() => callback(fail ? new Error("PRIVATE CHILD STDERR") : null,
    fail ? "PRIVATE PARTIAL OUTPUT" : "catalog " + generation));
  return {};
};
syncBuiltinESMExports();
console.warn = (message) => warnings.push(message);
const { default: plugin } = await import(pathToFileURL(process.argv[2]).href);
assert.equal(plugin.id, "lsops");
let callback;
let disposed = false;
let lookupFails = false;
const cleanup = await plugin.setup({ session: {
  hook: async (name, fn) => {
    assert.equal(name, "context");
    callback = fn;
    return { dispose: () => { disposed = true; } };
  },
  get: async (input) => {
    assert.deepEqual(input, { sessionID: "session-a" });
    if (lookupFails) throw new Error("unavailable");
    return { location: { directory: process.argv[3] } };
  },
} });

const first = { sessionID: "session-a", system: [{ type: "text", text: "other plugin" }] };
await callback(first);
assert.deepEqual(first.system, [
  { type: "text", text: "other plugin" }, { type: "text", text: "catalog 1" },
]);
generation = 2;
const continued = { sessionID: "session-a", system: [] };
await callback(continued);
assert.deepEqual(continued.system, [{ type: "text", text: "catalog 2" }]);
assert.equal(calls[0].options.cwd, process.argv[3]);
assert.equal(calls[1].options.cwd, process.argv[3]);

lookupFails = true;
await callback({ sessionID: "session-a", system: [] });
assert.equal(calls[2].options.cwd, process.cwd());

fail = true;
const failed = { sessionID: "session-a", system: [{ type: "text", text: "keep" }] };
await callback(failed);
assert.deepEqual(failed.system, [{ type: "text", text: "keep" }]);
assert.equal(warnings.length, 1);
assert(!warnings[0].includes("PRIVATE"));
fail = false;

const v1 = await plugin.server({ client: { session: {
  get: async (input) => {
    assert.deepEqual(input, { path: { id: "session-b" } });
    return { data: { directory: process.argv[3] } };
  },
} } });
generation = 3;
const v1First = { system: ["keep v1"] };
await v1["experimental.chat.system.transform"]({ sessionID: "session-b" }, v1First);
assert.deepEqual(v1First.system, ["keep v1", "catalog 3"]);
generation = 4;
const v1Next = { system: [] };
await v1["experimental.chat.system.transform"]({}, v1Next);
assert.deepEqual(v1Next.system, ["catalog 4"]);
assert.equal(calls[4].options.cwd, process.argv[3]);
assert.equal(calls[5].options.cwd, process.cwd());
for (const call of calls) {
  assert.equal(call.options.shell, false);
  assert.equal(call.options.windowsHide, true);
  assert.equal(call.options.encoding, "utf8");
  assert.equal(call.options.timeout, 10000);
  assert(call.options.maxBuffer > 0 && call.options.maxBuffer <= 1024 * 1024);
}
await cleanup();
assert(disposed);
console.log(JSON.stringify(calls.map(({ executable, args }) => ({ executable, args }))));
`
	output, stderr := openCodeRunNode(t, node, root, script, plugin, sessionDir)
	if stderr != "" {
		t.Fatalf("unexpected stderr: %s", stderr)
	}
	var calls []struct {
		Executable string   `json:"executable"`
		Args       []string `json:"args"`
	}
	if err := json.Unmarshal(output, &calls); err != nil {
		t.Fatalf("decode plugin results: %v: %s", err, output)
	}
	if len(calls) != 6 {
		t.Fatalf("plugin rendered %d times; want 6", len(calls))
	}
	wantArgs, _ := json.Marshal(hookArgs("opencode", "context", opts))
	for _, call := range calls {
		args, _ := json.Marshal(call.Args)
		if call.Executable != opts.Executable || !bytes.Equal(args, wantArgs) {
			t.Fatalf("plugin changed executable or arguments: %+v", call)
		}
	}
}

func TestOpenCodePluginRunsFreshExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native script fixture uses a POSIX shebang; both APIs and Windows argv are tested separately")
	}
	node := openCodeNode(t)
	if strings.ContainsAny(node, " \t\r\n") {
		t.Skip("Node path cannot be used as a shebang interpreter")
	}
	root := t.TempDir()
	sessionDir := filepath.Join(root, "working directory")
	if err := os.Mkdir(sessionDir, 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "registry's file.json")
	if err := os.WriteFile(config, []byte("first catalog"), 0600); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "lsops'$(not-a-shell).cjs")
	fixtureCode := "#!" + node + "\n" + `const fs = require("node:fs");
const args = process.argv.slice(2);
const config = args[args.indexOf("--config") + 1];
console.log(JSON.stringify({ args, cwd: process.cwd(), catalog: fs.readFileSync(config, "utf8") }));
`
	if err := os.WriteFile(fixture, []byte(fixtureCode), 0700); err != nil {
		t.Fatal(err)
	}
	opts := Options{Executable: fixture, ConfigPath: config, Mode: "output", Limit: 7}
	plugin := openCodeWritePlugin(t, root, opts)
	script := `import assert from "node:assert/strict";
import { writeFile } from "node:fs/promises";
import { pathToFileURL } from "node:url";
const { default: plugin } = await import(pathToFileURL(process.argv[2]).href);
let render;
await plugin.setup({ session: {
  get: async () => ({ location: { directory: process.argv[3] } }),
  hook: async (_, fn) => { render = fn; return { dispose() {} }; },
} });
const first = { sessionID: "same-session", system: [] };
await render(first);
assert.equal(first.system.length, 1);
await writeFile(process.argv[4], "changed catalog");
const next = { sessionID: "same-session", system: [] };
await render(next);
assert.equal(next.system.length, 1);
console.log(JSON.stringify([JSON.parse(first.system[0].text), JSON.parse(next.system[0].text)]));
`
	output, stderr := openCodeRunNode(t, node, root, script, plugin, sessionDir, config)
	if stderr != "" {
		t.Fatalf("unexpected stderr: %s", stderr)
	}
	var renders []struct {
		Args    []string `json:"args"`
		CWD     string   `json:"cwd"`
		Catalog string   `json:"catalog"`
	}
	if err := json.Unmarshal(output, &renders); err != nil {
		t.Fatalf("decode live fixture: %v: %s", err, output)
	}
	if len(renders) != 2 || renders[0].Catalog != "first catalog" || renders[1].Catalog != "changed catalog" {
		t.Fatalf("catalog did not refresh: %+v", renders)
	}
	wantArgs, _ := json.Marshal(hookArgs("opencode", "context", opts))
	for _, render := range renders {
		args, _ := json.Marshal(render.Args)
		// macOS may report /private/var for a temporary directory under /var.
		// Compare directory identity without relaxing the exact argv assertion.
		wantDir, err := os.Stat(sessionDir)
		if err != nil {
			t.Fatal(err)
		}
		gotDir, err := os.Stat(render.CWD)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(gotDir, wantDir) || !bytes.Equal(args, wantArgs) {
			t.Fatalf("child used wrong cwd/argv: %+v", render)
		}
	}
}

func openCodeNode(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is needed for generated-plugin runtime checks")
	}
	return node
}

func openCodeWritePlugin(t *testing.T, root string, opts Options) string {
	t.Helper()
	data, err := openCodeContent(opts)
	if err != nil {
		t.Fatal(err)
	}
	// The generated bytes are identical; .mjs makes Node parse the supported
	// module syntax without relying on a package.json or Node version defaults.
	path := filepath.Join(root, "lsops-plugin.mjs")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func openCodeRunNode(t *testing.T, node, cwd, script string, args ...string) ([]byte, string) {
	t.Helper()
	path := filepath.Join(cwd, "plugin-check.mjs")
	if err := os.WriteFile(path, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(node, append([]string{path}, args...)...)
	command.Dir = cwd
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("Node plugin check: %v\nstdout: %s\nstderr: %s", err, output, stderr.String())
	}
	return output, stderr.String()
}
