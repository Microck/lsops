package describe

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

const maxPackageBytes = 256 * 1024

type npmPackage struct {
	name        string
	description string
	binTargets  []string
}

func (r *Resolver) npmDescription(paths []string) (string, string) {
	for _, executable := range paths {
		dir := filepath.Dir(executable)
		for depth := 0; depth < 8; depth++ {
			pkg, found := r.npmPackages[dir]
			if !found {
				pkg = readNPMPackage(dir)
				r.npmPackages[dir] = pkg
			}
			if pkg != nil && pkg.description != "" {
				for _, target := range pkg.binTargets {
					if target == executable {
						return pkg.description, "npm:" + pkg.name
					}
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", ""
}

func readNPMPackage(dir string) *npmPackage {
	data, err := readPrefix(filepath.Join(dir, "package.json"), maxPackageBytes+1)
	if err != nil || len(data) > maxPackageBytes {
		return nil
	}
	var metadata struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Bin         json.RawMessage `json:"bin"`
	}
	if json.Unmarshal(data, &metadata) != nil {
		return nil
	}
	name, description := oneLine(metadata.Name), oneLine(metadata.Description)
	if name == "" || description == "" {
		return nil
	}
	var target string
	var bins map[string]string
	if json.Unmarshal(metadata.Bin, &target) == nil {
		bins = map[string]string{filepath.Base(name): target}
	} else if json.Unmarshal(metadata.Bin, &bins) != nil {
		return nil
	}
	pkg := &npmPackage{name: name, description: description}
	for _, target := range bins {
		if len(pkg.binTargets) >= 128 {
			break
		}
		if target == "" || filepath.IsAbs(target) {
			continue
		}
		// Preserve a symlink/.. pair until EvalSymlinks resolves it.
		path := dir + string(filepath.Separator) + filepath.FromSlash(target)
		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			pkg.binTargets = append(pkg.binTargets, resolved)
		}
	}
	return pkg
}
