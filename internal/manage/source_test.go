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
