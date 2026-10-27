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
type Source struct{ Manager, Package string }

var npmName = regexp.MustCompile(`^(?:@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`)
var pythonName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var brewName = regexp.MustCompile(`^[a-z0-9][a-z0-9+@._-]*$`)
var goName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~/-]*$`)

// Parse accepts only package pages with unambiguous registry identities.
// It does not fetch the link or infer an executable's identity from its name.
func Parse(link string) (Source, error) {
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
			return Source{"npm", name}, nil
		}
	case "pypi.org":
		name := strings.TrimPrefix(p, "/project/")
		if strings.HasPrefix(p, "/project/") && pythonName.MatchString(name) {
			return Source{"uv", name}, nil
		}
	case "formulae.brew.sh":
		name := strings.TrimPrefix(p, "/formula/")
		if strings.HasPrefix(p, "/formula/") && brewName.MatchString(name) {
			return Source{"brew", name}, nil
		}
	case "pkg.go.dev":
		name := strings.TrimPrefix(p, "/")
		if goName.MatchString(name) && strings.Contains(strings.Split(name, "/")[0], ".") && !strings.Contains(name, "//") {
			for _, part := range strings.Split(name, "/") {
				if part == "." || part == ".." {
					return Source{}, fmt.Errorf("invalid Go package path")
				}
			}
			return Source{"go", name}, nil
		}
	}
	return Source{}, fmt.Errorf("unsupported install link: use an npm package, PyPI project, Homebrew formula, or pkg.go.dev command page")
}

// Command returns argv, without a shell. Package managers resolve latest
// according to their own policies. No inferred package name or arbitrary URL is executed.
func (s Source) Command(update bool) []string {
	switch s.Manager {
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
