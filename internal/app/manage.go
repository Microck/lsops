package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Microck/lsops/internal/config"
	"github.com/Microck/lsops/internal/manage"
)

type installPlan struct {
	Name            string
	Link            string
	Manager, SHA256 string
	Args            []string
}

func sourceCommand(args []string, opts options, stdout, stderr io.Writer) int {
	var pos []string
	var manager, digest string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--help" {
			fmt.Fprintln(stdout, "usage: lsops source NAME HTTPS_URL [--manager npm|bun|pnpm|uv|pipx|brew|brew-cask|cargo|go|sh|bash|pwsh|powershell] [--sha256 HASH]; use - to clear")
			return 0
		}
		key, value, inline := strings.Cut(arg, "=")
		if key == "--manager" || key == "--sha256" {
			if !inline {
				i++
				if i >= len(args) {
					return fail(stderr, key+" needs a value", 2)
				}
				value = args[i]
			}
			if key == "--manager" {
				manager = value
			} else {
				digest = value
			}
			continue
		}
		if strings.HasPrefix(arg, "--") {
			return fail(stderr, "unknown source option: "+arg, 2)
		}
		pos = append(pos, arg)
	}
	if len(pos) != 2 {
		return fail(stderr, "usage: lsops source NAME HTTPS_URL [--manager MANAGER] [--sha256 HASH] (use - to clear)", 2)
	}
	link := pos[1]
	if link == "-" {
		link = ""
	}
	if err := config.SetInstallSpec(opts.configPath, pos[0], link, manager, digest); err != nil {
		return fail(stderr, err.Error(), 1)
	}
	fmt.Fprintln(stdout, "Saved install source. No installation or automatic update was enabled.")
	return 0
}

func plans(cfg config.Config, names []string, all, update bool) ([]installPlan, error) {
	if all && len(names) > 0 {
		return nil, errors.New("choose names or --all, not both")
	}
	if all {
		for n, t := range cfg.Tools {
			if t.InstallURL != "" && !t.Hide {
				names = append(names, n)
			}
		}
		sort.Strings(names)
	}
	if len(names) == 0 {
		return nil, errors.New("select at least one tool or use --all with configured install links")
	}
	var result []installPlan
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		t, ok := cfg.Tools[n]
		if !ok || t.Description == "" || t.Hide {
			return nil, fmt.Errorf("%q is not an enabled base-catalogue tool", n)
		}
		if t.InstallURL == "" {
			return nil, fmt.Errorf("%q has no install link; use lsops source", n)
		}
		s, err := config.InstallSource(t)
		if err != nil {
			return nil, err
		}
		result = append(result, installPlan{Name: n, Link: t.InstallURL, Manager: t.InstallManager, SHA256: t.InstallSHA256, Args: s.Command(update)})
	}
	return result, nil
}

func printPlans(w io.Writer, plan []installPlan) {
	for _, p := range plan {
		if manage.IsScriptCommand(p.Args) {
			fmt.Fprintf(w, "%s: %s\n  verified script: runner=%s sha256=%s (download, verify, then execute)\n", displayName(p.Name), p.Link, p.Manager, p.SHA256)
		} else {
			b, _ := json.Marshal(p.Args)
			fmt.Fprintf(w, "%s: %s\n  argv: %s\n", displayName(p.Name), p.Link, b)
		}
	}
}

// withUpdateLock prevents concurrent lsops installs/updates for this catalogue.
// A crash leaves a visible lock: never guess that another updater is dead.
func withUpdateLock(path string, f func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock := path + ".update-lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		return fmt.Errorf("cannot acquire update lock %q (another operation may be active; remove a stale lock only after checking): %w", lock, err)
	}
	defer os.Remove(lock)
	return f()
}

func executePlans(ctx context.Context, plan []installPlan, run manage.Runner, stdout, stderr io.Writer) error {
	var failures []error
	for _, p := range plan {
		if err := ctx.Err(); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Installing/updating %s\n", displayName(p.Name))
		if err := run(ctx, p.Args, stdout, stderr); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", p.Name, err))
		}
	}
	return errors.Join(failures...)
}

func installCommand(args []string, opts options, update bool, stdout, stderr io.Writer) int {
	return installWithRunner(args, opts, update, stdout, stderr, manage.Execute)
}
func installWithRunner(args []string, opts options, update bool, stdout, stderr io.Writer, run manage.Runner) int {
	fs := flag.NewFlagSet("install/update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	yes := fs.Bool("yes", false, "execute the displayed install/update operations")
	allowScripts := fs.Bool("allow-scripts", false, "also authorize hash-pinned installer scripts")
	all := fs.Bool("all", false, "select all base tools with install links")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return fail(stderr, err.Error(), 1)
	}
	plan, err := plans(cfg, fs.Args(), *all, update)
	if err != nil {
		return fail(stderr, err.Error(), 2)
	}
	printPlans(stdout, plan)
	if !*yes {
		fmt.Fprintln(stdout, "Preview only. Review the package identities, then rerun with --yes before names. Package install/build scripts may execute; latest versions may introduce breaking changes.")
		return 0
	}
	if err := authorizeScripts(plan, *allowScripts); err != nil {
		return fail(stderr, err.Error(), 2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err = withUpdateLock(opts.configPath, func() error { return executePlans(ctx, plan, run, stdout, stderr) })
	if err != nil {
		return fail(stderr, err.Error(), 1)
	}
	return 0
}

// Bundles contain only portable descriptive metadata and install links. They
// never carry project trust, local paths, secrets or auto-update approvals.
func portable(cfg config.Config) config.Config {
	out := config.Config{Version: 1, Tools: map[string]config.Tool{}}
	for n, t := range cfg.Tools {
		if t.Description != "" && !t.Hide {
			out.Tools[n] = config.Tool{Description: t.Description, Why: t.Why, When: t.When, InstallURL: t.InstallURL, InstallManager: t.InstallManager, InstallSHA256: t.InstallSHA256}
		}
	}
	return out
}

func readBundle(location string, client *http.Client) ([]byte, error) {
	var r io.ReadCloser
	if strings.HasPrefix(location, "https://") {
		u, err := url.Parse(location)
		if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
			return nil, errors.New("bundle URL must be HTTPS without credentials or fragment")
		}
		response, err := client.Get(location)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("bundle HTTP status %d", response.StatusCode)
		}
		r = response.Body
	} else {
		if strings.Contains(location, "://") {
			return nil, errors.New("remote bundles require HTTPS")
		}
		f, err := os.Open(location)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	data, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, errors.New("bundle exceeds 1 MiB")
	}
	return data, nil
}

func bundleCommand(args []string, opts options, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "export" {
		cfg, err := config.Load(opts.configPath)
		if err != nil {
			return fail(stderr, err.Error(), 1)
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(portable(cfg)); err != nil {
			return fail(stderr, err.Error(), 1)
		}
		return 0
	}
	if len(args) == 0 || args[0] != "import" {
		return fail(stderr, "usage: lsops bundle export | bundle import [--yes] FILE_OR_HTTPS_URL", 2)
	}
	fs := flag.NewFlagSet("bundle import", flag.ContinueOnError)
	fs.SetOutput(stderr)
	yes := fs.Bool("yes", false, "merge portable entries, overwriting their metadata but not local paths")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 1 {
		return fail(stderr, "supply one bundle file or HTTPS URL", 2)
	}
	client := bundleClient()
	data, err := readBundle(fs.Arg(0), client)
	if err != nil {
		return fail(stderr, err.Error(), 1)
	}
	cfg, err := config.Decode(data)
	if err != nil {
		return fail(stderr, err.Error(), 1)
	}
	// Reject, rather than silently accept, nonportable inputs.
	if len(cfg.Projects) > 0 {
		return fail(stderr, "bundle cannot include project trust", 1)
	}
	names := make([]string, 0, len(cfg.Tools))
	for n, t := range cfg.Tools {
		if t.Path != "" || t.Help != "" || t.Hide || t.Include || t.Description == "" {
			return fail(stderr, "bundle must contain only portable described tools (use bundle export)", 1)
		}
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		t := cfg.Tools[n]
		fmt.Fprintf(stdout, "%s: %s\n  install link: %s\n  manager: %s\n  sha256: %s\n", displayName(n), t.Description, t.InstallURL, t.InstallManager, t.InstallSHA256)
	}
	if !*yes {
		fmt.Fprintln(stdout, "Preview only. Download remote bundles to a local file for review, then import that same file with --yes. Existing descriptions and links are replaced; local paths are preserved. Import never installs or enables updates.")
		return 0
	}
	// Approving a live URL after preview would execute a different fetch. Require
	// a reviewable local artifact for the write step, not mutable remote content.
	if strings.Contains(fs.Arg(0), "://") {
		return fail(stderr, "download and review the bundle locally before importing with --yes", 2)
	}
	if err := config.ImportTools(opts.configPath, cfg); err != nil {
		return fail(stderr, err.Error(), 1)
	}
	fmt.Fprintln(stdout, "Imported catalogue. Preview lsops install --all next; auto-updates remain separately approved.")
	return 0
}

func bundleClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many bundle redirects")
		}
		if req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("bundle redirect must remain HTTPS without credentials")
		}
		return nil
	}}
}

func authorizeScripts(plan []installPlan, allow bool) error {
	if !allow {
		for _, p := range plan {
			if manage.IsScriptCommand(p.Args) {
				return errors.New("selection includes installer scripts; review their contents and pins, then explicitly add --allow-scripts")
			}
		}
	}
	return nil
}
