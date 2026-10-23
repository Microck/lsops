package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Microck/lsops/internal/config"
	"github.com/Microck/lsops/internal/describe"
	"github.com/Microck/lsops/internal/discover"
)

type tool struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Path         string   `json:"path"`
	Source       string   `json:"description_source"`
	HelpHint     string   `json:"help_hint"`
	HelpShell    string   `json:"help_shell"`
	Baseline     bool     `json:"system_command"`
	Registered   bool     `json:"registered"`
	Available    bool     `json:"available"`
	ExplicitPath bool     `json:"-"`
	Shadowed     []string `json:"shadowed_paths,omitempty"`
}

type summary struct {
	Candidates         int `json:"candidates"`
	Configured         int `json:"configured"`
	MissingDescription int `json:"missing_description"`
	Unavailable        int `json:"unavailable"`
	Discovered         int `json:"discovered"`
	HiddenSystem       int `json:"hidden_system"`
	HiddenConfigured   int `json:"hidden_configured"`
	HiddenUnknown      int `json:"hidden_unknown"`
	FilteredOut        int `json:"filtered_out"`
	Shown              int `json:"shown"`
	OmittedByLimit     int `json:"omitted_by_limit"`
}

type inventory struct {
	SchemaVersion int      `json:"schema_version"`
	Version       string   `json:"lsops_version"`
	Mode          string   `json:"mode"`
	Tools         []tool   `json:"tools"`
	Summary       summary  `json:"summary"`
	Warnings      []string `json:"warnings"`
}

func list(filters []string, opts options, stdout, stderr io.Writer, version string) int {
	cfg, err := config.Load(opts.configPath)
	if err != nil {
		return fail(stderr, err.Error(), 1)
	}
	result := inventory{SchemaVersion: 2, Version: version, Mode: "curated", Tools: []tool{}, Warnings: []string{}}
	configNames := make([]string, 0, len(cfg.Tools))
	for name := range cfg.Tools {
		configNames = append(configNames, name)
	}
	sort.Strings(configNames)
	result.Summary.Configured = len(configNames)
	if opts.discover {
		result.Mode = "discover"
		cwd, err := os.Getwd()
		if err != nil {
			return fail(stderr, "cannot read working directory: "+err.Error(), 1)
		}
		commands, warnings := discover.Scan(discover.Options{
			Path: os.Getenv("PATH"), PathExt: os.Getenv("PATHEXT"), GOOS: runtime.GOOS, CWD: cwd,
		})
		result.Summary.Discovered = len(commands)
		result.Summary.Candidates = len(commands)
		for _, warning := range warnings {
			result.Warnings = append(result.Warnings, cleanText(warning, 500))
		}
		resolver := describe.New(describe.Options{ManPath: os.Getenv("MANPATH")})
		for _, cmd := range commands {
			override := lookupOverride(cfg.Tools, configNames, cmd.Name, runtime.GOOS)
			if override.Hide {
				result.Summary.HiddenConfigured++
				continue
			}
			lookupName := cmd.Name
			if runtime.GOOS == "windows" {
				lookupName = strings.ToLower(lookupName)
			}
			info := resolver.Resolve(lookupName, cmd.Path)
			if override.Description != "" {
				info.Description, info.Source = override.Description, "config"
			}
			info.Description = cleanText(info.Description, 240)
			if info.Source == "" {
				info.Source = "unknown"
			}
			if len(filters) > 0 && !matches(cmd.Name, info.Description, filters) {
				result.Summary.FilteredOut++
				continue
			}
			if info.Baseline && !opts.all && len(filters) == 0 && !override.Include {
				result.Summary.HiddenSystem++
				continue
			}
			if opts.known && info.Description == "" {
				result.Summary.HiddenUnknown++
				continue
			}
			helpHint, helpShell := nativeHelp(lookupName, cmd.Path, info.Help, runtime.GOOS)
			if override.Help != "" {
				helpHint, helpShell = override.Help, "user-defined"
			}
			result.Tools = append(result.Tools, tool{
				Name: cmd.Name, Description: info.Description, Path: cmd.Path,
				Source: cleanText(info.Source, 1000), HelpHint: helpHint, HelpShell: helpShell,
				Baseline: info.Baseline, Shadowed: cmd.Shadowed,
				Available: true, Registered: override.Description != "",
			})
		}
	} else {
		// A curated list is authoritative. Do not enumerate PATH or guess a
		// description; only resolve each registered executable for availability.
		result.Summary.Candidates = len(configNames)
		for _, name := range configNames {
			entry := cfg.Tools[name]
			if entry.Hide {
				result.Summary.HiddenConfigured++
				continue
			}
			if entry.Description == "" {
				result.Summary.MissingDescription++
				continue
			}
			if len(filters) > 0 && !matches(name, entry.Description, filters) {
				result.Summary.FilteredOut++
				continue
			}
			path, available := registeredPath(name, entry.Path)
			invocation := path
			if invocation == "" {
				invocation = name
			}
			helpHint, helpShell := nativeHelp(name, invocation, "", runtime.GOOS)
			if entry.Help != "" {
				helpHint, helpShell = entry.Help, "user-defined"
			}
			result.Tools = append(result.Tools, tool{
				Name: name, Description: cleanText(entry.Description, 240),
				Path: path, Source: "config", HelpHint: helpHint, HelpShell: helpShell,
				Registered: true, Available: available, ExplicitPath: entry.Path != "",
			})
		}
	}
	if opts.limit > 0 && len(result.Tools) > opts.limit {
		// Context hooks should not lose useful described tools behind a large
		// alphabetic prefix of unknown OS helpers. Selection stays deterministic;
		// every omission is explicitly reported and the full list stays available.
		if opts.context {
			sort.SliceStable(result.Tools, func(i, j int) bool {
				return result.Tools[i].Description != "" && result.Tools[j].Description == ""
			})
		}
		result.Summary.OmittedByLimit = len(result.Tools) - opts.limit
		result.Tools = result.Tools[:opts.limit]
		if opts.context {
			sort.Slice(result.Tools, func(i, j int) bool { return result.Tools[i].Name < result.Tools[j].Name })
		}
	}
	result.Summary.Shown = len(result.Tools)
	for _, item := range result.Tools {
		if !item.Available {
			result.Summary.Unavailable++
		}
	}
	if opts.json {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return fail(stderr, "write output: "+err.Error(), 1)
		}
		return 0
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(stderr, "lsops: warning: %s\n", warning)
	}
	if opts.context {
		if opts.discover {
			fmt.Fprintln(stdout, "CLI commands discovered on PATH. Use native help for unfamiliar usage.")
		} else {
			fmt.Fprintln(stdout, "User-configured CLI tools. Use native help for unfamiliar usage.")
		}
		fmt.Fprintln(stdout, "Refresh with lsops; optional PATH discovery is available through lsops discover.")
		fmt.Fprintln(stdout)
	}
	width := 0
	for _, item := range result.Tools {
		if n := utf8.RuneCountInString(displayName(item.Name)); n > width && n < 40 {
			width = n
		}
	}
	for _, item := range result.Tools {
		name := displayName(item.Name)
		if opts.names {
			if _, err := fmt.Fprintln(stdout, name); err != nil {
				return fail(stderr, "write output: "+err.Error(), 1)
			}
			continue
		}
		description := item.Description
		if description == "" {
			description = "description unavailable"
		}
		line := fmt.Sprintf("%-*s — %s", width, name, description)
		if (opts.paths || item.ExplicitPath) && item.Path != "" {
			line += " [" + displayName(item.Path) + "]"
		}
		if !item.Available {
			line += " [unavailable in this environment]"
		}
		if _, err := fmt.Fprintln(stdout, line); err != nil {
			return fail(stderr, "write output: "+err.Error(), 1)
		}
	}
	if !opts.names {
		if len(result.Tools) == 0 {
			if !opts.discover && len(filters) == 0 && result.Summary.Configured == 0 {
				fmt.Fprintln(stdout, `No CLIs configured. Add one with: lsops add NAME "description"`)
			} else {
				fmt.Fprintln(stdout, "No matching CLI commands found.")
			}
		}
		var notes []string
		if n := result.Summary.MissingDescription; n > 0 {
			notes = append(notes, fmt.Sprintf("%d legacy config entries without descriptions omitted; use lsops add NAME \"description\"", n))
		}
		if n := result.Summary.HiddenSystem; n > 0 {
			notes = append(notes, fmt.Sprintf("%d common system commands hidden; use --all", n))
		}
		if n := result.Summary.HiddenConfigured; n > 0 {
			notes = append(notes, fmt.Sprintf("%d hidden by config", n))
		}
		if n := result.Summary.HiddenUnknown; n > 0 {
			notes = append(notes, fmt.Sprintf("%d without descriptions omitted by --known", n))
		}
		if n := result.Summary.OmittedByLimit; n > 0 {
			notes = append(notes, fmt.Sprintf("%d additional matches omitted; use --limit 0 or a filter", n))
		}
		if len(notes) > 0 {
			fmt.Fprintf(stdout, "\n(%s.)\n", strings.Join(notes, "; "))
		}
	}
	return 0
}

// registeredPath looks up one user-selected executable, without scanning the
// environment or running the command. A missing executable stays in the list.
func registeredPath(name, configuredPath string) (string, bool) {
	candidate := name
	if configuredPath != "" {
		if !filepath.IsAbs(configuredPath) {
			return configuredPath, false
		}
		candidate = configuredPath
	}
	path, err := exec.LookPath(candidate)
	if err != nil && !errors.Is(err, exec.ErrDot) {
		return configuredPath, false
	}
	if !filepath.IsAbs(path) {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			return configuredPath, false
		}
		// Preserve symlink/.. traversal in a PATH component.
		path = cwd + string(os.PathSeparator) + path
	}
	return path, true
}

func matches(name, description string, filters []string) bool {
	haystack := strings.ToLower(name + " " + description)
	for _, filter := range filters {
		if strings.Contains(haystack, strings.ToLower(filter)) {
			return true
		}
	}
	return false
}

func lookupOverride(tools map[string]config.Tool, sortedNames []string, name, goos string) config.Tool {
	if exact, ok := tools[name]; ok {
		return exact
	}
	if goos == "windows" {
		for _, candidate := range sortedNames {
			if strings.EqualFold(candidate, name) {
				return tools[candidate]
			}
		}
	}
	return config.Tool{}
}

func nativeHelp(name, path, suggestion, goos string) (string, string) {
	suffix := " --help"
	if strings.HasPrefix(suggestion, name+" ") {
		suffix = strings.TrimPrefix(suggestion, name)
	}
	if goos == "windows" {
		return "& '" + strings.ReplaceAll(path, "'", "''") + "'" + suffix, "powershell"
	}
	return shellQuote(path) + suffix, "posix"
}

func cleanText(s string, maxRunes int) string {
	var out strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			out.WriteByte(' ')
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			// Strip terminal escapes and invisible directional formatting.
		default:
			out.WriteRune(r)
		}
	}
	value := strings.Join(strings.Fields(out.String()), " ")
	runes := []rune(value)
	if len(runes) > maxRunes {
		value = string(runes[:maxRunes-1]) + "…"
	}
	return value
}

func displayName(s string) string {
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError {
			return strconv.QuoteToASCII(s)
		}
	}
	return s
}

func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_./:-", r))
	}) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
