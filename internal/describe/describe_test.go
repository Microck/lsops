package describe

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func putFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0755); err != nil {
		t.Fatal(err)
	}
}

func putGzip(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := gzip.NewWriter(f)
	if _, err = w.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestNPMSymlinkedBinWinsOverMan(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "node_modules", "@vendor", "widgets")
	actual := filepath.Join(pkg, "bin", "cli.js")
	putFile(t, actual, "#!/usr/bin/env node\nthrow new Error('must not run')\n")
	putFile(t, filepath.Join(pkg, "package.json"), `{"name":"@vendor/widgets","description":"Manage\nwidgets","bin":{"widgets":"bin/cli.js"}}`)
	link := filepath.Join(root, "bin", "widget-alias")
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	man := filepath.Join(root, "manuals")
	putFile(t, filepath.Join(man, "man1", "widget-alias.1"), ".SH NAME\nwidget-alias \\- old description\n")
	info := New(Options{ManPath: man}).Resolve("widget-alias", link)
	if info.Description != "Manage widgets" || info.Source != "npm:@vendor/widgets" {
		t.Fatalf("got %#v", info)
	}
}

func TestNPMPackageMustMapActualBin(t *testing.T) {
	root := t.TempDir()
	tool := filepath.Join(root, "bin", "widget")
	putFile(t, tool, "#!/bin/sh\nexit 99\n")
	putFile(t, filepath.Join(root, "other.js"), "unused")
	putFile(t, filepath.Join(root, "package.json"), `{"name":"unrelated","description":"Not this tool","bin":"other.js"}`)
	man := filepath.Join(root, "manuals")
	putFile(t, filepath.Join(man, "man1", "widget.1"), ".SH NAME\nwidget \\- describe the actual tool\n")
	info := New(Options{ManPath: man}).Resolve("widget", tool)
	if info.Description != "describe the actual tool" || !strings.HasPrefix(info.Source, "man:") {
		t.Fatalf("got %#v", info)
	}
}

func TestExecutableSymlinkParentTraversalPreservesKernelPath(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "installed", "package")
	putFile(t, filepath.Join(pkg, "bin", "widget"), "#!/bin/sh\nexit 99\n")
	putFile(t, filepath.Join(pkg, "package.json"), `{"name":"actual-package","description":"The physical executable","bin":"bin/widget"}`)
	if err := os.MkdirAll(filepath.Join(pkg, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(pkg, "nested"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	// Lexically cleaning link/.. would select this different executable.
	putFile(t, filepath.Join(root, "bin", "widget"), "#!/bin/sh\nexit 99\n")
	putFile(t, filepath.Join(root, "package.json"), `{"name":"wrong-package","description":"Wrong executable","bin":"bin/widget"}`)
	path := root + "/link/../bin/widget"
	info := New(Options{ManPath: filepath.Join(root, "no-man")}).Resolve("widget", path)
	if info.Description != "The physical executable" || info.Source != "npm:actual-package" {
		t.Fatalf("symlink/.. changed executable identity: %#v", info)
	}
}

func TestMalformedAndOversizedNPMMetadataFallThrough(t *testing.T) {
	for name, metadata := range map[string]string{"malformed": "{oops", "oversized": `{"name":"huge","description":"` + strings.Repeat("x", maxPackageBytes) + `","bin":"bin/widget"}`} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			tool := filepath.Join(root, "bin", "widget")
			putFile(t, tool, "#!/bin/sh\nexit 99\n")
			putFile(t, filepath.Join(root, "package.json"), metadata)
			info := New(Options{ManPath: filepath.Join(root, "no-man")}).Resolve("widget", tool)
			if info.Source != "unknown" || info.Description != "" {
				t.Fatalf("got %#v", info)
			}
		})
	}
}

func TestPythonEntryPointsAndSnapshot(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, "lib", "python3.13", "site-packages")
	dist := filepath.Join(site, "widgets-1.0.dist-info")
	tool := filepath.Join(root, "bin", "widget")
	putFile(t, tool, "#!/venv/bin/python3\nfrom widgetpkg.cli import main\nmain()\n")
	putFile(t, filepath.Join(dist, "entry_points.txt"), "[console_scripts]\nwidget = widgetpkg.cli:main\nother = otherpkg:run\n[gui_scripts]\nignored = ignored:main\n")
	putFile(t, filepath.Join(dist, "METADATA"), "Metadata-Version: 2.1\nName: Widgets\nSummary: Work with widgets\n and related tools\n\nLong body that is not a summary.\n")
	r := New(Options{ManPath: filepath.Join(root, "no-man")})
	info := r.Resolve("widget", tool)
	if info.Description != "Work with widgets and related tools" || info.Source != "python:Widgets" {
		t.Fatalf("got %#v", info)
	}
	other := filepath.Join(root, "bin", "other")
	putFile(t, other, "#!/venv/bin/python3\nfrom differentpkg import run\n")
	if info := r.Resolve("other", other); info.Source != "unknown" {
		t.Fatalf("unrelated same-name script inherited metadata: %#v", info)
	}
	late := filepath.Join(root, "bin", "late")
	putFile(t, late, "#!/venv/bin/python3\nfrom latepkg import main\n")
	lateDist := filepath.Join(site, "late-1.0.dist-info")
	putFile(t, filepath.Join(lateDist, "entry_points.txt"), "[console_scripts]\nlate = latepkg:main\n")
	putFile(t, filepath.Join(lateDist, "METADATA"), "Name: Late\nSummary: A newly installed tool\n\n")
	if info := r.Resolve("late", late); info.Source != "unknown" {
		t.Fatalf("expected cached prefix snapshot, got %#v", info)
	}
	if info := New(Options{ManPath: filepath.Join(root, "no-man")}).Resolve("late", late); info.Source != "python:Late" {
		t.Fatalf("fresh snapshot did not find new package: %#v", info)
	}
}

func TestManNamesAreLiteralAndHelpIsQuoted(t *testing.T) {
	root := t.TempDir()
	name := "odd name+[x]"
	man := filepath.Join(root, "man1", name+".1")
	putFile(t, man, ".TH ODD 1\n.SH \"NAME\"\n\\fBodd name+[x]\\fR \\- inspect odd files\n.SH SYNOPSIS\nnot part of summary\n")
	info := New(Options{ManPath: root}).Resolve(name, "")
	if info.Description != "inspect odd files" || info.Source != "man:"+man || info.Help != "'odd name+[x]' --help" {
		t.Fatalf("got %#v", info)
	}
	putFile(t, filepath.Join(root, "escape.1"), ".SH NAME\nescape \\- outside man1\n")
	if info := New(Options{ManPath: root}).Resolve("../escape", ""); info.Source != "unknown" {
		t.Fatalf("traversal name reached a man page: %#v", info)
	}
}

func TestGzipMdocAndBoundedDecompression(t *testing.T) {
	root := t.TempDir()
	putGzip(t, filepath.Join(root, "man8", "admin-widget.8.gz"), ".Dd October 22, 2026\n.Dt ADMIN-WIDGET 8\n.Sh NAME\n.Nm admin-widget\n.Nd administer widgets and\n.Xr widget 1\n.Sh DESCRIPTION\n"+strings.Repeat("x", maxManBytes*4))
	info := New(Options{ManPath: root}).Resolve("admin-widget", "")
	if info.Description != "administer widgets and widget(1)" || !strings.HasSuffix(info.Source, ".8.gz") {
		t.Fatalf("got %#v", info)
	}
	putFile(t, filepath.Join(root, "man1", "broken.1.gz"), "not a gzip stream")
	if info := New(Options{ManPath: root}).Resolve("broken", ""); info.Source != "unknown" {
		t.Fatalf("corrupt gzip produced metadata: %#v", info)
	}
}

func TestResolverNeverExecutesTools(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "executed")
	tool := filepath.Join(root, "bin", "mystery")
	putFile(t, tool, "#!/bin/sh\nprintf ran > '"+marker+"'\n")
	info := New(Options{ManPath: filepath.Join(root, "no-man")}).Resolve("mystery", tool)
	if info.Description != "" || info.Source != "unknown" || info.Baseline {
		t.Fatalf("unknown tool was overclaimed: %#v", info)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("tool was executed or unexpected stat error: %v", err)
	}
}

func TestBaselineIsConservativeAndIndependentOfDescription(t *testing.T) {
	root := t.TempDir()
	putFile(t, filepath.Join(root, "man1", "ls.1"), ".SH NAME\nls \\- local ls manual\n")
	r := New(Options{ManPath: root})
	info := r.Resolve("ls", "")
	if !info.Baseline || info.Description != "local ls manual" || !strings.HasPrefix(info.Source, "man:") {
		t.Fatalf("got %#v", info)
	}
	if info := r.Resolve("parallel-cli", ""); info.Baseline || info.Source != "builtin" || info.Description != "Search the web, extract pages, and run research with Parallel" {
		t.Fatalf("got %#v", info)
	}
	if info := r.Resolve("unknown-system-tool", "/usr/bin/unknown-system-tool"); info.Baseline {
		t.Fatalf("path caused baseline classification: %#v", info)
	}
	if info := r.Resolve("go", ""); info.Help != "go help" {
		t.Fatalf("incorrect native help suggestion: %#v", info)
	}
}
