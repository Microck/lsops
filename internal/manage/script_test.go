package manage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func scriptServer(t *testing.T, body string) (string, *http.Client) {
	t.Helper()
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
	t.Cleanup(s.Close)
	c := ScriptClient()
	c.Transport = s.Client().Transport
	return s.URL, c
}
func TestPreparePinnedScriptAndRunnerArgs(t *testing.T) {
	body := "#!/bin/sh\nprintf fixture\\n\n"
	link, client := scriptServer(t, body)
	digest := ScriptDigest([]byte(body))
	for _, manager := range []string{"sh", "bash", "pwsh", "powershell"} {
		s, err := ParseOptions(link, manager, digest)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.Command(false), s.Command(true)) {
			t.Fatal("script update must use same reviewed entrypoint")
		}
		dir := t.TempDir()
		argv, err := prepareScript(context.Background(), s.Command(false), dir, client)
		if err != nil {
			t.Fatal(err)
		}
		if argv[0] != manager {
			t.Fatal(argv)
		}
		path := argv[len(argv)-1]
		data, err := os.ReadFile(path)
		if err != nil || string(data) != body {
			t.Fatal(string(data), err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
			t.Fatal(info.Mode())
		}
		if (manager == "pwsh" || manager == "powershell") && !reflect.DeepEqual(argv[:len(argv)-1], []string{manager, "-NoLogo", "-NoProfile", "-NonInteractive", "-File"}) {
			t.Fatal(argv)
		}
	}
}
func TestScriptDigestMismatchNeverWritesOrExecutes(t *testing.T) {
	link, client := scriptServer(t, "changed installer")
	dir := t.TempDir()
	_, err := prepareScript(context.Background(), []string{"verified-script", "sh", link, strings.Repeat("0", 64)}, dir, client)
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("mismatch wrote executable script")
	}
	if _, err := prepareScript(context.Background(), []string{"verified-script"}, dir, client); err == nil {
		t.Fatal("short invocation accepted")
	}
}
func TestScriptFetchBoundaries(t *testing.T) {
	for _, body := range []string{"", strings.Repeat("x", MaxScriptSize+1)} {
		link, c := scriptServer(t, body)
		if _, err := DownloadScript(context.Background(), link, c); err == nil {
			t.Fatal("invalid body size accepted")
		}
	}
	link, c := scriptServer(t, "hello")
	if _, err := DownloadScript(context.Background(), link, ScriptClient()); err == nil {
		t.Fatal("untrusted TLS accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DownloadScript(ctx, link, c); err == nil {
		t.Fatal("canceled download accepted")
	}
	for _, u := range []string{"http://example.test/install.sh", "https://user:pass@example.test/install.sh", "https://example.test/install.sh?token=secret", "https://example.test/install.sh#fragment", "file:///tmp/script"} {
		if err := ValidateScriptURL(u); err == nil {
			t.Fatal(u)
		}
	}
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid/install.sh", http.StatusFound)
	}))
	defer s.Close()
	c = ScriptClient()
	c.Transport = s.Client().Transport
	if _, err := DownloadScript(context.Background(), s.URL, c); err == nil {
		t.Fatal("downgrade redirect accepted")
	}
	s2 := httptest.NewTLSServer(http.NotFoundHandler())
	defer s2.Close()
	c = ScriptClient()
	c.Transport = s2.Client().Transport
	if _, err := DownloadScript(context.Background(), s2.URL, c); err == nil {
		t.Fatal("404 accepted")
	}
}
func TestPinnedFixtureScriptExecutesOnlyAfterVerification(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX sh fixture; PowerShell argv is tested separately")
	}
	marker := filepath.Join(t.TempDir(), "marker")
	t.Setenv("LSOPS_SCRIPT_FIXTURE_MARKER", marker)
	body := "#!/bin/sh\nprintf 'fixture ran' > \"$LSOPS_SCRIPT_FIXTURE_MARKER\"\nprintf 'fixture output'\n"
	link, client := scriptServer(t, body)
	argv := []string{"verified-script", "sh", link, ScriptDigest([]byte(body))}
	var out bytes.Buffer
	if err := executeWithClient(context.Background(), argv, &out, io.Discard, client); err != nil {
		t.Fatal(err)
	}
	if out.String() != "fixture output" {
		t.Fatal(out.String())
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "fixture ran" {
		t.Fatal(string(data), err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	argv[3] = strings.Repeat("0", 64)
	if err := executeWithClient(context.Background(), argv, &out, io.Discard, client); err == nil {
		t.Fatal("mismatch executed")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("mismatch executed script")
	}
}
