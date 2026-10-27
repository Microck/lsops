package manage

import (
	"reflect"
	"testing"
)

func TestPackageLinks(t *testing.T) {
	tests := []struct {
		link            string
		install, update []string
	}{
		{"https://www.npmjs.com/package/@acme/cli", []string{"npm", "install", "--global", "--", "@acme/cli@latest"}, nil},
		{"https://pypi.org/project/ruff/", []string{"uv", "tool", "install", "--upgrade", "--", "ruff"}, nil},
		{"https://pkg.go.dev/example.com/tool/cmd/tool", []string{"go", "install", "example.com/tool/cmd/tool@latest"}, nil},
		{"https://formulae.brew.sh/formula/gh", []string{"brew", "install", "--formula", "--", "gh"}, []string{"brew", "upgrade", "--formula", "--", "gh"}},
	}
	for _, tt := range tests {
		t.Run(tt.link, func(t *testing.T) {
			s, err := Parse(tt.link)
			if err != nil {
				t.Fatal(err)
			}
			if got := s.Command(false); !reflect.DeepEqual(got, tt.install) {
				t.Fatal(got)
			}
			want := tt.update
			if want == nil {
				want = tt.install
			}
			if got := s.Command(true); !reflect.DeepEqual(got, want) {
				t.Fatal(got)
			}
		})
	}
}
func TestRejectUntrustedOrAmbiguousLinks(t *testing.T) {
	for _, link := range []string{
		"", "http://www.npmjs.com/package/a", "https://evil.test/install.sh", "https://www.npmjs.com.evil.test/package/a",
		"https://user:pass@www.npmjs.com/package/a", "https://www.npmjs.com:443/package/a", "https://www.npmjs.com/package/a?x=1",
		"https://www.npmjs.com/package/a#x", "https://www.npmjs.com/package/a%3bwhoami", "https://www.npmjs.com/package/--help",
		"https://www.npmjs.com/package/foo@1", "https://www.npmjs.com/package/a;id", "https://www.npmjs.com/package/a\nb",
		"https://pypi.org/project/--index-url", "https://formulae.brew.sh/formula/a/b", "https://pkg.go.dev/example.com/../cmd",
		"https://pkg.go.dev/example.com/a@latest", "https://pkg.go.dev/example.com/a//b", "https://pkg.go.dev/example.com/a%2fb",
	} {
		if _, err := Parse(link); err == nil {
			t.Errorf("accepted %q", link)
		}
	}
}

func TestAdditionalPackageManagers(t *testing.T) {
	cases := []struct {
		link, manager   string
		install, update []string
	}{
		{"https://www.npmjs.com/package/@acme/cli", "bun", []string{"bun", "add", "--global", "--", "@acme/cli@latest"}, nil},
		{"https://npmjs.com/package/example", "pnpm", []string{"pnpm", "add", "--global", "--", "example@npm:example@latest"}, nil},
		{"https://pypi.org/project/ruff", "pipx", []string{"pipx", "install", "--", "ruff"}, []string{"pipx", "upgrade", "--", "ruff"}},
		{"https://crates.io/crates/ripgrep", "", []string{"cargo", "install", "--locked", "--", "ripgrep"}, nil},
		{"https://formulae.brew.sh/cask/wezterm", "", []string{"brew", "install", "--cask", "--", "wezterm"}, []string{"brew", "upgrade", "--cask", "--", "wezterm"}},
	}
	for _, c := range cases {
		s, err := ParseOptions(c.link, c.manager, "")
		if err != nil {
			t.Fatal(err)
		}
		if got := s.Command(false); !reflect.DeepEqual(got, c.install) {
			t.Fatal(got, c.install)
		}
		want := c.update
		if want == nil {
			want = c.install
		}
		if got := s.Command(true); !reflect.DeepEqual(got, want) {
			t.Fatal(got, want)
		}
	}
	for _, c := range []struct{ link, manager, digest string }{
		{"https://npmjs.com/package/a", "sh", ""},
		{"https://npmjs.com/package/a", "uv", ""},
		{"https://pypi.org/project/a", "bun", ""},
		{"https://evil.test/install.sh", "", ""},
		{"https://evil.test/install.sh", "bash", "bad"},
		{"https://npmjs.com/package/a", "npm", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	} {
		if _, err := ParseOptions(c.link, c.manager, c.digest); err == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
}

func TestHomebrewTapFileLinks(t *testing.T) {
	for _, link := range []string{"https://github.com/Microck/homebrew-kagi/blob/main/Formula/kagi.rb", "https://raw.githubusercontent.com/Microck/homebrew-kagi/main/Formula/kagi.rb"} {
		s, err := Parse(link)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.Command(false), []string{"brew", "install", "--formula", "--", "Microck/kagi/kagi"}) {
			t.Fatal(s)
		}
	}
	s, err := Parse("https://github.com/acme/homebrew-tools/blob/main/Casks/example.rb")
	if err != nil || s.Manager != "brew-cask" || s.Package != "acme/tools/example" {
		t.Fatal(s, err)
	}
	for _, link := range []string{"https://github.com/acme/other-repo/blob/main/Formula/tool.rb", "https://github.com/acme/homebrew-tools", "https://github.com/acme/homebrew-tools/blob/main/Formula/tool.sh", "https://github.com/acme/homebrew-tools/blob/main/Formula/--bad.rb"} {
		if _, err := Parse(link); err == nil {
			t.Fatal(link)
		}
	}
}
