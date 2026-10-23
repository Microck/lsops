package integrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

func parseSettings(data []byte, path string) (map[string]any, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("settings %q are not valid UTF-8; existing file is unchanged", path)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := decodeValue(decoder, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot read settings %q as strict JSON: %w; JSONC comments and trailing commas are not supported; existing file is unchanged", path, err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("settings %q must contain exactly one JSON object; existing file is unchanged", path)
	}
	document, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("settings %q must be a JSON object; existing file is unchanged", path)
	}
	return document, nil
}

// The standard decoder permits duplicate object keys. Rejecting them avoids
// silently discarding an unrelated setting while rewriting a hook file.
func decodeValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > 128 {
		return nil, errors.New("JSON nesting exceeds 128 levels")
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, errors.New("expected a JSON object key")
			}
			if _, exists := object[key]; exists {
				return nil, fmt.Errorf("duplicate JSON object key %q", key)
			}
			value, err := decodeValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
			return nil, errors.New("unterminated JSON object")
		}
		return object, nil
	case '[':
		array := make([]any, 0)
		for decoder.More() {
			value, err := decodeValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if closing, err := decoder.Token(); err != nil || closing != json.Delim(']') {
			return nil, errors.New("unterminated JSON array")
		}
		return array, nil
	default:
		return nil, errors.New("unexpected JSON delimiter")
	}
}

func encodeSettings(document map[string]any) ([]byte, error) {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode agent settings: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxManagedFileSize {
		return nil, errors.New("updated agent settings would exceed the 8 MiB limit")
	}
	return data, nil
}

func nestedHooks(target string) bool {
	return target == "claude" || target == "gemini" || target == "codex"
}

func hookMetadata(target, key string) bool {
	return target == "gemini" && (key == "enabled" || key == "disabled" || key == "notifications")
}

func validateHookDocument(target string, document map[string]any) error {
	if target == "cursor" || target == "copilot" {
		if value, exists := document["version"]; exists {
			number, ok := value.(json.Number)
			if !ok || number.String() != "1" {
				return errors.New("unsupported hooks file version; expected version 1")
			}
		}
	}
	value, exists := document["hooks"]
	if !exists {
		return nil
	}
	hooks, ok := value.(map[string]any)
	if !ok {
		return errors.New("hooks must be a JSON object")
	}
	for event, value := range hooks {
		if hookMetadata(target, event) {
			continue
		}
		entries, ok := value.([]any)
		if !ok {
			return fmt.Errorf("hooks.%s must be an array", event)
		}
		for index, value := range entries {
			entry, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("hooks.%s[%d] must be an object", event, index)
			}
			if !nestedHooks(target) {
				continue
			}
			children, ok := entry["hooks"].([]any)
			if !ok {
				return fmt.Errorf("hooks.%s[%d].hooks must be an array", event, index)
			}
			for childIndex, child := range children {
				if _, ok := child.(map[string]any); !ok {
					return fmt.Errorf("hooks.%s[%d].hooks[%d] must be an object", event, index, childIndex)
				}
			}
		}
	}
	return nil
}

func mergeHooks(target string, document map[string]any, options Options, install bool) (bool, error) {
	if err := validateHookDocument(target, document); err != nil {
		return false, err
	}
	before, err := encodeSettings(document)
	if err != nil {
		return false, err
	}
	hooks, exists := document["hooks"].(map[string]any)
	if !exists {
		if !install {
			return false, nil
		}
		hooks = make(map[string]any)
		document["hooks"] = hooks
	}
	for event, value := range hooks {
		if hookMetadata(target, event) {
			continue
		}
		entries := value.([]any)
		remaining := make([]any, 0, len(entries))
		removed := false
		for _, value := range entries {
			entry := value.(map[string]any)
			if !nestedHooks(target) {
				if managedHook(target, entry) {
					removed = true
					continue
				}
				remaining = append(remaining, entry)
				continue
			}
			children := entry["hooks"].([]any)
			kept := make([]any, 0, len(children))
			removedChild := false
			for _, child := range children {
				if managedHook(target, child.(map[string]any)) {
					removed, removedChild = true, true
				} else {
					kept = append(kept, child)
				}
			}
			if removedChild {
				if len(kept) == 0 {
					continue
				}
				entry["hooks"] = kept
			}
			remaining = append(remaining, entry)
		}
		if removed {
			if len(remaining) == 0 {
				delete(hooks, event)
			} else {
				hooks[event] = remaining
			}
		}
	}
	if install {
		for event, entries := range desiredHooks(target, options) {
			previous, _ := hooks[event].([]any)
			hooks[event] = append(previous, entries...)
		}
		if target == "cursor" || target == "copilot" {
			document["version"] = json.Number("1")
		}
	}
	after, err := encodeSettings(document)
	if err != nil {
		return false, err
	}
	return !bytes.Equal(before, after), nil
}

func desiredHooks(target string, options Options) map[string][]any {
	events := []string{"SessionStart"}
	if target == "gemini" {
		events = append(events, "BeforeAgent")
	} else if target == "cursor" || target == "copilot" {
		events = []string{"sessionStart"}
	}
	result := make(map[string][]any)
	for _, event := range events {
		args := hookArgs(target, event, options)
		var entry map[string]any
		switch target {
		case "claude":
			entry = map[string]any{
				"matcher": "startup|resume|clear|compact|fork",
				"hooks":   []any{map[string]any{"type": "command", "command": options.Executable, "args": args, "timeout": 10}},
			}
		case "gemini":
			command := quotedCommand(options.Executable, args, false)
			// Gemini expands environment placeholders in settings before the
			// shell reads quotes. Splitting after a literal dollar sign avoids
			// that expansion while POSIX concatenation restores the argument.
			command = strings.ReplaceAll(command, "$", "$''")
			if options.GOOS == "windows" {
				command = windowsEncodedCommand(options.Executable, args)
			}
			entry = map[string]any{"hooks": []any{map[string]any{
				"name": "lsops", "type": "command", "command": command, "timeout": 10000,
			}}}
		case "cursor":
			entry = map[string]any{"command": quotedCommand(options.Executable, args, options.GOOS == "windows"), "timeout": 10}
		case "codex":
			entry = map[string]any{
				"matcher": "^(startup|resume|clear|compact|fork)$",
				"hooks": []any{map[string]any{
					"type": "command", "command": quotedCommand(options.Executable, args, false),
					"commandWindows": windowsEncodedCommand(options.Executable, args), "timeout": 10,
				}},
			}
		case "copilot":
			entry = map[string]any{"type": "command", "exec": options.Executable, "args": args, "timeoutSec": 10}
		}
		result[event] = []any{entry}
	}
	return result
}

func hasManagedHooks(target string, document map[string]any) bool {
	hooks, _ := document["hooks"].(map[string]any)
	for _, value := range hooks {
		entries, _ := value.([]any)
		for _, value := range entries {
			entry, _ := value.(map[string]any)
			if !nestedHooks(target) && managedHook(target, entry) {
				return true
			}
			if children, ok := entry["hooks"].([]any); nestedHooks(target) && ok {
				for _, child := range children {
					if object, ok := child.(map[string]any); ok && managedHook(target, object) {
						return true
					}
				}
			}
		}
	}
	return false
}

func managedHook(target string, hook map[string]any) bool {
	if kind, exists := hook["type"]; exists && kind != "command" {
		return false
	}
	switch args := hook["args"].(type) {
	case []string:
		return managedArgs(target, args)
	case []any:
		stringsOnly := make([]string, 0, len(args))
		for _, arg := range args {
			text, ok := arg.(string)
			if !ok {
				return false
			}
			stringsOnly = append(stringsOnly, text)
		}
		return managedArgs(target, stringsOnly)
	}
	command, _ := hook["command"].(string)
	words, ok := commandWords(command)
	return ok && len(words) > 1 && managedArgs(target, words[1:])
}

func managedArgs(target string, args []string) bool {
	if len(args) < 7 || args[0] != "--config" || args[1] == "" || args[2] != "hook" || args[3] != "render" || args[4] != "--harness" || args[5] != target {
		return false
	}
	for _, arg := range args[6:] {
		if arg == managedFlag {
			return true
		}
	}
	return false
}

func disabledNotes(target string, document map[string]any) []string {
	var notes []string
	if target == "claude" && document["disableAllHooks"] == true {
		notes = append(notes, "Claude's disableAllHooks setting is true; this hook will remain disabled until you change that setting.")
	}
	if target == "gemini" {
		settings, _ := document["hooksConfig"].(map[string]any)
		if settings["enabled"] == false {
			notes = append(notes, "Gemini's hooksConfig.enabled setting is false; hooks remain disabled.")
		}
		if disabled, ok := settings["disabled"].([]any); ok {
			for _, name := range disabled {
				if name == "lsops" || name == "*" {
					notes = append(notes, "Gemini's hooksConfig.disabled setting includes lsops or all hooks; the existing setting was preserved.")
					break
				}
			}
		}
	}
	return notes
}
