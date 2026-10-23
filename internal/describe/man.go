package describe

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxManBytes = 128 * 1024

var (
	fontEscape = regexp.MustCompile(`\\f(?:\[[^\]]{0,32}\]|\(.{2}|.)`)
	sizeEscape = regexp.MustCompile(`\\s(?:\[[^\]]{0,16}\]|[+-]?[0-9]{1,2})`)
)

func manRoots(explicit string) []string {
	defaults := []string{"/usr/local/share/man", "/usr/share/man", "/opt/homebrew/share/man", "/opt/local/share/man"}
	path := explicit
	if path == "" {
		path = os.Getenv("MANPATH")
	}
	if path == "" {
		return defaults
	}
	var roots []string
	for _, root := range filepath.SplitList(path) {
		if root == "" {
			roots = append(roots, defaults...)
		} else {
			roots = append(roots, filepath.Clean(root))
		}
	}
	return roots
}

func (r *Resolver) manDescription(name string, paths []string) (string, string) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return "", ""
	}
	roots := make([]string, 0, len(paths)*2+len(r.manRoots))
	if r.nearbyMan {
		for _, executable := range paths {
			prefix := filepath.Dir(filepath.Dir(executable))
			roots = append(roots, filepath.Join(prefix, "share", "man"), filepath.Join(prefix, "man"))
		}
	}
	roots = append(roots, r.manRoots...)
	seen := make(map[string]bool)
	for _, root := range roots {
		for _, section := range []string{"1", "8"} {
			dir := filepath.Join(root, "man"+section)
			if filepath.Base(root) == "man"+section {
				dir = root
			}
			for _, suffix := range []string{"", ".gz"} {
				path := filepath.Join(dir, name+"."+section+suffix)
				if seen[path] {
					continue
				}
				seen[path] = true
				description, exists := r.manPages[path]
				if !exists {
					description = readManDescription(path)
					r.manPages[path] = description
				}
				if description != "" {
					return description, "man:" + path
				}
			}
		}
	}
	return "", ""
}

func readManDescription(path string) string {
	data, err := readPrefix(path, maxManBytes)
	if err != nil {
		return ""
	}
	if strings.HasSuffix(path, ".gz") {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return ""
		}
		defer reader.Close()
		data, err = io.ReadAll(io.LimitReader(reader, maxManBytes))
		if err != nil {
			return ""
		}
	}
	return parseManName(string(data))
}

func parseManName(data string) string {
	var text []string
	inName, mdocDescription := false, false
	for _, raw := range strings.Split(data, "\n") {
		line := strings.TrimSpace(raw)
		macro, args := manMacro(line)
		if macro == "SH" || macro == "Sh" {
			if inName {
				break
			}
			inName = strings.EqualFold(strings.Trim(args, `"`), "NAME")
			continue
		}
		if !inName {
			continue
		}
		if macro == "Nd" {
			text = nil
			mdocDescription = true
			text = append(text, args)
			continue
		}
		if macro == "Nm" && !mdocDescription {
			text = append(text, args)
			continue
		}
		if macro != "" {
			switch macro {
			case `\"`, "br", "PP", "P", "Pp", "LP", "sp", "nf", "fi", "ad", "na", "hy", "nh":
				continue
			case "Xr":
				fields := strings.Fields(args)
				if len(fields) >= 2 {
					text = append(text, fields[0]+"("+fields[1]+")")
				}
				continue
			case "Fl":
				text = append(text, "-"+args)
				continue
			}
			line = args
		}
		if comment := strings.Index(line, `\"`); comment >= 0 {
			line = line[:comment]
		}
		text = append(text, line)
	}
	result := cleanRoff(strings.Join(text, " "))
	if mdocDescription {
		return oneLine(result)
	}
	for _, separator := range []string{" - ", " – ", " — "} {
		if _, description, ok := strings.Cut(result, separator); ok {
			return oneLine(description)
		}
	}
	return ""
}

func manMacro(line string) (string, string) {
	if len(line) == 0 || (line[0] != '.' && line[0] != '\'') {
		return "", line
	}
	parts := strings.Fields(line[1:])
	if len(parts) == 0 {
		return "", ""
	}
	return parts[0], strings.TrimSpace(strings.TrimPrefix(line[1:], parts[0]))
}

func cleanRoff(s string) string {
	s = fontEscape.ReplaceAllString(s, "")
	s = sizeEscape.ReplaceAllString(s, "")
	s = strings.NewReplacer(
		`\-`, "-", `\(en`, "–", `\[en]`, "–", `\(em`, "—", `\[em]`, "—",
		`\(aq`, "'", `\[aq]`, "'", `\(dq`, `"`, `\[dq]`, `"`,
		`\(lq`, `"`, `\(rq`, `"`, `\[lq]`, `"`, `\[rq]`, `"`,
		`\&`, "", `\/`, "", `\c`, "", `\~`, " ", `\ `, " ", `\e`, `\`,
	).Replace(s)
	return strings.Join(strings.Fields(s), " ")
}
