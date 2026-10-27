package manage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExecuteUsesArgvClosedStdinAndTemporaryCWD(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX script fixture; Windows npm adapter needs native operator acceptance")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "npm")
	script := "#!/bin/sh\nprintf 'cwd=%s\\n' \"$PWD\"\nfor arg do printf 'arg=%s\\n' \"$arg\"; done\nif read line; then exit 9; fi\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	var out, stderr bytes.Buffer
	if err := Execute(context.Background(), []string{"npm", "install", "--global", "--", "fixture;not-shell@latest"}, &out, &stderr); err != nil {
		t.Fatal(err, stderr.String())
	}
	if !strings.Contains(out.String(), "arg=fixture;not-shell@latest") {
		t.Fatal(out.String())
	}
	cwd := strings.TrimPrefix(strings.Split(out.String(), "\n")[0], "cwd=")
	if cwd == dir || !strings.Contains(cwd, "lsops-package-") {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(cwd); !os.IsNotExist(err) {
		t.Fatal("temporary cwd not cleaned", err)
	}
}
func TestExecuteMissingManagerAndCancellation(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := Execute(context.Background(), []string{"nonexistent"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("missing manager accepted")
	}
	if runtime.GOOS == "windows" {
		return
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "npm"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Execute(ctx, []string{"npm", "install"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("canceled context executed")
	}
}

func TestWindowsNPMAdapterNeverUsesShell(t *testing.T) {
	dir := t.TempDir()
	js := filepath.Join(dir, "node_modules", "npm", "bin", "npm-cli.js")
	if err := os.MkdirAll(filepath.Dir(js), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(js, []byte("// fixture only"), 0600); err != nil {
		t.Fatal(err)
	}
	lookup := func(name string) (string, error) { return filepath.Join(dir, name+".exe"), nil }
	path, args, err := resolveCommand([]string{"npm", "install", "--global", "--", "@acme/cli@latest"}, "windows", lookup)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "node.exe") || len(args) != 6 || args[0] != "node" || args[1] != js || args[5] != "@acme/cli@latest" {
		t.Fatal(path, args)
	}
	if err := os.Remove(js); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resolveCommand([]string{"npm", "install"}, "windows", lookup); err == nil {
		t.Fatal("missing npm JS accepted")
	}
	if _, _, err := resolveCommand(nil, "linux", lookup); err == nil {
		t.Fatal("empty argv accepted")
	}
	if _, _, err := resolveCommand([]string{"uv"}, "linux", func(string) (string, error) { return "relative/uv", nil }); err == nil {
		t.Fatal("relative manager accepted")
	}
}
