package manage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const MaxScriptSize = 4 << 20

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidateScriptURL(link string) error {
	u, err := url.Parse(link)
	if err != nil || len(link) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return errors.New("installer URL must be HTTPS without credentials, query or fragment (maximum 2048 bytes)")
	}
	return nil
}

func ScriptClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many installer redirects")
		}
		return ValidateScriptURL(req.URL.String())
	}}
}

// DownloadScript only retrieves bytes; it never evaluates them. TLS verification
// is required, redirects remain HTTPS, and both time and body size are bounded.
func DownloadScript(ctx context.Context, link string, client *http.Client) ([]byte, error) {
	if err := ValidateScriptURL(link); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("installer HTTP status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxScriptSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > MaxScriptSize {
		return nil, errors.New("installer must contain 1 byte to 4 MiB")
	}
	return data, nil
}

func ScriptDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// prepareScript downloads a complete script, verifies the exact approved digest,
// and writes it privately. Only the caller may subsequently invoke its runner.
func prepareScript(ctx context.Context, argv []string, dir string, client *http.Client) ([]string, error) {
	if len(argv) != 4 {
		return nil, errors.New("invalid verified-script invocation")
	}
	source, err := ParseOptions(argv[2], argv[1], argv[3])
	if err != nil {
		return nil, err
	}
	if source.URL == "" {
		return nil, errors.New("verified-script needs a script manager")
	}
	data, err := DownloadScript(ctx, source.URL, client)
	if err != nil {
		return nil, err
	}
	if ScriptDigest(data) != source.SHA256 {
		return nil, errors.New("installer SHA-256 changed or mismatched; refusing execution, download and review it before explicitly updating the pin and auto-update approval")
	}
	name := "installer.sh"
	if source.Manager == "pwsh" || source.Manager == "powershell" {
		name = "installer.ps1"
	}
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return nil, writeErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if source.Manager == "pwsh" || source.Manager == "powershell" {
		return []string{source.Manager, "-NoLogo", "-NoProfile", "-NonInteractive", "-File", path}, nil
	}
	return []string{source.Manager, path}, nil
}

// IsScriptCommand identifies the internal operation marker, not an executable.
func IsScriptCommand(argv []string) bool {
	return len(argv) > 0 && strings.EqualFold(argv[0], "verified-script")
}
