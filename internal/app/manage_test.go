package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Microck/lsops/internal/config"
)

func managedFixture(t *testing.T) (options, config.Config) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Config{Version: 1, Tools: map[string]config.Tool{
		"alpha":  {Description: "Alpha CLI", InstallURL: "https://www.npmjs.com/package/alpha"},
		"beta":   {Description: "Beta CLI", InstallURL: "https://pypi.org/project/beta"},
		"manual": {Description: "Manual CLI"},
	}}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	return options{configPath: path}, cfg
}
func TestInstallPreviewAndExplicitExecution(t *testing.T) {
	opts, _ := managedFixture(t)
	var out, errout bytes.Buffer
	var calls [][]string
	run := func(_ context.Context, argv []string, _, _ io.Writer) error { calls = append(calls, argv); return nil }
	if code := installWithRunner([]string{"--all"}, opts, false, &out, &errout, run); code != 0 || len(calls) != 0 {
		t.Fatalf("%d %s", code, errout.String())
	}
	if !strings.Contains(out.String(), "Preview only") || !strings.Contains(out.String(), "alpha@latest") {
		t.Fatal(out.String())
	}
	if code := installWithRunner([]string{"--yes", "alpha"}, opts, true, &out, &errout, run); code != 0 {
		t.Fatal(errout.String())
	}
	if len(calls) != 1 || !reflect.DeepEqual(calls[0], []string{"npm", "install", "--global", "--", "alpha@latest"}) {
		t.Fatal(calls)
	}
}
func TestInstallValidatesWholeSelectionBeforeExecution(t *testing.T) {
	opts, _ := managedFixture(t)
	calls := 0
	run := func(context.Context, []string, io.Writer, io.Writer) error { calls++; return nil }
	for _, args := range [][]string{{"--yes", "alpha", "missing"}, {"--yes", "manual"}, {"--yes", "--all", "alpha"}, {"--yes"}} {
		if code := installWithRunner(args, opts, false, io.Discard, io.Discard, run); code == 0 {
			t.Fatal(args)
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
}
func TestFailuresContinueAndExitNonzero(t *testing.T) {
	opts, _ := managedFixture(t)
	calls := 0
	run := func(context.Context, []string, io.Writer, io.Writer) error {
		calls++
		return errors.New("fixture failure")
	}
	if code := installWithRunner([]string{"--yes", "--all"}, opts, true, io.Discard, io.Discard, run); code != 1 || calls != 2 {
		t.Fatalf("code=%d calls=%d", code, calls)
	}
	if _, err := os.Stat(opts.configPath + ".update-lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}
func TestAutoUpdateRequiresExactSeparateApproval(t *testing.T) {
	opts, cfg := managedFixture(t)
	calls := 0
	run := func(context.Context, []string, io.Writer, io.Writer) error { calls++; return nil }
	if active, err := autoPass(context.Background(), opts.configPath, run, io.Discard, io.Discard); err != nil || active {
		t.Fatal(active, err)
	}
	if code := autoUpdateCommand([]string{"enable", "alpha"}, opts, io.Discard, io.Discard); code != 0 {
		t.Fatal(code)
	}
	if _, err := os.Stat(approvalsPath(opts.configPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview wrote approvals")
	}
	if code := autoUpdateCommand([]string{"enable", "--yes", "alpha"}, opts, io.Discard, io.Discard); code != 0 {
		t.Fatal(code)
	}
	if active, err := autoPass(context.Background(), opts.configPath, run, io.Discard, io.Discard); err != nil || !active || calls != 1 {
		t.Fatal(active, err, calls)
	}
	// Importing a replacement source cannot change the previously approved identity.
	replacement := cfg.Tools["alpha"]
	replacement.InstallURL = "https://www.npmjs.com/package/replacement"
	cfg.Tools["alpha"] = replacement
	if err := config.ImportTools(opts.configPath, portable(cfg)); err != nil {
		t.Fatal(err)
	}
	if active, err := autoPass(context.Background(), opts.configPath, run, io.Discard, io.Discard); err != nil || active || calls != 1 {
		t.Fatal(active, err, calls)
	}
	if code := autoUpdateCommand([]string{"disable", "--all"}, opts, io.Discard, io.Discard); code != 0 {
		t.Fatal(code)
	}
	approved, err := loadApprovals(opts.configPath)
	if err != nil || len(approved.Tools) != 0 {
		t.Fatal(approved, err)
	}
}
func TestAutomaticPlansSkipHiddenAndRemoved(t *testing.T) {
	_, cfg := managedFixture(t)
	approved := portable(cfg)
	a := cfg.Tools["alpha"]
	a.Hide = true
	cfg.Tools["alpha"] = a
	delete(cfg.Tools, "beta")
	p, skip, err := automaticPlans(cfg, approved)
	if err != nil || len(p) != 0 || len(skip) != 3 {
		t.Fatal(p, skip, err)
	}
}
func TestAutoLoopStopsWhenDisabledAndHonorsCancellation(t *testing.T) {
	opts, _ := managedFixture(t)
	run := func(context.Context, []string, io.Writer, io.Writer) error {
		t.Fatal("unexpected execution")
		return nil
	}
	if err := autoLoop(context.Background(), opts.configPath, time.Hour, false, run, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if code := autoUpdateCommand([]string{"enable", "--yes", "alpha"}, opts, io.Discard, io.Discard); code != 0 {
		t.Fatal(code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	run = func(context.Context, []string, io.Writer, io.Writer) error { cancel(); return nil }
	if err := autoLoop(ctx, opts.configPath, time.Hour, false, run, io.Discard, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestLockAndApprovalSymlinkRefusal(t *testing.T) {
	opts, _ := managedFixture(t)
	if err := withUpdateLock(opts.configPath, func() error {
		if err := withUpdateLock(opts.configPath, func() error { t.Fatal("nested lock acquired"); return nil }); err == nil {
			t.Fatal("missing lock error")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(opts.configPath, approvalsPath(opts.configPath)); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := loadApprovals(opts.configPath); err == nil {
		t.Fatal("accepted symlink")
	}
}
func TestPortableBundleRoundTrip(t *testing.T) {
	opts, cfg := managedFixture(t)
	a := cfg.Tools["alpha"]
	a.Path = "/some/local/path"
	a.Help = "private local help"
	cfg.Tools["alpha"] = a
	cfg.Projects = []string{filepath.Clean(t.TempDir())}
	if err := config.Save(opts.configPath, cfg); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := bundleCommand([]string{"export"}, opts, &out, io.Discard); code != 0 {
		t.Fatal(code)
	}
	if strings.Contains(out.String(), "/some/local") || strings.Contains(out.String(), "projects") || strings.Contains(out.String(), "help") {
		t.Fatal(out.String())
	}
	bundle := filepath.Join(t.TempDir(), "bundle.json")
	if err := os.WriteFile(bundle, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	dest := options{configPath: filepath.Join(t.TempDir(), "config.json")}
	if code := bundleCommand([]string{"import", bundle}, dest, io.Discard, io.Discard); code != 0 {
		t.Fatal(code)
	}
	if _, err := os.Stat(dest.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview changed config")
	}
	if code := bundleCommand([]string{"import", "--yes", bundle}, dest, io.Discard, io.Discard); code != 0 {
		t.Fatal(code)
	}
	loaded, err := config.Load(dest.configPath)
	if err != nil || loaded.Tools["alpha"].InstallURL != a.InstallURL {
		t.Fatal(loaded, err)
	}
	if _, err := os.Stat(approvalsPath(dest.configPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("import enabled updates")
	}
}
func TestBundleLimitsAndHTTPS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/big" {
			io.WriteString(w, strings.Repeat("a", (1<<20)+1))
			return
		}
		io.WriteString(w, `{"version":1,"tools":{}}`)
	}))
	defer server.Close()
	if _, err := readBundle(server.URL, server.Client()); err != nil {
		t.Fatal(err)
	}
	if _, err := readBundle(server.URL+"/big", server.Client()); err == nil {
		t.Fatal("oversized accepted")
	}
	if _, err := readBundle("http://example.test/tools.json", server.Client()); err == nil {
		t.Fatal("HTTP accepted")
	}
	if _, err := readBundle("https://user:pass@example.test/tools.json", server.Client()); err == nil {
		t.Fatal("credentials accepted")
	}
}
func TestInstallIgnoresProjectOverlays(t *testing.T) {
	opts, cfg := managedFixture(t)
	project := t.TempDir()
	overlay := config.Config{Version: 1, Tools: map[string]config.Tool{"alpha": {Description: "Override", InstallURL: "https://www.npmjs.com/package/evil"}}}
	if err := config.Save(filepath.Join(project, config.ProjectFile), overlay); err != nil {
		t.Fatal(err)
	}
	cfg.Projects = []string{project}
	if err := config.Save(opts.configPath, cfg); err != nil {
		t.Fatal(err)
	}
	// Even a trusted overlay is never consulted by package operations.
	var got []string
	run := func(_ context.Context, args []string, _, _ io.Writer) error { got = args; return nil }
	if code := installWithRunner([]string{"--yes", "alpha"}, opts, false, io.Discard, io.Discard, run); code != 0 {
		t.Fatal(code)
	}
	if got[len(got)-1] != "alpha@latest" {
		t.Fatal(got)
	}
}

func TestAddInstallLinkAtomicAndPreserved(t *testing.T) {
	opts, _ := managedFixture(t)
	call := func(args ...string) int {
		return Run(append([]string{"--config", opts.configPath}, args...), io.Discard, io.Discard, "test")
	}
	if code := call("add", "alpha", "New description", "--install-url", "https://www.npmjs.com/package/new-alpha"); code != 0 {
		t.Fatal(code)
	}
	if code := call("add", "alpha", "Another description"); code != 0 {
		t.Fatal(code)
	}
	cfg, err := config.Load(opts.configPath)
	if err != nil || cfg.Tools["alpha"].InstallURL != "https://www.npmjs.com/package/new-alpha" {
		t.Fatal(cfg, err)
	}
	before, _ := os.ReadFile(opts.configPath)
	if code := call("add", "alpha", "Must not save", "--install-url", "https://evil.test/install.sh"); code == 0 {
		t.Fatal("invalid link accepted")
	}
	after, _ := os.ReadFile(opts.configPath)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid update partially saved")
	}
	if code := call("add", "alpha", "Another description", "--install-url="); code != 0 {
		t.Fatal(code)
	}
	cfg, err = config.Load(opts.configPath)
	if err != nil || cfg.Tools["alpha"].InstallURL != "" {
		t.Fatal(cfg, err)
	}
}
func TestBundleRejectsTrustPathsMalformedAndUnknownFields(t *testing.T) {
	opts, _ := managedFixture(t)
	for _, input := range []string{
		`{"version":1,"tools":{"a":{"description":"a","path":"/private/bin/a"}}}`,
		`{"version":1,"tools":{"a":{"description":"a","help":"run arbitrary command"}}}`,
		`{"version":1,"tools":{"a":{"description":"a","auto_update":true}}}`,
		`{"version":1,"tools":{"a":{"description":"a","install_url":"https://bad.example/x"}}}`,
		`{"version":1,"tools":{}} {}`,
		`null`,
	} {
		path := filepath.Join(t.TempDir(), "input.json")
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(opts.configPath)
		if code := bundleCommand([]string{"import", "--yes", path}, opts, io.Discard, io.Discard); code == 0 {
			t.Fatal(input)
		}
		after, _ := os.ReadFile(opts.configPath)
		if !bytes.Equal(before, after) {
			t.Fatal("rejected import changed config")
		}
	}
}
func TestImportPreservesLocalSettingsAndUnrelatedTools(t *testing.T) {
	opts, cfg := managedFixture(t)
	a := cfg.Tools["alpha"]
	a.Path = "/local/alpha"
	a.Hide = true
	cfg.Tools["alpha"] = a
	if err := config.Save(opts.configPath, cfg); err != nil {
		t.Fatal(err)
	}
	incoming := config.Config{Version: 1, Tools: map[string]config.Tool{"alpha": {Description: "Imported", InstallURL: "https://www.npmjs.com/package/alpha"}}}
	if err := config.ImportTools(opts.configPath, incoming); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(opts.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tools["alpha"].Path != a.Path || !got.Tools["alpha"].Hide || got.Tools["alpha"].Description != "Imported" || got.Tools["beta"].Description == "" {
		t.Fatal(got)
	}
}
func TestAutoCommandsValidateArguments(t *testing.T) {
	opts, _ := managedFixture(t)
	for _, args := range [][]string{{}, {"wat"}, {"run", "--interval", "1s"}, {"run", "extra"}, {"status", "extra"}, {"disable"}, {"disable", "--all", "alpha"}, {"enable", "--yes", "manual"}} {
		if code := autoUpdateCommand(args, opts, io.Discard, io.Discard); code == 0 {
			t.Fatal(args)
		}
	}
	for _, action := range []string{"enable", "disable", "status", "run"} {
		if code := autoUpdateCommand([]string{action, "--help"}, opts, io.Discard, io.Discard); code != 0 {
			t.Fatal(action, code)
		}
	}
}
func TestListWithLinksIsStillNonExecuting(t *testing.T) {
	opts, _ := managedFixture(t)
	var out, errout bytes.Buffer
	// Parsing links is local. No managers are needed for JSON inventory.
	t.Setenv("PATH", t.TempDir())
	if code := Run([]string{"--config", opts.configPath, "--json"}, &out, &errout, "test"); code != 0 {
		t.Fatal(code, errout.String())
	}
	if !strings.Contains(out.String(), `"install_url": "https://www.npmjs.com/package/alpha"`) {
		t.Fatal(out.String())
	}
	if _, err := os.Stat(approvalsPath(opts.configPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("listing created approvals")
	}
}
func TestBundleRedirectPolicy(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid/bundle", http.StatusFound)
	}))
	defer server.Close()
	client := bundleClient()
	client.Transport = server.Client().Transport
	if _, err := readBundle(server.URL, client); err == nil || !strings.Contains(err.Error(), "remain HTTPS") {
		t.Fatal(err)
	}
	if _, err := readBundle(server.URL, bundleClient()); err == nil {
		t.Fatal("untrusted TLS accepted")
	}
}
