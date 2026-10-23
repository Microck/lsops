// Package discover inventories external commands without running them.
package discover

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Command describes the first executable found for a name in PATH order.
type Command struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Shadowed []string `json:"shadowed_paths,omitempty"`
}

// Options supplies the environment to inspect. An empty Path inventories nothing.
// CWD defaults to the process working directory; GOOS defaults to runtime.GOOS.
// GOOS controls PATH/PATHEXT syntax, while filesystem paths always refer to the
// host filesystem. This also permits Windows name-resolution tests on Unix.
type Options struct {
	Path    string
	PathExt string
	GOOS    string
	CWD     string
}

type candidate struct {
	name string
	path string
	rank int
}

type windowsFile struct {
	path       string
	executable bool
}

type windowsDirectory struct {
	files   map[string]windowsFile
	aliases map[string][]candidate
}

// Scan returns commands sorted by name and nonfatal directory warnings. PATH
// order wins; on Windows, PATHEXT order breaks ties within each directory.
// Missing directories are ignored. Symlinks are followed only for inspection:
// the returned path retains the spelling used to invoke the executable.
//
// No command, including a command's --help or --version, is ever executed.
// Unix access checks use Go's absolute-path executable lookup. Its OS-specific
// implementation can fall back to permission bits when access checks are
// unavailable. Discovery cannot guarantee a future execution will succeed.
func Scan(opts Options) ([]Command, []string) {
	commands := make([]Command, 0)
	if opts.Path == "" {
		return commands, nil
	}
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	windows := goos == "windows"
	exts := pathExtensions(opts.PathExt)
	var warnings []string
	seenPaths := make(map[string]bool)
	var seenDirs []fs.FileInfo
	var windowsDirs []windowsDirectory
	byName := make(map[string]int)
	for _, entry := range splitPath(opts.Path, windows) {
		dir, err := absoluteDir(entry, opts.CWD)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("cannot resolve PATH directory %q: %v", entry, err))
			continue
		}
		pathKey := nameKey(dir, windows)
		if seenPaths[pathKey] {
			continue
		}
		seenPaths[pathKey] = true
		info, err := os.Stat(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				warnings = append(warnings, fmt.Sprintf("cannot inspect PATH directory %q: %v", dir, err))
			}
			continue
		}
		if !info.IsDir() {
			warnings = append(warnings, fmt.Sprintf("PATH entry %q is not a directory", dir))
			continue
		}
		duplicate := false
		for _, previous := range seenDirs {
			if os.SameFile(info, previous) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		seenDirs = append(seenDirs, info)
		entries, err := os.ReadDir(dir)
		if err != nil && !os.IsNotExist(err) {
			warnings = append(warnings, fmt.Sprintf("cannot read PATH directory %q: %v", dir, err))
		}
		var candidates []candidate
		winDir := windowsDirectory{files: make(map[string]windowsFile), aliases: make(map[string][]candidate)}
		for _, file := range entries {
			if file.IsDir() {
				continue
			}
			fullPath := appendPath(dir, file.Name())
			fileInfo, err := os.Stat(fullPath)
			if err != nil || !fileInfo.Mode().IsRegular() {
				continue
			}
			if windows {
				lower := strings.ToLower(file.Name())
				accepted := false
				for rank, ext := range exts {
					if len(lower) > len(ext) && strings.HasSuffix(lower, ext) {
						candidates = append(candidates, candidate{file.Name()[:len(file.Name())-len(ext)], fullPath, rank})
						accepted = true
					}
				}
				if _, exists := winDir.files[lower]; !exists {
					winDir.files[lower] = windowsFile{path: fullPath, executable: accepted}
				}
				continue
			}
			if fileInfo.Mode().Perm()&0111 == 0 {
				continue
			}
			if runtime.GOOS != "windows" {
				// Absolute paths bypass PATH and Go's relative-path ErrDot guard.
				// LookPath performs filesystem checks; it never starts the file.
				if _, err := exec.LookPath(fullPath); err != nil {
					continue
				}
			}
			candidates = append(candidates, candidate{file.Name(), fullPath, 0})
		}
		sort.Slice(candidates, func(i, j int) bool {
			a, b := candidates[i], candidates[j]
			if ak, bk := nameKey(a.name, windows), nameKey(b.name, windows); ak != bk {
				return ak < bk
			}
			if a.rank != b.rank {
				return a.rank < b.rank
			}
			return a.path < b.path
		})
		for _, item := range candidates {
			key := nameKey(item.name, windows)
			if windows {
				winDir.aliases[key] = append(winDir.aliases[key], item)
			}
			if index, exists := byName[key]; exists {
				commands[index].Shadowed = append(commands[index].Shadowed, item.path)
			} else {
				byName[key] = len(commands)
				commands = append(commands, Command{Name: item.name, Path: item.path})
			}
		}
		if windows {
			windowsDirs = append(windowsDirs, winDir)
		}
	}
	if windows {
		commands = resolveWindowsDottedNames(commands, windowsDirs)
	}
	sort.Slice(commands, func(i, j int) bool {
		return nameKey(commands[i].Name, windows) < nameKey(commands[j].Name, windows)
	})
	return commands, warnings
}

func resolveWindowsDottedNames(commands []Command, dirs []windowsDirectory) []Command {
	result := commands[:0]
	for _, cmd := range commands {
		if !strings.ContainsRune(cmd.Name, '.') {
			result = append(result, cmd)
			continue
		}
		// Windows tries an exact filename when the invocation contains a dot,
		// before appending PATHEXT in that directory. A stem such as tool.exe
		// must not incorrectly resolve to tool.exe.cmd if tool.exe exists.
		key := strings.ToLower(cmd.Name)
		paths := make([]string, 0)
		seen := make(map[string]bool)
		blocked := false
		appendUnique := func(path string) {
			pathKey := strings.ToLower(path)
			if !seen[pathKey] {
				seen[pathKey] = true
				paths = append(paths, path)
			}
		}
		for _, dir := range dirs {
			if exact, exists := dir.files[key]; exists {
				if !exact.executable && len(paths) == 0 {
					// An exact non-PATHEXT file blocks this stem; advertising the
					// later suffixed file under that name would be misleading.
					blocked = true
					break
				}
				if exact.executable {
					appendUnique(exact.path)
				}
			}
			for _, item := range dir.aliases[key] {
				appendUnique(item.path)
			}
		}
		if blocked || len(paths) == 0 {
			continue
		}
		cmd.Path, cmd.Shadowed = paths[0], nil
		if len(paths) > 1 {
			cmd.Shadowed = paths[1:]
		}
		result = append(result, cmd)
	}
	return result
}

func nameKey(name string, windows bool) string {
	if windows {
		return strings.ToLower(name)
	}
	return name
}

func splitPath(value string, windows bool) []string {
	if !windows {
		return strings.Split(value, ":")
	}
	// Quoted Windows entries can contain semicolons; quotes are not filename
	// characters. Keep explicit empty components, including trailing ones.
	var result []string
	var current strings.Builder
	quoted := false
	for _, c := range value {
		switch {
		case c == '"':
			quoted = !quoted
		case c == ';' && !quoted:
			result = append(result, current.String())
			current.Reset()
		default:
			current.WriteRune(c)
		}
	}
	return append(result, current.String())
}

func pathExtensions(value string) []string {
	if value == "" {
		value = ".COM;.EXE;.BAT;.CMD"
	}
	var result []string
	seen := make(map[string]bool)
	for _, ext := range strings.Split(value, ";") {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if len(ext) < 2 || ext[0] != '.' || strings.ContainsAny(ext, `/\\:`) || seen[ext] {
			continue
		}
		seen[ext] = true
		result = append(result, ext)
	}
	return result
}

func absoluteDir(dir, cwd string) (string, error) {
	if filepath.IsAbs(dir) {
		return dir, nil
	}
	if cwd == "" || !filepath.IsAbs(cwd) {
		actual, err := os.Getwd()
		if err != nil {
			return "", err
		}
		if cwd == "" {
			cwd = actual
		} else {
			cwd = appendPath(actual, cwd)
		}
	}
	if dir == "" || dir == "." {
		return cwd, nil
	}
	if runtime.GOOS == "windows" {
		volume := filepath.VolumeName(dir)
		if volume != "" {
			if strings.EqualFold(volume, filepath.VolumeName(cwd)) {
				return appendPath(cwd, dir[len(volume):]), nil
			}
			// A drive-relative path on another drive uses that drive's current
			// directory according to the host's native Windows path semantics.
			return filepath.Abs(dir)
		}
		if os.IsPathSeparator(dir[0]) {
			return filepath.VolumeName(cwd) + dir, nil
		}
	}
	return appendPath(cwd, dir), nil
}

func appendPath(dir, name string) string {
	// Unlike filepath.Join, do not lexically collapse "symlink/..": doing so
	// can silently inspect a different directory from the one named in PATH.
	if len(dir) > 0 && os.IsPathSeparator(dir[len(dir)-1]) {
		return dir + name
	}
	return dir + string(filepath.Separator) + name
}
