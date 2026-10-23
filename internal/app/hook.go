package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Microck/lsops/internal/integrate"
)

type hookOptions struct {
	mode, scope, dir, harness, event, configPath string
	limit                                        int
	help, scopeSet                               bool
}

// hookCommand manages integrations and renders their context. Rendering always
// reads the current registry; installing a hook never captures a catalogue.
func hookCommand(args []string, opts options, stdout, stderr io.Writer, version string) int {
	if len(args) == 0 {
		return fail(stderr, "usage: lsops hook install|uninstall|status|preview|render|targets [HARNESS] [options]", 2)
	}
	action := args[0]
	if action == "help" || action == "--help" || action == "-h" {
		return writeHookUsage(stdout, stderr)
	}
	// Keep the original preview command as a harmless compatibility alias.
	if action == "claude" {
		action = "preview"
		args = append([]string{"preview", "claude"}, args[1:]...)
	}
	switch action {
	case "install", "uninstall", "status", "preview", "render", "targets":
	default:
		return fail(stderr, "unknown hook command: "+action, 2)
	}
	hookOpts, pos, err := parseHookOptions(args[1:], opts)
	if err != nil {
		return fail(stderr, err.Error(), 2)
	}
	if hookOpts.help {
		return writeHookUsage(stdout, stderr)
	}
	if action == "targets" {
		if len(pos) != 0 {
			return fail(stderr, "usage: lsops hook targets", 2)
		}
		var out bytes.Buffer
		for _, target := range integrate.Targets() {
			fmt.Fprintf(&out, "%s — %s: %s\n", target.Name, target.Label, target.Refresh)
			for _, note := range target.Notes {
				fmt.Fprintf(&out, "  %s\n", note)
			}
		}
		return writeHookBytes(stdout, stderr, out.Bytes())
	}
	if action == "render" {
		if len(pos) != 0 {
			return fail(stderr, "hook render does not accept positional arguments", 2)
		}
		return renderHook(hookOpts, stdout, stderr, version)
	}
	if hookOpts.harness != "" || hookOpts.event != "" {
		return fail(stderr, "--harness and --event apply only to hook render", 2)
	}
	if action == "status" && len(pos) == 0 {
		pos = []string{"all"}
	}
	if len(pos) != 1 {
		return fail(stderr, "usage: lsops hook "+action+" HARNESS [--scope user|project]", 2)
	}
	if action == "preview" && pos[0] == "all" {
		return fail(stderr, "hook preview requires one harness; use hook targets to list them", 2)
	}
	targets, err := selectedHookTargets(pos[0])
	if err != nil {
		return fail(stderr, err.Error(), 2)
	}
	installOpts, err := integrationOptions(hookOpts, action == "install" || action == "preview")
	if err != nil {
		return fail(stderr, err.Error(), 1)
	}
	if action == "preview" {
		data, err := integrate.Preview(targets[0].Name, installOpts)
		if err != nil {
			return fail(stderr, err.Error(), 1)
		}
		return writeHookBytes(stdout, stderr, data)
	}
	code := 0
	for _, target := range targets {
		var result integrate.Result
		var err error
		switch action {
		case "install":
			result, err = integrate.Install(target.Name, installOpts)
		case "uninstall":
			result, err = integrate.Uninstall(target.Name, installOpts)
		case "status":
			result, err = integrate.Status(target.Name, installOpts)
		}
		if err != nil {
			fail(stderr, target.Name+": "+err.Error(), 1)
			code = 1
			continue
		}
		var out bytes.Buffer
		switch action {
		case "install":
			verb := "Configured"
			if !result.Changed {
				verb = "Already configured"
			}
			fmt.Fprintf(&out, "%s %s (%s mode, %s scope).\n", verb, target.Label, hookOpts.mode, hookOpts.scope)
		case "uninstall":
			if result.Changed {
				fmt.Fprintf(&out, "Removed the lsops integration for %s (%s scope).\n", target.Label, hookOpts.scope)
			} else {
				fmt.Fprintf(&out, "No lsops integration configured for %s (%s scope).\n", target.Label, hookOpts.scope)
			}
		case "status":
			state := "not configured"
			if result.Installed {
				state = "configured"
			}
			fmt.Fprintf(&out, "%s: %s (%s scope).\n", target.Name, state, hookOpts.scope)
		}
		for _, path := range result.Paths {
			fmt.Fprintf(&out, "  %s\n", displayName(path))
		}
		if action != "uninstall" {
			fmt.Fprintf(&out, "  Refresh: %s\n", target.Refresh)
		}
		for _, note := range result.Notes {
			fmt.Fprintf(&out, "  %s\n", note)
		}
		if writeHookBytes(stdout, stderr, out.Bytes()) != 0 {
			return 1
		}
	}
	return code
}

// A small option parser allows both "install claude --mode output" and
// "install --mode output claude" without changing positional arguments.
func parseHookOptions(args []string, opts options) (hookOptions, []string, error) {
	h := hookOptions{mode: "instructions", scope: "user", limit: opts.limit, configPath: opts.configPath}
	var pos []string
	literal := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if literal || !strings.HasPrefix(arg, "-") || arg == "-" {
			pos = append(pos, arg)
			continue
		}
		if arg == "--" {
			literal = true
			continue
		}
		if arg == "--help" || arg == "-h" {
			h.help = true
			continue
		}
		if arg == "--managed-lsops-hook" {
			continue // A stable marker used only to identify installed entries.
		}
		key, value, hasValue := strings.Cut(arg, "=")
		switch key {
		case "--mode", "--scope", "--dir", "--harness", "--event", "--config", "--limit":
		default:
			return h, nil, fmt.Errorf("unknown hook option: %s", key)
		}
		if !hasValue {
			i++
			if i >= len(args) {
				return h, nil, fmt.Errorf("%s requires a value", key)
			}
			value = args[i]
		}
		if value == "" {
			return h, nil, fmt.Errorf("%s must not be empty", key)
		}
		switch key {
		case "--mode":
			h.mode = value
		case "--scope":
			h.scope, h.scopeSet = value, true
		case "--dir":
			h.dir = value
		case "--harness":
			h.harness = value
		case "--event":
			h.event = value
		case "--config":
			h.configPath = value
		case "--limit":
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				return h, nil, errors.New("--limit must be zero or a positive integer")
			}
			h.limit = n
		}
	}
	if h.mode != "instructions" && h.mode != "output" {
		return h, nil, errors.New("--mode must be instructions or output")
	}
	if h.scope != "user" && h.scope != "project" {
		return h, nil, errors.New("--scope must be user or project")
	}
	if h.dir != "" {
		if h.scopeSet && h.scope != "project" {
			return h, nil, errors.New("--dir selects a project; use --scope project or omit --scope")
		}
		h.scope = "project"
	}
	if h.limit < 0 {
		return h, nil, errors.New("--limit must be zero or a positive integer")
	}
	return h, pos, nil
}

func selectedHookTargets(name string) ([]integrate.Target, error) {
	targets := integrate.Targets()
	if name == "all" {
		return targets, nil
	}
	for _, target := range targets {
		if target.Name == name {
			return []integrate.Target{target}, nil
		}
	}
	return nil, fmt.Errorf("unsupported harness %q; use lsops hook targets", name)
}

func integrationOptions(h hookOptions, needsExecutable bool) (integrate.Options, error) {
	home, homeErr := os.UserHomeDir()
	if homeErr != nil && h.scope == "user" {
		return integrate.Options{}, fmt.Errorf("find home directory: %w", homeErr)
	}
	project := h.dir
	if project == "" {
		var err error
		project, err = os.Getwd()
		if err != nil {
			return integrate.Options{}, fmt.Errorf("find project directory: %w", err)
		}
	}
	project, err := absoluteHookPath(project)
	if err != nil {
		return integrate.Options{}, err
	}
	cfg, err := absoluteHookPath(h.configPath)
	if err != nil {
		return integrate.Options{}, fmt.Errorf("locate lsops configuration: %w", err)
	}
	opts := integrate.Options{
		ConfigPath: cfg, Home: home, ProjectDir: project,
		Scope: h.scope, Mode: h.mode, GOOS: runtime.GOOS, Limit: h.limit,
		ClaudeHome: os.Getenv("CLAUDE_CONFIG_DIR"), CodexHome: os.Getenv("CODEX_HOME"),
		OMPHome:     os.Getenv("PI_CODING_AGENT_DIR"),
		CopilotHome: os.Getenv("COPILOT_HOME"), OpenCodeHome: os.Getenv("OPENCODE_CONFIG_DIR"),
	}
	if root := os.Getenv("PI_CONFIG_DIR"); opts.OMPHome == "" && root != "" {
		opts.OMPHome = root + string(os.PathSeparator) + "agent"
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); opts.OpenCodeHome == "" && xdg != "" {
		// Preserve filesystem traversal through any symlink/.. components.
		separator := string(os.PathSeparator)
		if os.IsPathSeparator(xdg[len(xdg)-1]) {
			separator = ""
		}
		opts.OpenCodeHome = xdg + separator + "opencode"
	}
	if needsExecutable {
		opts.Executable, err = hookExecutable()
		if err != nil {
			return integrate.Options{}, err
		}
	}
	return opts, nil
}

func hookExecutable() (string, error) {
	executable, err := os.Executable()
	// Restricted containers can omit /proc/self/exe. Looking up argv[0]
	// resolves a path without starting lsops or any discovered command.
	if err != nil && len(os.Args) > 0 {
		executable, err = exec.LookPath(os.Args[0])
	}
	if err != nil {
		return "", fmt.Errorf("locate installed lsops: %w", err)
	}
	return absoluteHookPath(executable)
}

func absoluteHookPath(path string) (string, error) {
	if path == "" {
		return "", errors.New("path must not be empty")
	}
	if filepath.IsAbs(path) {
		return path, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	// Do not clean away a symlink/.. component before the filesystem sees it.
	return cwd + string(os.PathSeparator) + path, nil
}

func renderHook(h hookOptions, stdout, stderr io.Writer, version string) int {
	event, err := hookRenderEvent(h.harness, h.event)
	if err != nil {
		return fail(stderr, err.Error(), 2)
	}
	var content string
	if h.mode == "output" {
		var out bytes.Buffer
		code := list(nil, options{configPath: h.configPath, limit: h.limit}, &out, stderr, version)
		if code != 0 {
			return code
		}
		content = out.String()
	} else {
		executable, err := hookExecutable()
		if err != nil {
			return fail(stderr, err.Error(), 1)
		}
		cfg, err := absoluteHookPath(h.configPath)
		if err != nil {
			return fail(stderr, err.Error(), 1)
		}
		content = hookInstructions(executable, cfg, runtime.GOOS, h.limit)
	}
	if h.harness == "" || h.harness == "opencode" || h.harness == "omp" {
		return writeHookBytes(stdout, stderr, []byte(content))
	}
	var response any
	switch h.harness {
	case "claude", "codex", "gemini":
		response = map[string]any{"hookSpecificOutput": map[string]string{
			"hookEventName": event, "additionalContext": content,
		}}
	case "cursor":
		response = map[string]string{"additional_context": content}
	case "copilot":
		response = map[string]string{"additionalContext": content}
	}
	if err := json.NewEncoder(stdout).Encode(response); err != nil {
		return fail(stderr, "write hook output: "+err.Error(), 1)
	}
	return 0
}

func hookRenderEvent(harness, event string) (string, error) {
	var allowed []string
	switch harness {
	case "":
		if event == "" {
			return "", nil
		}
		return "", errors.New("--event requires --harness")
	case "claude", "codex":
		allowed = []string{"SessionStart"}
	case "gemini":
		allowed = []string{"SessionStart", "BeforeAgent"}
	case "cursor", "copilot":
		allowed = []string{"sessionStart"}
	case "opencode", "omp":
		allowed = []string{"context"}
	default:
		return "", fmt.Errorf("unsupported harness %q; use lsops hook targets", harness)
	}
	if event == "" {
		return allowed[0], nil
	}
	for _, candidate := range allowed {
		if candidate == event {
			return event, nil
		}
	}
	return "", fmt.Errorf("unsupported %s hook event %q; supported: %s", harness, event, strings.Join(allowed, ", "))
}

func hookInstructions(executable, configPath, goos string, limit int) string {
	args := []string{executable, "--config", configPath}
	if limit > 0 {
		args = append(args, "--limit", strconv.Itoa(limit))
	}
	var quoted []string
	for _, arg := range args {
		if goos == "windows" {
			quoted = append(quoted, "'"+strings.ReplaceAll(arg, "'", "''")+"'")
		} else {
			quoted = append(quoted, shellQuote(arg))
		}
	}
	command := strings.Join(quoted, " ")
	if goos == "windows" {
		command = "& " + command
	}
	return "Before choosing command-line tools, run " + command + " to see the user's configured CLI catalogue.\n" +
		"Use a listed tool when it fits the task. Consult its native help (usually --help or help) for arguments and subcommands.\n"
}

func writeHookBytes(stdout, stderr io.Writer, data []byte) int {
	if _, err := stdout.Write(data); err != nil {
		return fail(stderr, "write hook output: "+err.Error(), 1)
	}
	return 0
}

func writeHookUsage(stdout, stderr io.Writer) int {
	return writeHookBytes(stdout, stderr, []byte(`Usage:
  lsops hook install HARNESS|all [--mode instructions|output] [--scope user|project]
  lsops hook uninstall HARNESS|all [--scope user|project]
  lsops hook status [HARNESS|all] [--scope user|project]
  lsops hook preview HARNESS [--mode instructions|output] [--scope user|project]
  lsops hook render [--mode instructions|output] [--harness HARNESS] [--event EVENT]
  lsops hook targets

Options:
  --mode MODE   instructions (default): tell the agent how to run lsops.
                output: insert the current curated list on each hook execution.
  --scope SCOPE user (default) or project.
  --dir DIR     Select a project directory; implies --scope project.
  --limit N     Maximum catalogue entries; 0 (default) is unlimited.
  --config FILE Select the lsops registry used by the installed hook.

Preview prints a configuration snippet without changing harness settings.
Status reports configuration presence, not whether the harness has approved it.
Use hook targets for each harness's refresh events and activation requirements.
`))
}
