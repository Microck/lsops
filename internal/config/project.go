package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const ProjectFile = ".lsops.json"

// SetProject opts a physical directory and its descendants into a local overlay.
// It changes only the selected base catalogue; it does not create project files.
func SetProject(path, directory string, enable bool) error {
	if err := singleLine(directory); err != nil {
		return err
	}
	if strings.TrimSpace(directory) == "" {
		return errors.New("project directory must not be empty")
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	physical, err := filepath.EvalSymlinks(root)
	if err == nil {
		root = physical
	} else if enable || !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if enable {
		info, err := os.Stat(root)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("project root must be a directory")
		}
		if _, err := loadProject(root); err != nil {
			return err
		}
	}
	if _, err := writableDestination(path); err != nil {
		return err
	}
	cfg, err := Load(path)
	if err != nil {
		return err
	}
	projects := []string{}
	found := false
	for _, existing := range cfg.Projects {
		if samePath(existing, root) {
			found = true
			if !enable {
				continue
			}
		}
		projects = append(projects, existing)
	}
	if enable && !found {
		projects = append(projects, root)
	}
	if enable == found {
		return nil
	}
	sort.Strings(projects)
	cfg.Projects = projects
	return writeAtomic(path, cfg)
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func loadProject(root string) (Config, error) {
	path := filepath.Join(root, ProjectFile)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("create %s before enabling this project", path)
	}
	if err != nil {
		return Config{}, err
	}
	if !info.Mode().IsRegular() {
		return Config{}, fmt.Errorf("project catalogue %q must be a regular file, not a symlink", path)
	}
	cfg, err := Load(path)
	if err != nil {
		return Config{}, err
	}
	if len(cfg.Projects) > 0 {
		return Config{}, errors.New("project catalogues cannot register other projects")
	}
	return cfg, nil
}

// ForDirectory overlays only the nearest explicitly registered physical ancestor.
// Unregistered .lsops.json files are never read. A record replaces the whole
// global record of the same name; hide:true suppresses it. No state is written.
func ForDirectory(base Config, directory string) (Config, string, error) {
	cwd, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return Config{}, "", err
	}
	selected := ""
	for _, root := range base.Projects {
		// A moved root must be opted in again, rather than following a new symlink.
		physical, err := filepath.EvalSymlinks(root)
		if err != nil || !samePath(physical, root) {
			continue
		}
		rel, err := filepath.Rel(root, cwd)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		if len(root) > len(selected) {
			selected = root
		}
	}
	if selected == "" {
		return base, "", nil
	}
	overlay, err := loadProject(selected)
	if err != nil {
		return Config{}, "", err
	}
	merged := Config{Version: base.Version, Projects: append([]string(nil), base.Projects...), Tools: make(map[string]Tool)}
	for name, tool := range base.Tools {
		merged.Tools[name] = tool
	}
	names := make([]string, 0, len(overlay.Tools))
	for name := range overlay.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		key := existingName(merged.Tools, name, runtime.GOOS)
		merged.Tools[key] = overlay.Tools[name]
	}
	return merged, filepath.Join(selected, ProjectFile), nil
}
