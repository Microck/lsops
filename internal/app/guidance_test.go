package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuidanceRoundTripAndRendering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	run := func(args ...string) string {
		t.Helper()
		code, out, err := invoke(append([]string{"--config", path}, args...)...)
		if code != 0 {
			t.Fatalf("%v: %d %s", args, code, err)
		}
		return out
	}
	run("add", "example", "Query records", "--why", "Avoid manual requests", "--when=Investigating incidents")
	for _, args := range [][]string{{}, {"--json"}, {"hook", "render", "--harness", "claude", "--event", "SessionStart", "--mode", "output"}} {
		out := run(args...)
		for _, want := range []string{"Query records", "Avoid manual requests", "Investigating incidents"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%v missing %q: %s", args, want, out)
			}
		}
	}
	if out := run("incidents"); !strings.Contains(out, "example") {
		t.Fatal(out)
	}
	if out := run("--names"); strings.Contains(out, "manual") {
		t.Fatal(out)
	}
	run("add", "example", "Updated description")
	if out := run(); !strings.Contains(out, "Avoid manual requests") {
		t.Fatal(out)
	}
	run("add", "example", "Updated description", "--why=", "--when=")
	if out := run("--json"); strings.Contains(out, "\"why\"") || strings.Contains(out, "\"when\"") {
		t.Fatal(out)
	}
	run("add", "example", "Literal guidance", "--why=--literal", "--when=--literal")
	if out := run("--json"); !strings.Contains(out, "--literal") {
		t.Fatal(out)
	}
	before, _ := os.ReadFile(path)
	for _, args := range [][]string{
		{"add", "example", "Rejected", "--why", strings.Repeat("x", 241)},
		{"add", "example", "Rejected", "--when", "bad\x1btext"},
		{"add", "example", "Rejected", "--when"},
		{"add", "example", "Rejected", "--why", "--when=Reviewing"},
		{"add", "example", "Rejected", "--when", "--path=/tmp/example"},
	} {
		code, _, _ := invoke(append([]string{"--config", path}, args...)...)
		if code == 0 {
			t.Fatalf("accepted %v", args)
		}
		after, _ := os.ReadFile(path)
		if string(before) != string(after) {
			t.Fatal("invalid update changed config")
		}
	}
}
