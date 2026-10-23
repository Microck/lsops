package integrate

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Microck/lsops/internal/config"
)

const managedFlag = "--managed-lsops-hook"

func normalizeOptions(options Options, needsCommand bool) (Options, error) {
	if options.Scope == "" {
		options.Scope = "user"
	}
	if options.Scope != "user" && options.Scope != "project" {
		return options, errors.New("scope must be user or project")
	}
	if options.Mode == "" {
		options.Mode = "instructions"
	}
	if options.Mode != "instructions" && options.Mode != "output" {
		return options, errors.New("mode must be instructions or output")
	}
	if options.Limit < 0 {
		return options, errors.New("limit must be zero (unlimited) or a positive number")
	}
	if options.GOOS == "" {
		options.GOOS = runtime.GOOS
	}
	if options.Home == "" {
		var err error
		options.Home, err = os.UserHomeDir()
		if err != nil {
			return options, fmt.Errorf("find home directory: %w", err)
		}
	}
	var err error
	options.Home, err = absoluteLocalPath(options.Home, "home directory")
	if err != nil {
		return options, err
	}
	if options.Scope == "project" {
		if options.ProjectDir == "" {
			options.ProjectDir, err = os.Getwd()
			if err != nil {
				return options, fmt.Errorf("find project directory: %w", err)
			}
		}
		options.ProjectDir, err = absoluteLocalPath(options.ProjectDir, "project directory")
		if err != nil {
			return options, err
		}
	}
	for _, field := range []struct {
		name  string
		value *string
	}{
		{"Claude home", &options.ClaudeHome}, {"Codex home", &options.CodexHome},
		{"Copilot home", &options.CopilotHome}, {"OpenCode home", &options.OpenCodeHome},
	} {
		if *field.value != "" {
			*field.value, err = absoluteLocalPath(*field.value, field.name)
			if err != nil {
				return options, err
			}
		}
	}
	if !needsCommand {
		return options, nil
	}
	if options.Executable == "" {
		options.Executable, err = os.Executable()
		if err != nil {
			return options, fmt.Errorf("find lsops executable: %w; provide its absolute path", err)
		}
	}
	options.Executable, err = absoluteCommandPath(options.Executable, "lsops executable", options.GOOS)
	if err != nil {
		return options, err
	}
	if options.ConfigPath == "" {
		options.ConfigPath, err = config.DefaultPath()
		if err != nil {
			return options, err
		}
	}
	options.ConfigPath, err = absoluteCommandPath(options.ConfigPath, "registry path", options.GOOS)
	return options, err
}

func absoluteLocalPath(path, label string) (string, error) {
	if err := safePath(path, label); err != nil {
		return "", err
	}
	if filepath.IsAbs(path) {
		return path, nil
	}
	current, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	// Do not clean a/../b: a may be a symlink, making the filesystem's path
	// resolution differ from lexical path cleaning.
	return joinPath(current, path), nil
}

func joinPath(base string, parts ...string) string {
	suffix := strings.Join(parts, string(os.PathSeparator))
	if len(base) > 0 && os.IsPathSeparator(base[len(base)-1]) {
		return base + suffix
	}
	return base + string(os.PathSeparator) + suffix
}

func absoluteCommandPath(path, label, goos string) (string, error) {
	if err := safePath(path, label); err != nil {
		return "", err
	}
	// Accept literal Windows absolute paths in previews generated on Unix.
	// Actual destination files always use the host's filesystem semantics.
	if goos == "windows" && windowsAbsolute(path) {
		return path, nil
	}
	return absoluteLocalPath(path, label)
}

func safePath(path, label string) error {
	if path == "" || !utf8.ValidString(path) || utf8.RuneCountInString(path) > 32767 {
		return fmt.Errorf("%s must be a nonempty valid UTF-8 path of at most 32767 characters", label)
	}
	for _, r := range path {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return fmt.Errorf("%s must not contain newlines or control characters", label)
		}
	}
	return nil
}

func windowsAbsolute(path string) bool {
	if len(path) >= 3 && ((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z')) && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		return true
	}
	if !strings.HasPrefix(path, `\\`) {
		return false
	}
	rest := strings.ReplaceAll(path[2:], `\`, "/")
	server, share, found := strings.Cut(rest, "/")
	share, _, _ = strings.Cut(share, "/")
	return found && server != "" && share != ""
}

func destination(target string, options Options) (string, error) {
	if target == "opencode" {
		return openCodePath(options)
	}
	if options.Scope == "project" {
		switch target {
		case "claude", "gemini":
			return joinPath(options.ProjectDir, "."+target, "settings.json"), nil
		case "cursor", "codex":
			return joinPath(options.ProjectDir, "."+target, "hooks.json"), nil
		case "copilot":
			return joinPath(options.ProjectDir, ".github", "hooks", "lsops.json"), nil
		}
	}
	var home, file string
	switch target {
	case "claude":
		home, file = options.ClaudeHome, "settings.json"
	case "gemini":
		file = "settings.json"
	case "cursor":
		file = "hooks.json"
	case "codex":
		home, file = options.CodexHome, "hooks.json"
	case "copilot":
		home, file = options.CopilotHome, filepath.Join("hooks", "lsops.json")
	default:
		return "", fmt.Errorf("unsupported agent %q", target)
	}
	if home == "" {
		home = joinPath(options.Home, "."+target)
	}
	return joinPath(home, file), nil
}

func hookArgs(target, event string, options Options) []string {
	return []string{"--config", options.ConfigPath, "hook", "render", "--harness", target, "--event", event, "--mode", options.Mode, "--limit", strconv.Itoa(options.Limit), managedFlag}
}

func quotedCommand(executable string, args []string, powershell bool) string {
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	prefix := ""
	if powershell {
		prefix = "& "
		quote = func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	}
	parts := []string{quote(executable)}
	for _, arg := range args {
		parts = append(parts, quote(arg))
	}
	return prefix + strings.Join(parts, " ")
}

func windowsEncodedCommand(executable string, args []string) string {
	script := quotedCommand(executable, args, true) + "; exit $LASTEXITCODE"
	units := utf16.Encode([]rune(script))
	data := make([]byte, len(units)*2)
	for i, value := range units {
		binary.LittleEndian.PutUint16(data[i*2:], value)
	}
	return "powershell.exe -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(data)
}

// commandWords recognizes the literal shell syntax generated above. It is not
// a general shell evaluator: expansions, pipelines, and command substitutions
// are deliberately not interpreted while identifying ownership.
func commandWords(command string) ([]string, bool) {
	const prefix = "powershell.exe -NoProfile -NonInteractive -EncodedCommand "
	if strings.HasPrefix(command, prefix) {
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(command, prefix))
		if err != nil || len(data)%2 != 0 {
			return nil, false
		}
		units := make([]uint16, len(data)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(data[i*2:])
		}
		command = string(utf16.Decode(units))
		const suffix = "; exit $LASTEXITCODE"
		if !strings.HasSuffix(command, suffix) {
			return nil, false
		}
		command = strings.TrimSuffix(command, suffix)
	}
	powershell := strings.HasPrefix(command, "& ")
	if powershell {
		command = strings.TrimPrefix(command, "& ")
	}
	var words []string
	for position := 0; position < len(command); {
		for position < len(command) && (command[position] == ' ' || command[position] == '\t') {
			position++
		}
		if position == len(command) {
			break
		}
		var word strings.Builder
		started := false
		for position < len(command) && command[position] != ' ' && command[position] != '\t' {
			quote := command[position]
			if quote != '\'' && (quote != '"' || powershell) {
				return nil, false // All generated words are quoted literals.
			}
			started = true
			position++
			closed := false
			for position < len(command) {
				value := command[position]
				position++
				if value == quote {
					if powershell && position < len(command) && command[position] == quote {
						word.WriteByte(quote)
						position++
						continue
					}
					closed = true
					break
				}
				// POSIX double quotes only occur around a literal apostrophe
				// in our escaping syntax. Reject all expandable alternatives.
				if quote == '"' && value != '\'' {
					return nil, false
				}
				word.WriteByte(value)
			}
			if !closed {
				return nil, false
			}
		}
		if !started {
			return nil, false
		}
		words = append(words, word.String())
	}
	return words, len(words) > 0
}
