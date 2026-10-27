package manage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Runner is injectable so tests never install packages on the host.
type Runner func(context.Context, []string, io.Writer, io.Writer) error

// Execute resolves a real executable on PATH and invokes argv directly, not a
// shell. stdin is closed: unattended jobs must fail instead of asking for sudo.
func Execute(ctx context.Context, argv []string, stdout, stderr io.Writer) error {
	return executeWithClient(ctx, argv, stdout, stderr, ScriptClient())
}

func executeWithClient(ctx context.Context, argv []string, stdout, stderr io.Writer, client *http.Client) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	dir, err := os.MkdirTemp("", "lsops-package-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if IsScriptCommand(argv) {
		argv, err = prepareScript(ctx, argv, dir, client)
		if err != nil {
			return err
		}
	}
	path, argv, err := resolveCommand(argv, runtime.GOOS, exec.LookPath)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, path, argv[1:]...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 5 * time.Second
	// Work outside the calling project; user/global manager configuration applies.
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed (no automatic retry in this pass): %w", argv[0], err)
	}
	return nil
}

// resolveCommand is separately testable without invoking a native manager.
func resolveCommand(argv []string, goos string, lookPath func(string) (string, error)) (string, []string, error) {
	if len(argv) == 0 {
		return "", nil, fmt.Errorf("missing package manager command")
	}
	path, err := lookPath(argv[0])
	if err != nil {
		return "", nil, fmt.Errorf("%s is required on PATH: %w", argv[0], err)
	}
	if !filepath.IsAbs(path) {
		return "", nil, fmt.Errorf("package manager must resolve to an absolute path")
	}
	// npm.cmd requires cmd.exe on Windows. Do not use a shell to execute it;
	// invoke the official npm CLI JS beside its launcher through node instead.
	if goos == "windows" && (argv[0] == "npm" || argv[0] == "pnpm") {
		js := filepath.Join(filepath.Dir(path), "node_modules", "npm", "bin", "npm-cli.js")
		if argv[0] == "pnpm" {
			js = filepath.Join(filepath.Dir(path), "node_modules", "pnpm", "bin", "pnpm.cjs")
		}
		info, e := os.Stat(js)
		if e != nil || !info.Mode().IsRegular() {
			return "", nil, fmt.Errorf("cannot find the manager CLI JavaScript beside its launcher; use a standard Node.js/npm installation layout")
		}
		node, e := lookPath("node")
		if e != nil || !filepath.IsAbs(node) {
			return "", nil, fmt.Errorf("node is required on PATH")
		}
		path = node
		argv = append([]string{"node", js}, argv[1:]...)
	}
	return path, argv, nil
}
