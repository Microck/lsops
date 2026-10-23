package integrate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestOMPPathsAndLifecycle(t *testing.T) {
	opts := testOptions(t)
	for _, tc := range []struct{ scope, custom, want string }{
		{"user", "", filepath.Join(opts.Home, ".omp", "agent", "extensions", "lsops.js")},
		{"user", filepath.Join(opts.Home, "named profile"), filepath.Join(opts.Home, "named profile", "extensions", "lsops.js")},
		{"project", filepath.Join(opts.Home, "ignored"), filepath.Join(opts.ProjectDir, ".omp", "extensions", "lsops.js")},
	} {
		o := opts
		o.Scope = tc.scope
		o.OMPHome = tc.custom
		path, err := destination("omp", o)
		if err != nil || path != tc.want {
			t.Fatalf("path %q %v; want %q", path, err, tc.want)
		}
		result, err := Install("omp", o)
		if err != nil || !result.Installed || !result.Changed {
			t.Fatalf("install %+v %v", result, err)
		}
		data, err := os.ReadFile(path)
		if err != nil || !bytes.HasPrefix(data, []byte(ompMarker+"\n")) {
			t.Fatalf("header %s %v", data, err)
		}
		if isManagedPluginFor("opencode", data) {
			t.Fatal("cross-target ownership")
		}
		status, err := Status("omp", o)
		if err != nil || !status.Installed {
			t.Fatalf("status %+v %v", status, err)
		}
		again, err := Install("omp", o)
		if err != nil || again.Changed {
			t.Fatalf("repeat %+v %v", again, err)
		}
		removed, err := Uninstall("omp", o)
		if err != nil || !removed.Changed || removed.Installed {
			t.Fatalf("remove %+v %v", removed, err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("still present %v", err)
		}
		foreign := []byte("export default function other() {}\n")
		if err := os.WriteFile(path, foreign, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Install("omp", o); err == nil {
			t.Fatal("overwrote foreign extension")
		}
		if _, err := Uninstall("omp", o); err == nil {
			t.Fatal("removed foreign extension")
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(after, foreign) {
			t.Fatal("foreign changed")
		}
	}
}

func TestOMPLifecycleTimingAndIsolation(t *testing.T) {
	node := openCodeNode(t)
	root := t.TempDir()
	opts := testOptions(t)
	opts.Mode = "output"
	opts.Limit = 7
	data, err := Preview("omp", opts)
	if err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(root, "omp-plugin.mjs")
	if err = os.WriteFile(plugin, data, 0600); err != nil {
		t.Fatal(err)
	}
	script := `import assert from "node:assert/strict";
import cp from "node:child_process";
import {syncBuiltinESMExports} from "node:module";
import {pathToFileURL} from "node:url";
const calls=[];const warnings=[];const pending=[];let generation=1;let fail=false;let defer=false;
cp.execFile=(executable,args,options,callback)=>{calls.push({executable,args,options});const done=()=>callback(fail?new Error("PRIVATE STDERR"):null,fail?"PRIVATE PARTIAL":"catalog "+generation);if(defer)pending.push(done);else queueMicrotask(done);return {};};
syncBuiltinESMExports();console.warn=x=>warnings.push(x);
const {default:factory}=await import(pathToFileURL(process.argv[2]).href);
const handlers=new Map();const sent=[];
factory({on(name,handler){assert(!handlers.has(name));handlers.set(name,handler);},sendMessage(message,options){sent.push({message,options});}});
assert.deepEqual([...handlers.keys()],["session_start","session_switch","session_branch","session_tree","session_compact","session_shutdown"]);
assert.equal(calls.length,0);
assert(!handlers.has("context"));assert(!handlers.has("before_agent_start"));assert(!handlers.has("model_select"));
const home={cwd:process.argv[3]},project={cwd:process.argv[4]};
await handlers.get("session_start")({},home);assert.equal(sent.length,1);assert.equal(sent[0].message.content,"catalog 1");assert.equal(sent[0].message.display,false);assert.equal(sent[0].message.customType,"lsops.catalogue");assert.deepEqual(sent[0].options,{triggerTurn:false});
// Model and tool continuations cannot call lsops or inject another message.
for(let i=0;i<20;i++)for(const event of ["context","turn_start","tool_result","before_agent_start","model_select"])await handlers.get(event)?.({},home);
assert.equal(calls.length,1);assert.equal(sent.length,1);
generation=2;await handlers.get("session_compact")({},home);assert.equal(sent.length,2);assert.equal(sent[1].message.content,"catalog 2");
for(const event of ["session_switch","session_branch","session_tree"])await handlers.get(event)({},project);
assert.equal(sent.length,5);assert.equal(calls[0].options.cwd,home.cwd);assert.equal(calls[2].options.cwd,project.cwd);
assert.equal(calls[0].options.shell,false);assert.equal(calls[0].options.timeout,10000);assert.equal(calls[0].options.maxBuffer,262144);assert.equal(calls[0].args[calls[0].args.indexOf("--harness")+1],"omp");assert.equal(calls[0].args[calls[0].args.indexOf("--mode")+1],"output");assert.equal(calls[0].args[calls[0].args.indexOf("--limit")+1],"7");
fail=true;await handlers.get("session_compact")({},home);assert.equal(sent.length,5);assert.equal(warnings.length,1);assert(!warnings[0].includes("PRIVATE"));fail=false;
const count=calls.length;await handlers.get("session_switch")({},{cwd:"relative"});assert.equal(calls.length,count);assert.equal(sent.length,5);
await handlers.get("session_switch")({},{cwd:home.cwd+"\0bad"});assert.equal(calls.length,count);assert.equal(sent.length,5);
defer=true;const old=handlers.get("session_start")({},home);const fresh=handlers.get("session_switch")({},project);pending.shift()();await old;assert.equal(sent.length,5);pending.shift()();await fresh;assert.equal(sent.length,6);
const dying=handlers.get("session_compact")({},project);handlers.get("session_shutdown")();pending.shift()();await dying;assert.equal(sent.length,6);
console.log("PASS");`
	output, diagnostic := openCodeRunNode(t, node, root, script, plugin, opts.Home, opts.ProjectDir)
	if diagnostic != "" || string(bytes.TrimSpace(output)) != "PASS" {
		t.Fatalf("runtime %s %s", output, diagnostic)
	}
}

func TestOMPRejectsSymlinkDestination(t *testing.T) {
	opts := testOptions(t)
	path, err := destination("omp", opts)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(t.TempDir(), "foreign.js")
	original := []byte("foreign contents")
	if err = os.WriteFile(victim, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(victim, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err = Install("omp", opts); err == nil {
		t.Fatal("installed through symlink")
	}
	if _, err = Uninstall("omp", opts); err == nil {
		t.Fatal("removed through symlink")
	}
	after, _ := os.ReadFile(victim)
	if !bytes.Equal(after, original) {
		t.Fatal("symlink target modified")
	}
}
