package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"

	"github.com/Microck/lsops/internal/manage"
)

// SetInstallURL sets portable installation metadata, not permission to run it.
func SetInstallURL(path, name, link string) error { return SetInstallSpec(path, name, link, "", "") }

func InstallSource(t Tool) (manage.Source, error) {
	return manage.ParseOptions(t.InstallURL, t.InstallManager, t.InstallSHA256)
}

// SetInstallSpec replaces the complete install identity (including a script
// digest or alternate manager) without changing local execution approvals.
func SetInstallSpec(path, name, link, manager, digest string) error {
	if link != "" || manager != "" || digest != "" {
		if _, err := manage.ParseOptions(link, manager, digest); err != nil {
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
	name = existingName(cfg.Tools, name, runtime.GOOS)
	t, ok := cfg.Tools[name]
	if !ok || t.Description == "" {
		return fmt.Errorf("register %q with lsops add first", name)
	}
	t.InstallURL, t.InstallManager, t.InstallSHA256 = link, manager, digest
	cfg.Tools[name] = t
	return writeAtomic(path, cfg)
}

// Decode parses portable catalogue input using the same strict validation as Load.
func Decode(data []byte) (Config, error) { return loadFrom(bytes.NewReader(data), "import") }

// ImportTools merges only portable records. It never copies executable paths,
// project trust, visibility overrides, or automatic execution permission.
func ImportTools(path string, incoming Config) error {
	data, err := json.Marshal(incoming)
	if err != nil {
		return err
	}
	incoming, err = Decode(data)
	if err != nil {
		return err
	}
	if len(incoming.Projects) != 0 {
		return fmt.Errorf("portable catalogue cannot carry project trust")
	}
	for _, t := range incoming.Tools {
		if t.Path != "" || t.Help != "" || t.Hide || t.Include {
			return fmt.Errorf("portable catalogue cannot carry local settings")
		}
	}
	if _, err := writableDestination(path); err != nil {
		return err
	}
	cfg, err := Load(path)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(incoming.Tools))
	for name := range incoming.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t := incoming.Tools[name]
		if t.Description == "" {
			return fmt.Errorf("imported tool %q needs a description", name)
		}
		name = existingName(cfg.Tools, name, runtime.GOOS)
		previous := cfg.Tools[name]
		previous.Description, previous.Why, previous.When = t.Description, t.Why, t.When
		previous.InstallURL, previous.InstallManager, previous.InstallSHA256 = t.InstallURL, t.InstallManager, t.InstallSHA256
		cfg.Tools[name] = previous
	}
	return writeAtomic(path, cfg)
}

// Save validates and atomically saves a base-format catalogue. Callers must not
// use this to import project trust or grant automatic execution implicitly.
func Save(path string, cfg Config) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	normalized, err := Decode(data)
	if err != nil {
		return err
	}
	return writeAtomic(path, normalized)
}
