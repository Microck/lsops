package config

import (
	"strings"
	"testing"
)

func TestLoadRejectsDirectoriesBeforeReading(t *testing.T) {
	_, err := Load(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExistingNameRespectsPlatformAndExactMatches(t *testing.T) {
	tools := map[string]Tool{"Git": {}, "git": {}}
	for _, test := range []struct{ name, goos, want string }{
		{"git", "windows", "git"},
		{"GIT", "windows", "Git"},
		{"GIT", "linux", "GIT"},
		{"missing", "windows", "missing"},
	} {
		if got := existingName(tools, test.name, test.goos); got != test.want {
			t.Fatalf("%+v: got %q", test, got)
		}
	}
}
