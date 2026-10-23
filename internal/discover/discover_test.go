package discover

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func makeFile(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("this is deliberately not an executable program\n"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func makeDir(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func unixOnly(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permission/symlink semantics")
	}
}

func assertScan(t *testing.T, opts Options, want []Command) {
	t.Helper()
	got, warnings := Scan(opts)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Scan() = %#v; want %#v", got, want)
	}
}

func TestUnixFiltersAndShadowing(t *testing.T) {
	unixOnly(t)
	root := t.TempDir()
	first := makeDir(t, root, "first bin")
	second := makeDir(t, root, "second bin")
	alpha := makeFile(t, first, "alpha", 0755)
	shadow := makeFile(t, second, "alpha", 0755)
	zed := makeFile(t, first, "zed", 0111)
	text := makeFile(t, first, "text", 0644)
	directory := makeDir(t, first, "directory")
	for name, target := range map[string]string{
		"alias":    shadow,
		"broken":   filepath.Join(root, "missing"),
		"textlink": text,
		"dirlink":  directory,
	} {
		if err := os.Symlink(target, filepath.Join(first, name)); err != nil {
			t.Fatal(err)
		}
	}
	assertScan(t, Options{Path: first + ":" + second, GOOS: "linux"}, []Command{
		{Name: "alias", Path: filepath.Join(first, "alias")},
		{Name: "alpha", Path: alpha, Shadowed: []string{shadow}},
		{Name: "zed", Path: zed},
	})
}

func TestUnixCaseSensitiveNames(t *testing.T) {
	unixOnly(t)
	dir := t.TempDir()
	upper := makeFile(t, dir, "Tool", 0755)
	lower := makeFile(t, dir, "tool", 0755)
	upperInfo, _ := os.Stat(upper)
	lowerInfo, _ := os.Stat(lower)
	if os.SameFile(upperInfo, lowerInfo) {
		t.Skip("case-insensitive filesystem")
	}
	assertScan(t, Options{Path: dir, GOOS: "linux"}, []Command{
		{Name: "Tool", Path: upper},
		{Name: "tool", Path: lower},
	})
}

func TestEmptyAndRelativePathComponents(t *testing.T) {
	unixOnly(t)
	cwd := t.TempDir()
	rootTool := makeFile(t, cwd, "root-tool", 0755)
	bin := makeDir(t, cwd, "relative bin")
	binTool := makeFile(t, bin, "bin-tool", 0755)
	assertScan(t, Options{Path: "", CWD: cwd, GOOS: "linux"}, []Command{})
	assertScan(t, Options{Path: ":", CWD: cwd, GOOS: "linux"}, []Command{
		{Name: "root-tool", Path: rootTool},
	})
	assertScan(t, Options{Path: "relative bin::.:relative bin:", CWD: cwd, GOOS: "linux"}, []Command{
		{Name: "bin-tool", Path: binTool},
		{Name: "root-tool", Path: rootTool},
	})
}

func TestPhysicalDirectoryDuplicatesKeepFirstSpelling(t *testing.T) {
	unixOnly(t)
	root := t.TempDir()
	dir := makeDir(t, root, "real")
	makeFile(t, dir, "tool", 0755)
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	assertScan(t, Options{Path: alias + ":" + dir + ":" + alias + "/.", GOOS: "linux"}, []Command{
		{Name: "tool", Path: filepath.Join(alias, "tool")},
	})
}

func TestSymlinkDotDotIsNotLexicallyCleaned(t *testing.T) {
	unixOnly(t)
	root := t.TempDir()
	cwd := makeDir(t, root, "cwd")
	elsewhere := makeDir(t, root, "elsewhere")
	nested := makeDir(t, elsewhere, "nested")
	makeFile(t, elsewhere, "tool", 0755)
	if err := os.Symlink(nested, filepath.Join(cwd, "link")); err != nil {
		t.Fatal(err)
	}
	assertScan(t, Options{Path: "link/..", CWD: cwd, GOOS: "linux"}, []Command{
		{Name: "tool", Path: cwd + "/link/../tool"},
	})
}

func TestMissingDirectoriesAreSilent(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does not exist")
	assertScan(t, Options{Path: missing}, []Command{})
	file := makeFile(t, dir, "not-a-directory", 0644)
	got, warnings := Scan(Options{Path: file})
	if len(got) != 0 || len(warnings) != 1 || !strings.Contains(warnings[0], "not a directory") {
		t.Fatalf("got commands=%v warnings=%v; want one non-directory warning", got, warnings)
	}
}

func TestUnreadableDirectoryWarning(t *testing.T) {
	unixOnly(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("current user can read mode-0000 directories")
	}
	got, warnings := Scan(Options{Path: dir, GOOS: "linux"})
	if len(got) != 0 || len(warnings) != 1 {
		t.Fatalf("got commands=%v warnings=%v; want an unreadable-directory warning", got, warnings)
	}
}

func TestEffectiveUnixExecutePermission(t *testing.T) {
	unixOnly(t)
	if os.Geteuid() == 0 {
		t.Skip("root may execute any regular file with an execute bit")
	}
	dir := t.TempDir()
	// A group execute bit does not grant execution to the owner if the owner
	// execute bit is absent. A test using mode&0111 alone would include this.
	makeFile(t, dir, "group-only", 0010)
	assertScan(t, Options{Path: dir, GOOS: "linux"}, []Command{})
}

func TestWindowsPathExtAndPathPrecedence(t *testing.T) {
	root := t.TempDir()
	first := makeDir(t, root, "first bin")
	second := makeDir(t, root, "second bin")
	fooEXE := makeFile(t, first, "foo.EXE", 0644)
	fooCMD := makeFile(t, first, "FOO.cmd", 0644)
	fooSecond := makeFile(t, second, "Foo.exe", 0644)
	barCMD := makeFile(t, first, "bar.cmd", 0644)
	barEXE := makeFile(t, second, "bar.exe", 0644)
	makeFile(t, first, "ignore.txt", 0755)
	makeFile(t, first, "noextension", 0755)
	makeDir(t, first, "directory.exe")
	assertScan(t, Options{Path: first + ";" + second, GOOS: "windows", PathExt: ".EXE;.CMD;.exe"}, []Command{
		{Name: "bar", Path: barCMD, Shadowed: []string{barEXE}},
		{Name: "foo", Path: fooEXE, Shadowed: []string{fooCMD, fooSecond}},
	})
	assertScan(t, Options{Path: first, GOOS: "windows", PathExt: ".cmd;.exe"}, []Command{
		{Name: "bar", Path: barCMD},
		{Name: "FOO", Path: fooCMD, Shadowed: []string{fooEXE}},
	})
}

func TestWindowsDefaultAndCustomExtensions(t *testing.T) {
	dir := t.TempDir()
	com := makeFile(t, dir, "tool.com", 0644)
	exe := makeFile(t, dir, "tool.exe", 0644)
	bat := makeFile(t, dir, "tool.bat", 0644)
	cmd := makeFile(t, dir, "tool.cmd", 0644)
	py := makeFile(t, dir, "script.py", 0644)
	assertScan(t, Options{Path: dir, GOOS: "windows"}, []Command{
		{Name: "tool", Path: com, Shadowed: []string{exe, bat, cmd}},
	})
	assertScan(t, Options{Path: dir, GOOS: "windows", PathExt: ".PY"}, []Command{
		{Name: "script", Path: py},
	})
}

func TestWindowsQuotedAndEmptyComponents(t *testing.T) {
	cwd := t.TempDir()
	rootTool := makeFile(t, cwd, "root.exe", 0644)
	bin := makeDir(t, cwd, "bin;with spaces")
	binTool := makeFile(t, bin, "bin.exe", 0644)
	assertScan(t, Options{Path: `"bin;with spaces";;.`, CWD: cwd, GOOS: "windows"}, []Command{
		{Name: "bin", Path: binTool},
		{Name: "root", Path: rootTool},
	})
}

func TestWindowsDottedStemsRespectExactFiles(t *testing.T) {
	for _, test := range []struct {
		name       string
		exactFirst bool
		sameDir    bool
	}{
		{name: "same directory", exactFirst: true, sameDir: true},
		{name: "exact file earlier in PATH", exactFirst: true},
		{name: "suffixed file earlier in PATH"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			first := makeDir(t, root, "first")
			second := makeDir(t, root, "second")
			exactDir, suffixDir := second, first
			if test.exactFirst {
				exactDir, suffixDir = first, second
			}
			if test.sameDir {
				suffixDir = exactDir
			}
			exact := makeFile(t, exactDir, "tool.exe", 0644)
			suffix := makeFile(t, suffixDir, "tool.exe.cmd", 0644)
			aliasWinner, aliasShadow := suffix, exact
			if test.exactFirst {
				aliasWinner, aliasShadow = exact, suffix
			}
			assertScan(t, Options{Path: first + ";" + second, GOOS: "windows"}, []Command{
				{Name: "tool", Path: exact},
				{Name: "tool.exe", Path: aliasWinner, Shadowed: []string{aliasShadow}},
			})
		})
	}
}

func TestWindowsNonExecutableExactFileBlocksDottedStem(t *testing.T) {
	dir := t.TempDir()
	makeFile(t, dir, "tool.data", 0644)
	makeFile(t, dir, "tool.data.cmd", 0644)
	assertScan(t, Options{Path: dir, GOOS: "windows"}, []Command{})
}
