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

func TestOMPContextRefreshAndIsolation(t *testing.T) {
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
const calls=[];const warnings=[];let generation=1;let fail=false;
cp.execFile=(executable,args,options,callback)=>{calls.push({executable,args,options});queueMicrotask(()=>callback(fail?new Error("PRIVATE STDERR"):null,fail?"PRIVATE PARTIAL":"catalog "+generation));return {};};
syncBuiltinESMExports();console.warn=x=>warnings.push(x);
const {default:factory}=await import(pathToFileURL(process.argv[2]).href);let context;
factory({on(name,handler){assert.equal(name,"context");context=handler;}});assert.equal(calls.length,0);
const keep={role:"user",content:"keep"};const other={role:"custom",customType:"foreign",content:"leave alone"};
const old={role:"custom",customType:"lsops.catalogue",content:"old inventory"};
const event={messages:[keep,other,old]};
let result=await context(event,{cwd:process.argv[3]});assert.deepEqual(event.messages,[keep,other,old]);assert.equal(result.messages.length,3);assert.equal(result.messages[0],keep);assert.equal(result.messages[1],other);assert.equal(result.messages[2].content,"catalog 1");assert.equal(result.messages[2].display,false);
generation=2;result=await context({messages:result.messages},{cwd:process.argv[4]});assert.equal(result.messages.length,3);assert.equal(result.messages[2].content,"catalog 2");assert.equal(calls[0].options.cwd,process.argv[3]);assert.equal(calls[1].options.cwd,process.argv[4]);
assert.equal(calls[0].options.shell,false);assert.equal(calls[0].options.timeout,10000);assert.equal(calls[0].options.maxBuffer,262144);assert.equal(calls[0].args[calls[0].args.indexOf("--harness")+1],"omp");assert.equal(calls[0].args[calls[0].args.indexOf("--mode")+1],"output");assert.equal(calls[0].args[calls[0].args.indexOf("--limit")+1],"7");
fail=true;result=await context({messages:result.messages},{cwd:process.argv[3]});assert.deepEqual(result.messages,[keep,other]);assert.equal(warnings.length,1);assert(!warnings[0].includes("PRIVATE"));
const count=calls.length;result=await context(event,{cwd:"relative"});assert.deepEqual(result.messages,[keep,other]);assert.equal(calls.length,count);
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
