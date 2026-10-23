// Package config reads and edits the user-curated CLI registry and legacy
// discovery overrides.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxFileSize          = 1 << 20
	maxDescriptionLength = 240
	maxHelpLength        = 500
	maxPathLength        = 4096
)

// Tool registers a CLI description and optional absolute executable path.
// Legacy records without descriptions remain valid. Help is display text,
// never a command for lsops to execute.
type Tool struct {
	Description string `json:"description,omitempty"`
	Why         string `json:"why,omitempty"`
	When        string `json:"when,omitempty"`
	Path        string `json:"path,omitempty"`
	Help        string `json:"help,omitempty"`
	Hide        bool   `json:"hide,omitempty"`
	Include     bool   `json:"include,omitempty"`
}

// Config is the versioned JSON override file.
type Config struct {
	Projects []string        `json:"projects,omitempty"`
	Version  int             `json:"version"`
	Tools    map[string]Tool `json:"tools"`
}

// DefaultPath returns the user configuration path. An explicit XDG_CONFIG_HOME
// takes precedence on every platform; otherwise Go's native config directory is
// used, including Library/Application Support on macOS.
func DefaultPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		var err error
		dir, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("find user config directory: %w", err)
		}
	}
	return filepath.Join(dir, "lsops", "config.json"), nil
}

func emptyConfig() Config {
	return Config{Version: 1, Tools: make(map[string]Tool)}
}

// Load reads one JSON object, rejecting unknown fields, invalid values, and
// files larger than 1 MiB. A missing file is an empty version-1 configuration.
func Load(path string) (Config, error) {
	if path == "" {
		return Config{}, errors.New("config path must not be empty")
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyConfig(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("inspect config %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return Config{}, fmt.Errorf("config %q is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config %q: %w", path, err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	if len(data) > maxFileSize {
		return Config{}, fmt.Errorf("config %q exceeds the 1 MiB limit", path)
	}
	if !utf8.Valid(data) {
		return Config{}, fmt.Errorf("config %q is not valid UTF-8", path)
	}

	// RawMessage distinguishes an absent version (allowed) from an explicit
	// zero or null (unsupported), without relaxing unknown-field checking.
	var disk *struct {
		Projects []string        `json:"projects,omitempty"`
		Version  json.RawMessage `json:"version"`
		Tools    map[string]Tool `json:"tools"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&disk); err != nil {
		return Config{}, fmt.Errorf("decode config %q: %w", path, err)
	}
	if disk == nil {
		return Config{}, fmt.Errorf("config %q must be a JSON object", path)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Config{}, fmt.Errorf("config %q must contain only one JSON document", path)
		}
		return Config{}, fmt.Errorf("trailing data in config %q: %w", path, err)
	}
	if len(disk.Version) != 0 {
		var version int
		if err := json.Unmarshal(disk.Version, &version); err != nil || version != 1 {
			return Config{}, fmt.Errorf("config %q has an unsupported version; expected 1", path)
		}
	}

	cfg := emptyConfig()
	for _, root := range disk.Projects {
		if err := singleLine(root); err != nil || !filepath.IsAbs(root) || filepath.Clean(root) != root {
			return Config{}, fmt.Errorf("project root %q must be a clean absolute path", root)
		}
		cfg.Projects = append(cfg.Projects, root)
	}
	// Stable ordering gives deterministic errors when several entries are bad.
	names := make([]string, 0, len(disk.Tools))
	for name := range disk.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := validateName(name); err != nil {
			return Config{}, fmt.Errorf("tool %q in config %q: %w", name, path, err)
		}
		tool := disk.Tools[name]
		if tool.Hide && tool.Include {
			return Config{}, fmt.Errorf("tool %q in config %q: hide and include cannot both be true", name, path)
		}
		tool.Description, err = normalizeText(tool.Description, "description", maxDescriptionLength)
		if err != nil {
			return Config{}, fmt.Errorf("tool %q in config %q: %w", name, path, err)
		}
		for label, field := range map[string]*string{"why": &tool.Why, "when": &tool.When} {
			*field, err = normalizeText(*field, label, maxDescriptionLength)
			if err != nil {
				return Config{}, fmt.Errorf("tool %q in config %q: %w", name, path, err)
			}
		}
		tool.Help, err = normalizeText(tool.Help, "help", maxHelpLength)
		if err != nil {
			return Config{}, fmt.Errorf("tool %q in config %q: %w", name, path, err)
		}
		if err := validateExecutablePath(tool.Path); err != nil {
			return Config{}, fmt.Errorf("tool %q in config %q: %w", name, path, err)
		}
		cfg.Tools[name] = tool
	}
	return cfg, nil
}

func validateName(name string) error {
	if name == "" || strings.TrimSpace(name) == "" {
		return errors.New("tool name must not be empty")
	}
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return errors.New("tool name must be an executable name, not a path")
	}
	if err := singleLine(name); err != nil {
		return fmt.Errorf("invalid tool name: %w", err)
	}
	return nil
}

func normalizeText(value, field string, limit int) (string, error) {
	if err := singleLine(value); err != nil {
		return "", fmt.Errorf("%s %w", field, err)
	}
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > limit {
		return "", fmt.Errorf("%s must not exceed %d characters", field, limit)
	}
	return value, nil
}

func singleLine(value string) error {
	if !utf8.ValidString(value) {
		return errors.New("must be valid UTF-8")
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return errors.New("must be a single line without control characters")
		}
	}
	return nil
}

func validateExecutablePath(path string) error {
	if path == "" {
		return nil
	}
	if err := singleLine(path); err != nil {
		return fmt.Errorf("path %w", err)
	}
	if utf8.RuneCountInString(path) > maxPathLength {
		return fmt.Errorf("path must not exceed %d characters", maxPathLength)
	}
	if !absoluteExecutablePath(path) {
		return errors.New("path must be an absolute executable path")
	}
	return nil
}

// Validate both POSIX and Windows spellings so loading a registry on another
// platform does not discard its entries. This checks syntax, not existence or
// availability. Preserve the literal path: no trimming or variable expansion.
func absoluteExecutablePath(path string) bool {
	if strings.HasPrefix(path, "/") {
		return true
	}
	if len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) &&
		path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		return true
	}
	if !strings.HasPrefix(path, `\\`) {
		return false
	}
	// UNC paths need a server and share. This also permits Windows extended
	// absolute paths such as \\?\C:\tools\cli.exe and \\?\UNC\server\share.
	rest := strings.ReplaceAll(path[2:], `\`, "/")
	server, share, found := strings.Cut(rest, "/")
	if !found || server == "" {
		return false
	}
	share, _, _ = strings.Cut(share, "/")
	return share != ""
}

// SetTool adds or updates a registration with a required description. An empty
// executablePath preserves the existing path; a nonempty one replaces it.
// Existing help/visibility settings and other records are retained. No PATH
// lookup, existence check, or executable invocation is performed.
func SetTool(path, name, description, executablePath string) error {
	return SetToolGuidance(path, name, description, executablePath, nil, nil)
}

// SetToolGuidance updates optional guidance atomically with the description.
// Nil preserves an existing field; a pointer to an empty string clears it.
func SetToolGuidance(path, name, description, executablePath string, why, when *string) error {
	if err := validateName(name); err != nil {
		return err
	}
	description, err := normalizeText(description, "description", maxDescriptionLength)
	if err != nil {
		return err
	}
	if description == "" {
		return errors.New("description must not be empty")
	}
	if err := validateExecutablePath(executablePath); err != nil {
		return err
	}
	if _, err := writableDestination(path); err != nil {
		return err
	}
	cfg, err := Load(path)
	if err != nil {
		return err
	}
	name = existingName(cfg.Tools, name, runtime.GOOS)
	tool, exists := cfg.Tools[name]
	previous := tool
	for label, update := range map[string]struct {
		value  *string
		target *string
	}{
		"why": {why, &tool.Why}, "when": {when, &tool.When},
	} {
		if update.value != nil {
			value, err := normalizeText(*update.value, label, maxDescriptionLength)
			if err != nil {
				return err
			}
			*update.target = value
		}
	}
	tool.Description = description
	if executablePath != "" {
		tool.Path = executablePath
	}
	if exists && tool == previous {
		return nil
	}
	cfg.Tools[name] = tool
	return writeAtomic(path, cfg)
}

// SetDescription adds or updates a required description while preserving an
// existing executable path and other settings. It is an alias for SetTool with
// an empty executablePath.
func SetDescription(path, name, description string) error {
	return SetTool(path, name, description, "")
}

// Forget removes the named registration or legacy override. It returns false
// when there was nothing to remove and does not create a missing config file
// or directory.
func Forget(path, name string) (bool, error) {
	if err := validateName(name); err != nil {
		return false, err
	}
	exists, err := writableDestination(path)
	if err != nil || !exists {
		return false, err
	}
	cfg, err := Load(path)
	if err != nil {
		return false, err
	}
	name = existingName(cfg.Tools, name, runtime.GOOS)
	if _, exists := cfg.Tools[name]; !exists {
		return false, nil
	}
	delete(cfg.Tools, name)
	if err := writeAtomic(path, cfg); err != nil {
		return false, err
	}
	return true, nil
}

// Windows command names are case-insensitive. Preserve the spelling of an
// existing override so a successful edit cannot be masked by an older key.
func existingName(tools map[string]Tool, name, goos string) string {
	if _, ok := tools[name]; ok || goos != "windows" {
		return name
	}
	var matches []string
	for key := range tools {
		if strings.EqualFold(key, name) {
			matches = append(matches, key)
		}
	}
	if len(matches) == 0 {
		return name
	}
	sort.Strings(matches)
	return matches[0]
}

// writableDestination rejects symlinks, including dangling ones. Reading a
// symlinked config is allowed, but an edit must never replace that symlink.
func writableDestination(path string) (bool, error) {
	if path == "" {
		return false, errors.New("config path must not be empty")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect config %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("refusing to edit symlinked config %q", path)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("config %q is not a regular file", path)
	}
	return true, nil
}

func writeAtomic(path string, cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxFileSize {
		return errors.New("updated config would exceed the 1 MiB limit")
	}
	if _, err := writableDestination(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config directory %q: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".lsops-config-*")
	if err != nil {
		return fmt.Errorf("create temporary config in %q: %w", dir, err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return fmt.Errorf("set config permissions: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	// Recheck immediately before replacement, after all fallible file writes.
	if _, err := writableDestination(path); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace config %q: %w", path, err)
	}
	return nil
}
