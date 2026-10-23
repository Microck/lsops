// Package describe resolves short descriptions from local, passive metadata.
// It never executes an executable or starts a subprocess.
package describe

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
)

type Info struct {
	Description string
	Source      string
	Baseline    bool
	Help        string
}

type Options struct {
	// ManPath is a colon-separated list of man roots. Empty uses MANPATH and
	// standard locations. An explicit value confines lookups to those roots.
	ManPath string
}

// Resolver is an in-memory snapshot: create a new resolver to refresh metadata.
// Its caches also record misses, so large PATH listings do not repeatedly scan
// the same package directories. Resolve is safe for concurrent calls.
type Resolver struct {
	mu            sync.Mutex
	manRoots      []string
	nearbyMan     bool
	results       map[string]Info
	npmPackages   map[string]*npmPackage
	pythonIndexes map[string]map[string][]pythonEntry
	manPages      map[string]string
}

func New(opts Options) *Resolver {
	return &Resolver{
		manRoots:      manRoots(opts.ManPath),
		nearbyMan:     opts.ManPath == "",
		results:       make(map[string]Info),
		npmPackages:   make(map[string]*npmPackage),
		pythonIndexes: make(map[string]map[string][]pythonEntry),
		manPages:      make(map[string]string),
	}
}

func (r *Resolver) Resolve(name, executablePath string) Info {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := name + "\x00" + executablePath
	if info, ok := r.results[key]; ok {
		return info
	}
	_, baseline := baselineDescriptions[name]
	info := Info{Source: "unknown", Baseline: baseline, Help: helpCommand(name)}
	paths := executablePaths(executablePath)
	if description, source := r.npmDescription(paths); description != "" {
		info.Description, info.Source = description, source
	} else if description, source := r.pythonDescription(name, paths); description != "" {
		info.Description, info.Source = description, source
	} else if description, source := r.manDescription(name, paths); description != "" {
		info.Description, info.Source = description, source
	} else if description := builtinDescriptions[name]; description != "" {
		info.Description, info.Source = description, "builtin"
	} else if description := baselineDescriptions[name]; description != "" {
		info.Description, info.Source = description, "builtin"
	}
	r.results[key] = info
	return info
}

func executablePaths(path string) []string {
	if path == "" {
		return nil
	}
	abs := path
	if !filepath.IsAbs(abs) {
		cwd, err := os.Getwd()
		if err != nil {
			return nil
		}
		// Join/Abs clean before resolving symlinks. For link/../tool that
		// changes the file the kernel would open, so preserve the components.
		abs = cwd + string(filepath.Separator) + path
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil
	}
	// Keep a final-symlink alias for its installation prefix, but resolve its
	// parent first so later Dir/Join operations cannot change its meaning.
	dir, base := filepath.Split(abs)
	parent, err := filepath.EvalSymlinks(dir)
	if err == nil {
		alias := filepath.Join(parent, base)
		if alias != resolved {
			return []string{resolved, alias}
		}
	}
	return []string{resolved}
}

// readPrefix only reads regular files and never reads more than limit bytes.
// Parsers either accept the useful prefix (man/metadata) or reject truncation
// (JSON). In particular, a metadata path naming a FIFO is never opened.
func readPrefix(path string, limit int64) ([]byte, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() {
		return nil, os.ErrInvalid
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit))
}

func readDirBounded(path string, limit int) []os.DirEntry {
	stat, err := os.Stat(path)
	if err != nil || !stat.IsDir() {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	entries, _ := f.ReadDir(limit)
	return entries
}

func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) > 360 {
		s = string(runes[:357]) + "..."
	}
	return s
}

func helpCommand(name string) string {
	if name == "" {
		return ""
	}
	switch name {
	case "go", "cargo", "rustup", "openssl":
		return name + " help"
	case "ffmpeg", "ffprobe", "sqlite3", "magick", "convert", "identify":
		return name + " -help"
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("_-.+", c)) {
			return "'" + strings.ReplaceAll(name, "'", "'\\''") + "' --help"
		}
	}
	return name + " --help"
}
