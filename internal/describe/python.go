package describe

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxPythonMetadataBytes = 64 * 1024
	maxPythonSiteEntries   = 512
	maxPythonCommands      = 2048
)

var pythonDirName = regexp.MustCompile(`^python[0-9]+(?:\.[0-9]+)*t?$`)

type pythonEntry struct {
	distribution string
	description  string
	module       string
}

func (r *Resolver) pythonDescription(name string, paths []string) (string, string) {
	for _, executable := range paths {
		binDir := filepath.Dir(executable)
		if filepath.Base(binDir) != "bin" && filepath.Base(binDir) != "Scripts" {
			continue
		}
		// Avoid indexing site-packages for every unrelated executable in /usr/bin.
		data, err := readPrefix(executable, 16*1024)
		if err != nil || !pythonLauncher(data) {
			continue
		}
		prefix := filepath.Dir(binDir)
		index, exists := r.pythonIndexes[prefix]
		if !exists {
			index = indexPythonPrefix(prefix)
			r.pythonIndexes[prefix] = index
		}
		for _, entry := range index[name] {
			if pythonEntryMatches(string(data), entry) {
				return entry.description, "python:" + entry.distribution
			}
		}
	}
	return "", ""
}

func pythonLauncher(data []byte) bool {
	if !bytes.HasPrefix(data, []byte("#!")) {
		return false
	}
	line, _, _ := bytes.Cut(data, []byte{'\n'})
	if bytes.Contains(line, []byte("python")) {
		return true
	}
	// pip uses a /bin/sh trampoline when a Python interpreter path is too long.
	if bytes.Contains(line, []byte("sh")) {
		if len(data) > 512 {
			data = data[:512]
		}
		return bytes.Contains(data, []byte("'''exec'")) && bytes.Contains(data, []byte("python"))
	}
	return false
}

func pythonEntryMatches(script string, entry pythonEntry) bool {
	if strings.Contains(script, "from "+entry.module+" import ") || strings.Contains(script, "import "+entry.module+"\n") || strings.Contains(script, "import "+entry.module+"\r\n") {
		return true
	}
	// Older setuptools console scripts refer to their distribution, not module.
	if strings.Contains(script, "load_entry_point(") {
		for _, quote := range []string{"'", `"`} {
			if strings.Contains(script, quote+entry.distribution+"==") || strings.Contains(script, quote+entry.distribution+quote) {
				return true
			}
		}
	}
	return false
}

func indexPythonPrefix(prefix string) map[string][]pythonEntry {
	index := make(map[string][]pythonEntry)
	sites := pythonSiteDirs(prefix)
	count := 0
	for _, site := range sites {
		for _, dir := range readDirBounded(site, maxPythonSiteEntries) {
			if !dir.IsDir() || !strings.HasSuffix(dir.Name(), ".dist-info") {
				continue
			}
			root := filepath.Join(site, dir.Name())
			entryData, err := readPrefix(filepath.Join(root, "entry_points.txt"), maxPythonMetadataBytes)
			if err != nil {
				continue
			}
			commands := parseEntryPoints(string(entryData))
			if len(commands) == 0 {
				continue
			}
			metadata, err := readPrefix(filepath.Join(root, "METADATA"), maxPythonMetadataBytes)
			if err != nil {
				continue
			}
			name, summary := parsePythonMetadata(string(metadata))
			if name == "" || summary == "" || strings.EqualFold(summary, "UNKNOWN") {
				continue
			}
			for command, module := range commands {
				index[command] = append(index[command], pythonEntry{distribution: name, description: summary, module: module})
				count++
				if count >= maxPythonCommands {
					return index
				}
			}
		}
	}
	return index
}

func pythonSiteDirs(prefix string) []string {
	sites := []string{filepath.Join(prefix, "Lib", "site-packages")}
	for _, lib := range []string{"lib", "lib64"} {
		root := filepath.Join(prefix, lib)
		for _, child := range readDirBounded(root, 64) {
			if child.IsDir() && pythonDirName.MatchString(child.Name()) {
				for _, leaf := range []string{"site-packages", "dist-packages"} {
					sites = append(sites, filepath.Join(root, child.Name(), leaf))
				}
				if len(sites) >= 17 {
					return sites
				}
			}
		}
	}
	return sites
}

func parseEntryPoints(data string) map[string]string {
	commands := make(map[string]string)
	console := false
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			console = line == "[console_scripts]"
			continue
		}
		if !console || line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		module, _, hasFunction := strings.Cut(strings.TrimSpace(value), ":")
		module = strings.TrimSpace(module)
		if !hasFunction || name == "" || module == "" || strings.ContainsAny(name, "/\\\x00") {
			continue
		}
		commands[name] = module
		if len(commands) >= 128 {
			break
		}
	}
	return commands
}

func parsePythonMetadata(data string) (string, string) {
	var name, summary string
	var last string
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if last == "Summary" {
				summary += " " + strings.TrimSpace(line)
			}
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			last = ""
			continue
		}
		last = field
		switch field {
		case "Name":
			name = strings.TrimSpace(value)
		case "Summary":
			summary = strings.TrimSpace(value)
		}
	}
	return oneLine(name), oneLine(summary)
}
