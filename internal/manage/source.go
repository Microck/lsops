// Package manage handles explicitly requested package-manager operations.
// Catalogue listing and hooks never call this package's execution functions.
package manage

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Source is derived from a supported public install link, never shell text.
type Source struct{ Manager, Package, URL, SHA256 string }

var npmName = regexp.MustCompile(`^(?:@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`)
var pythonName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var brewName = regexp.MustCompile(`^[a-z0-9][a-z0-9+@._-]*$`)
var githubPart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

var goName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~/-]*$`)

// Parse accepts only package pages with unambiguous registry identities.
// It does not fetch the link or infer an executable's identity from its name.
func Parse(link string) (Source, error) { return ParseOptions(link, "", "") }

// ParseOptions permits explicit alternate managers and hash-pinned HTTPS
// installer scripts. No interpreter is ever inferred from a URL extension.
func ParseOptions(link, manager, digest string) (Source, error) {
	if manager == "sh" || manager == "bash" || manager == "pwsh" || manager == "powershell" {
		if err := ValidateScriptURL(link); err != nil {
			return Source{}, err
		}
		if !sha256Pattern.MatchString(digest) {
			return Source{}, fmt.Errorf("script sources require --install-sha256 (64 lowercase hex characters); review the script before trusting its digest")
		}
		return Source{Manager: manager, URL: link, SHA256: digest}, nil
	}
	if digest != "" {
		return Source{}, fmt.Errorf("SHA-256 is only valid for sh, bash, pwsh or powershell script sources")
	}
	source, err := parsePackage(link)
	if err != nil {
		return Source{}, err
	}
	if manager == "" || manager == source.Manager {
		return source, nil
	}
	if source.Manager == "npm" && (manager == "bun" || manager == "pnpm") || source.Manager == "uv" && manager == "pipx" {
		source.Manager = manager
		return source, nil
	}
	return Source{}, fmt.Errorf("manager %q does not match this package URL", manager)
}

func parsePackage(link string) (Source, error) {
	if len(link) > 2048 {
		return Source{}, fmt.Errorf("install link exceeds 2048 bytes")
	}
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return Source{}, fmt.Errorf("install link must be a plain HTTPS package URL without credentials, query or fragment")
	}
	p := strings.TrimSuffix(u.Path, "/")
	switch u.Host {
	case "www.npmjs.com", "npmjs.com":
		name := strings.TrimPrefix(p, "/package/")
		if strings.HasPrefix(p, "/package/") && npmName.MatchString(name) {
			return Source{Manager: "npm", Package: name}, nil
		}
	case "pypi.org":
		name := strings.TrimPrefix(p, "/project/")
		if strings.HasPrefix(p, "/project/") && pythonName.MatchString(name) {
			return Source{Manager: "uv", Package: name}, nil
		}
	case "github.com", "raw.githubusercontent.com":
		// A concrete Ruby formula/cask file identifies a tap and package. Never
		// infer a package from an arbitrary repository or execute the URL as Ruby.
		parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
		if u.Host == "github.com" {
			if len(parts) != 6 || parts[2] != "blob" {
				break
			}
			parts = append(parts[:2], parts[3:]...)
		}
		if len(parts) == 5 && githubPart.MatchString(parts[0]) && strings.HasPrefix(parts[1], "homebrew-") && githubPart.MatchString(strings.TrimPrefix(parts[1], "homebrew-")) && githubPart.MatchString(parts[2]) && strings.HasSuffix(parts[4], ".rb") {
			name := strings.TrimSuffix(parts[4], ".rb")
			manager := ""
			if parts[3] == "Formula" {
				manager = "brew"
			}
			if parts[3] == "Casks" {
				manager = "brew-cask"
			}
			if manager != "" && brewName.MatchString(name) {
				return Source{Manager: manager, Package: parts[0] + "/" + strings.TrimPrefix(parts[1], "homebrew-") + "/" + name}, nil
			}
		}
	case "crates.io":
		name := strings.TrimPrefix(p, "/crates/")
		if strings.HasPrefix(p, "/crates/") && pythonName.MatchString(name) {
			return Source{Manager: "cargo", Package: name}, nil
		}
	case "formulae.brew.sh":
		if strings.HasPrefix(p, "/cask/") {
			name := strings.TrimPrefix(p, "/cask/")
			if brewName.MatchString(name) {
				return Source{Manager: "brew-cask", Package: name}, nil
			}
		}
		name := strings.TrimPrefix(p, "/formula/")
		if strings.HasPrefix(p, "/formula/") && brewName.MatchString(name) {
			return Source{Manager: "brew", Package: name}, nil
		}
	case "pkg.go.dev":
		name := strings.TrimPrefix(p, "/")
		if goName.MatchString(name) && strings.Contains(strings.Split(name, "/")[0], ".") && !strings.Contains(name, "//") {
			for _, part := range strings.Split(name, "/") {
				if part == "." || part == ".." {
					return Source{}, fmt.Errorf("invalid Go package path")
				}
			}
			return Source{Manager: "go", Package: name}, nil
		}
	}
	return Source{}, fmt.Errorf("unsupported install link: use npm, PyPI, Homebrew, crates.io or pkg.go.dev; HTTPS scripts require an explicit sh/bash/pwsh/powershell manager and SHA-256")
}

// Command returns argv, without a shell. Package managers resolve latest
// according to their own policies. No inferred package name or arbitrary URL is executed.
func (s Source) Command(update bool) []string {
	switch s.Manager {
	case "sh", "bash", "pwsh", "powershell":
		return []string{"verified-script", s.Manager, s.URL, s.SHA256}
	case "bun":
		return []string{"bun", "add", "--global", "--", s.Package + "@latest"}
	case "pnpm":
		// Explicit npm alias avoids pnpm's special runtime/toolchain selectors for
		// package names such as node, yarn or rust.
		return []string{"pnpm", "add", "--global", "--", s.Package + "@npm:" + s.Package + "@latest"}
	case "cargo":
		return []string{"cargo", "install", "--locked", "--", s.Package}
	case "pipx":
		if update {
			return []string{"pipx", "upgrade", "--", s.Package}
		}
		return []string{"pipx", "install", "--", s.Package}
	case "brew-cask":
		if update {
			return []string{"brew", "upgrade", "--cask", "--", s.Package}
		}
		return []string{"brew", "install", "--cask", "--", s.Package}
	case "npm":
		return []string{"npm", "install", "--global", "--", s.Package + "@latest"}
	case "uv":
		return []string{"uv", "tool", "install", "--upgrade", "--", s.Package}
	case "go":
		return []string{"go", "install", s.Package + "@latest"}
	case "brew":
		if update {
			return []string{"brew", "upgrade", "--formula", "--", s.Package}
		}
		return []string{"brew", "install", "--formula", "--", s.Package}
	}
	return nil
}
