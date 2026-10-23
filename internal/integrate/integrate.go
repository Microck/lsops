// Package integrate installs and removes lsops-owned agent hooks without
// replacing unrelated user settings. It never runs a hook or an agent.
package integrate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Options describes the executable, registry, and installation destination.
// Explicit agent homes override Home only for a user-scoped installation.
type Options struct {
	Executable   string
	ConfigPath   string
	Home         string
	ProjectDir   string
	Scope        string
	Mode         string
	GOOS         string
	ClaudeHome   string
	CodexHome    string
	CopilotHome  string
	OpenCodeHome string
	OMPHome      string
	Limit        int
}

// Result reports configuration presence, not whether the agent has loaded or
// approved the hook. Paths includes any new backup alongside the settings file.
type Result struct {
	Target    string
	Paths     []string
	Changed   bool
	Installed bool
	Notes     []string
}

// Target describes a supported agent and its refresh behavior.
type Target struct {
	Name    string
	Label   string
	Refresh string
	Notes   []string
}

// Targets returns the supported integrations in a stable order.
func Targets() []Target {
	return []Target{
		{Name: "claude", Label: "Claude Code", Refresh: "Session start, resume, clear, compact, and fork", Notes: []string{"Existing hook enablement and permissions are preserved."}},
		{Name: "gemini", Label: "Gemini CLI", Refresh: "Session start and before each agent turn", Notes: []string{"BeforeAgent refreshes the context on the next turn after compaction; no post-compaction event is installed.", "Existing hooksConfig enablement and disabled-hook settings are preserved."}},
		{Name: "cursor", Label: "Cursor", Refresh: "Session start", Notes: []string{"Startup only; Cursor has no supported post-compaction refresh for this integration."}},
		{Name: "codex", Label: "Codex", Refresh: "Session start, resume, clear, compact, and fork", Notes: []string{"Review and trust a new or changed hook in /hooks; project hooks also require project trust. Installation does not approve a hook."}},
		{Name: "copilot", Label: "GitHub Copilot CLI", Refresh: "Session start", Notes: []string{"Startup only; no post-compaction context hook is installed."}},
		{Name: "omp", Label: "oh-my-pi (OMP)", Refresh: "At session start/resume, session switch/branch/tree navigation, and after compaction", Notes: []string{"Uses ExtensionAPI session lifecycle events, not a per-model context hook. Restart or reload OMP after installation; disabled extensions and project trust are preserved."}},
		{Name: "opencode", Label: "OpenCode", Refresh: "Before every agent model request", Notes: []string{"Requires OpenCode 1.18.29+ or V2."}},
	}
}

// Install adds or replaces only lsops-managed hooks. Existing settings are
// backed up byte-for-byte before an atomic replacement.
func Install(target string, options Options) (Result, error) {
	return change(target, options, true)
}

// Uninstall removes only lsops-managed hooks, retaining unrelated settings.
func Uninstall(target string, options Options) (Result, error) {
	return change(target, options, false)
}

// Status checks whether the managed integration is configured. It does not
// execute the agent, verify hook approval, or change any files.
func Status(target string, options Options) (Result, error) {
	definition, err := targetDefinition(target)
	if err != nil {
		return Result{}, err
	}
	options, err = normalizeOptions(options, false)
	if err != nil {
		return Result{}, err
	}
	path, err := destination(target, options)
	if err != nil {
		return Result{}, err
	}
	result := initialResult(definition, path)
	data, err := readManagedFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if target == "opencode" || target == "omp" {
		result.Installed = isManagedPluginFor(target, data)
		if !result.Installed {
			result.Notes = append(result.Notes, "A file exists at the plugin path, but it is not managed by lsops.")
		}
		return result, nil
	}
	document, err := parseSettings(data, path)
	if err != nil {
		return result, err
	}
	if err := validateHookDocument(target, document); err != nil {
		return result, fmt.Errorf("inspect %q: %w; existing file is unchanged", path, err)
	}
	result.Installed = hasManagedHooks(target, document)
	result.Notes = append(result.Notes, disabledNotes(target, document)...)
	return result, nil
}

// Preview returns a standalone integration snippet. It does not read, merge,
// or write any agent settings.
func Preview(target string, options Options) ([]byte, error) {
	if _, err := targetDefinition(target); err != nil {
		return nil, err
	}
	options, err := normalizeOptions(options, true)
	if err != nil {
		return nil, err
	}
	if target == "opencode" || target == "omp" {
		return pluginContent(target, options)
	}
	document := make(map[string]any)
	if _, err := mergeHooks(target, document, options, true); err != nil {
		return nil, err
	}
	return encodeSettings(document)
}

func change(target string, options Options, install bool) (Result, error) {
	definition, err := targetDefinition(target)
	if err != nil {
		return Result{}, err
	}
	options, err = normalizeOptions(options, install)
	if err != nil {
		return Result{}, err
	}
	path, err := destination(target, options)
	if err != nil {
		return Result{}, err
	}
	result := initialResult(definition, path)
	original, err := readManagedFile(path)
	existed := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	if !install && !existed {
		return result, nil
	}
	var updated []byte
	if target == "opencode" || target == "omp" {
		if existed && !isManagedPluginFor(target, original) {
			return result, fmt.Errorf("refusing to replace or remove %q: it is not an lsops-managed plugin; existing file is unchanged", path)
		}
		if install {
			updated, err = pluginContent(target, options)
			if err != nil {
				return result, err
			}
			result.Installed = true
			if bytes.Equal(original, updated) {
				return result, nil
			}
		}
	} else {
		document := make(map[string]any)
		if existed {
			document, err = parseSettings(original, path)
			if err != nil {
				return result, err
			}
		}
		changed, mergeErr := mergeHooks(target, document, options, install)
		if mergeErr != nil {
			return result, fmt.Errorf("update %q: %w; existing file is unchanged", path, mergeErr)
		}
		result.Installed = hasManagedHooks(target, document)
		result.Notes = append(result.Notes, disabledNotes(target, document)...)
		if !changed {
			return result, nil
		}
		updated, err = encodeSettings(document)
		if err != nil {
			return result, err
		}
	}
	backup, err := replaceManagedFile(path, original, updated, existed, (target == "opencode" || target == "omp") && !install)
	if err != nil {
		return result, err
	}
	result.Changed = true
	if backup != "" {
		result.Paths = append(result.Paths, backup)
		result.Notes = append(result.Notes, "The previous file was backed up to "+backup+".")
	}
	return result, nil
}

func targetDefinition(name string) (Target, error) {
	for _, target := range Targets() {
		if name == target.Name {
			return target, nil
		}
	}
	return Target{}, fmt.Errorf("unsupported agent %q; choose claude, gemini, cursor, codex, copilot, omp, or opencode", name)
}

func initialResult(target Target, path string) Result {
	return Result{Target: target.Name, Paths: []string{path}, Notes: append([]string(nil), target.Notes...)}
}

func isManagedPlugin(data []byte) bool {
	first, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimSuffix(first, "\r") == openCodeMarker
}

func pluginContent(target string, options Options) ([]byte, error) {
	if target == "omp" {
		return ompContent(options)
	}
	return openCodeContent(options)
}
func isManagedPluginFor(target string, data []byte) bool {
	if target != "omp" {
		return isManagedPlugin(data)
	}
	first, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimSuffix(first, "\r") == ompMarker
}
