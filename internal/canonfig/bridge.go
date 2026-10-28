// Package canonfig implements lsops' optional, local Canonfig catalogue bridge.
// Canonfig transports a staging file; lsops alone owns its live catalogue.
package canonfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Microck/lsops/internal/config"
)

const limit = 1 << 20

type Options struct {
	Config, Bundle string
	Apply          bool
}
type Result struct {
	Added     []string       `json:"added"`
	Updated   []string       `json:"updated"`
	Removed   []string       `json:"removed"`
	Local     []string       `json:"local"`
	Conflicts []string       `json:"conflicts"`
	Changed   bool           `json:"changed"`
	Applied   bool           `json:"applied"`
	Catalogue *config.Config `json:"catalogue,omitempty"`
}
type ownership struct {
	Version int                    `json:"version"`
	Config  string                 `json:"config"`
	Bundle  string                 `json:"bundle"`
	Owned   map[string]config.Tool `json:"owned"`
}

func project(t config.Tool) config.Tool {
	return config.Tool{Description: t.Description, Why: t.Why, When: t.When, InstallURL: t.InstallURL, InstallManager: t.InstallManager, InstallSHA256: t.InstallSHA256}
}
func portable(c config.Config) config.Config {
	out := config.Config{Version: 1, Tools: map[string]config.Tool{}}
	for n, t := range c.Tools {
		if t.Description != "" && !t.Hide {
			out.Tools[n] = project(t)
		}
	}
	return out
}
func decode(data []byte, shared bool) (config.Config, error) {
	if e := strictJSON(data); e != nil {
		return config.Config{}, e
	}
	if shared {
		var shape map[string]json.RawMessage
		if e := json.Unmarshal(data, &shape); e != nil {
			return config.Config{}, e
		}
		tools, ok := shape["tools"]
		if !ok || len(tools) == 0 || tools[0] != '{' {
			return config.Config{}, errors.New("shared catalogue requires a tools object; use an explicit empty object to remove owned metadata")
		}
	}
	c, e := config.Decode(data)
	if e != nil {
		return c, e
	}
	seen := map[string]bool{}
	for n, t := range c.Tools {
		key := strings.ToLower(n)
		if seen[key] {
			return c, fmt.Errorf("case-colliding tool: %s", n)
		}
		seen[key] = true
		if shared && (t != project(t) || t.Description == "") {
			return c, fmt.Errorf("shared tool %s contains local settings or lacks a description", n)
		}
	}
	if shared && len(c.Projects) > 0 {
		return c, errors.New("shared catalogue cannot carry project trust")
	}
	return c, nil
}
func encode(v any) ([]byte, error) {
	b, e := json.MarshalIndent(v, "", "  ")
	return append(b, '\n'), e
}

// Plan preserves unowned tools, including identical pre-existing entries. A
// conflict blocks the entire batch. Removing an owned entry never uninstalls it.
func Plan(current, incoming config.Config, owned map[string]config.Tool) (Result, config.Config, map[string]config.Tool) {
	r := Result{Added: []string{}, Updated: []string{}, Removed: []string{}, Local: []string{}, Conflicts: []string{}}
	next := config.Config{Version: 1, Projects: append([]string(nil), current.Projects...), Tools: map[string]config.Tool{}}
	for n, t := range current.Tools {
		next.Tools[n] = t
	}
	nextOwned := map[string]config.Tool{}
	names := map[string]bool{}
	actual := map[string]string{}
	for n := range current.Tools {
		actual[strings.ToLower(n)] = n
		if _, ok := owned[n]; !ok {
			r.Local = append(r.Local, n)
		}
	}
	for n := range owned {
		names[n] = true
	}
	for n := range incoming.Tools {
		names[n] = true
	}
	ordered := []string{}
	for n := range names {
		ordered = append(ordered, n)
	}
	sort.Strings(ordered)
	for _, n := range ordered {
		cur, exists := current.Tools[n]
		prev, wasOwned := owned[n]
		desired, wanted := incoming.Tools[n]
		if a, ok := actual[strings.ToLower(n)]; ok && a != n {
			r.Conflicts = append(r.Conflicts, n+": name differs in case from local "+a)
			continue
		}
		if !wasOwned {
			if exists {
				if wanted && project(cur) != desired {
					r.Conflicts = append(r.Conflicts, n+": independently registered locally")
				}
			} else if wanted {
				next.Tools[n] = desired
				nextOwned[n] = desired
				r.Added = append(r.Added, n)
			}
			continue
		}
		if !exists || project(cur) != prev {
			r.Conflicts = append(r.Conflicts, n+": local edits to shared metadata")
			continue
		}
		local := cur
		local.Description = ""
		local.Why = ""
		local.When = ""
		local.InstallURL = ""
		local.InstallManager = ""
		local.InstallSHA256 = ""
		if wanted {
			desired.Path = local.Path
			desired.Help = local.Help
			desired.Hide = local.Hide
			desired.Include = local.Include
			next.Tools[n] = desired
			nextOwned[n] = project(desired)
			if project(desired) != prev {
				r.Updated = append(r.Updated, n)
			}
		} else {
			if local == (config.Tool{}) {
				delete(next.Tools, n)
			} else {
				next.Tools[n] = local
			}
			r.Removed = append(r.Removed, n)
		}
	}
	sort.Strings(r.Local)
	r.Changed = len(r.Added)+len(r.Updated)+len(r.Removed) > 0
	return r, next, nextOwned
}

// Canonicalize existing parents (including OS aliases such as macOS /var).
// Leaf symlinks are rejected separately, never followed. Parents must exist.
func canonical(file string) (string, error) {
	a, e := filepath.Abs(file)
	if e != nil {
		return "", e
	}
	p, e := filepath.EvalSymlinks(filepath.Dir(a))
	if e != nil {
		return "", e
	}
	return filepath.Join(p, filepath.Base(a)), nil
}
func inspect(file string) (os.FileInfo, error) {
	s, e := os.Lstat(file)
	if errors.Is(e, os.ErrNotExist) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if !s.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %s", file)
	}
	return s, nil
}
func read(file string) ([]byte, error) {
	s, e := inspect(file)
	if e != nil || s == nil {
		return nil, e
	}
	if s.Size() > limit {
		return nil, fmt.Errorf("file exceeds 1 MiB: %s", file)
	}
	f, e := os.Open(file)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	actual, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !os.SameFile(s, actual) {
		return nil, errors.New("file changed while opening")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil {
		return nil, e
	}
	if len(b) > limit || !utf8.Valid(b) {
		return nil, errors.New("file exceeds 1 MiB or is not UTF-8")
	}
	return b, nil
}
func same(a, b string) bool { return strings.EqualFold(a, b) }
func paths(o Options) (Options, error) {
	var e error
	o.Config, e = canonical(o.Config)
	if e != nil {
		return o, e
	}
	o.Bundle, e = canonical(o.Bundle)
	if e != nil {
		return o, e
	}
	if same(o.Config, o.Bundle+".capture-lock") {
		return o, errors.New("config conflicts with capture lock")
	}
	b, e := inspect(o.Bundle)
	if e != nil {
		return o, e
	}
	state := o.Config + ".canonfig.json"
	for _, f := range []string{o.Config, o.Config + ".updates.json", o.Config + ".update-lock", state, state + ".pending", state + ".lock", state + ".last-transaction.json"} {
		if same(f, o.Bundle) {
			return o, errors.New("bundle must be separate from config, approvals and bridge state")
		}
		s, err := os.Lstat(f)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return o, err
		}
		if b != nil && s != nil && os.SameFile(b, s) {
			return o, errors.New("bundle aliases a reserved local file")
		}
	}
	return o, nil
}
func flushDir(file string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, e := os.Open(filepath.Dir(file))
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func replace(file string, data, before []byte) error {
	if len(data) > limit {
		return errors.New("output exceeds 1 MiB")
	}
	old, e := read(file)
	if e != nil {
		return e
	}
	if !bytes.Equal(old, before) {
		return fmt.Errorf("concurrent edit: %s", file)
	}
	f, e := os.CreateTemp(filepath.Dir(file), ".lsops-canonfig-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = f.Chmod(0600); e != nil {
		return e
	}
	if _, e = f.Write(data); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	old, e = read(file)
	if e != nil {
		return e
	}
	if !bytes.Equal(old, before) {
		return fmt.Errorf("concurrent edit: %s", file)
	}
	if e = os.Rename(f.Name(), file); e != nil {
		return e
	}
	return flushDir(file)
}
func lock(file string) (func(), error) {
	if e := os.Mkdir(file, 0700); e != nil {
		return nil, fmt.Errorf("cannot acquire %s; check for an active worker or interrupted operation: %w", file, e)
	}
	return func() { _ = os.Remove(file) }, nil
}

func Capture(o Options) (Result, error) {
	r := Result{}
	o, e := paths(o)
	if e != nil {
		return r, e
	}
	if o.Apply {
		unlock, e := lock(o.Bundle + ".capture-lock")
		if e != nil {
			return r, e
		}
		defer unlock()
	}
	before, e := read(o.Config)
	if e != nil {
		return r, e
	}
	if before == nil {
		return r, errors.New("source config is missing")
	}
	c, e := decode(before, false)
	if e != nil {
		return r, e
	}
	c = portable(c)
	r.Catalogue = &c
	after, e := encode(c)
	if e != nil {
		return r, e
	}
	prior, e := read(o.Bundle)
	if e != nil {
		return r, e
	}
	r.Changed = !bytes.Equal(prior, after)
	if o.Apply && r.Changed {
		now, e := read(o.Config)
		if e != nil {
			return r, e
		}
		if !bytes.Equal(now, before) {
			return r, errors.New("source changed during capture")
		}
		if e = replace(o.Bundle, after, prior); e != nil {
			return r, e
		}
		r.Applied = true
	}
	return r, nil
}

func Sync(o Options) (Result, error) { return syncWith(o, replace) }
func syncWith(o Options, write func(string, []byte, []byte) error) (Result, error) {
	r := Result{}
	o, e := paths(o)
	if e != nil {
		return r, e
	}
	state := o.Config + ".canonfig.json"
	pending := state + ".pending"
	receipt := state + ".last-transaction.json"
	if o.Apply {
		unlock, e := lock(o.Config + ".update-lock")
		if e != nil {
			return r, e
		}
		defer unlock()
		unlockBridge, e := lock(state + ".lock")
		if e != nil {
			return r, e
		}
		defer unlockBridge()
	}
	b, e := inspect(pending)
	if e != nil {
		return r, e
	}
	if b != nil {
		return r, fmt.Errorf("interrupted sync: inspect %s and restore both recorded backups before clearing it; no automatic recovery", pending)
	}
	incomingRaw, e := read(o.Bundle)
	if e != nil {
		return r, e
	}
	if incomingRaw == nil {
		return r, errors.New("shared catalogue missing; absence is not an empty catalogue")
	}
	incoming, e := decode(incomingRaw, true)
	if e != nil {
		return r, e
	}
	before, e := read(o.Config)
	if e != nil {
		return r, e
	}
	current := config.Config{Version: 1, Tools: map[string]config.Tool{}}
	if before != nil {
		current, e = decode(before, false)
		if e != nil {
			return r, e
		}
	}
	stateBefore, e := read(state)
	if e != nil {
		return r, e
	}
	owned := map[string]config.Tool{}
	if stateBefore != nil {
		if e = strictJSON(stateBefore); e != nil {
			return r, e
		}
		var s ownership
		d := json.NewDecoder(bytes.NewReader(stateBefore))
		d.DisallowUnknownFields()
		if e = d.Decode(&s); e != nil {
			return r, e
		}
		var extra any
		if e = d.Decode(&extra); e != io.EOF {
			return r, errors.New("invalid trailing ownership data")
		}
		if s.Version != 1 || s.Config != o.Config || s.Bundle != o.Bundle || s.Owned == nil {
			return r, errors.New("ownership state does not match config and bundle")
		}
		raw, _ := encode(config.Config{Version: 1, Tools: s.Owned})
		validated, e := decode(raw, true)
		if e != nil {
			return r, e
		}
		owned = validated.Tools
	}
	r, next, nextOwned := Plan(current, incoming, owned)
	if len(r.Conflicts) > 0 || !o.Apply {
		return r, nil
	}
	after, e := encode(next)
	if e != nil {
		return r, e
	}
	stateAfter, e := encode(ownership{1, o.Config, o.Bundle, nextOwned})
	if e != nil {
		return r, e
	}
	// Keep idle passes read-only; do not adopt existing identical local entries.
	if reflect.DeepEqual(current, next) && reflect.DeepEqual(owned, nextOwned) {
		return r, nil
	}
	if len(after) > limit || len(stateAfter) > limit {
		return r, errors.New("merged catalogue or ownership state exceeds 1 MiB")
	}
	for f, want := range map[string][]byte{o.Bundle: incomingRaw, o.Config: before, state: stateBefore} {
		got, e := read(f)
		if e != nil {
			return r, e
		}
		if !bytes.Equal(got, want) {
			return r, errors.New("concurrent edit before sync")
		}
	}
	if _, e = inspect(receipt); e != nil {
		return r, e
	}
	journal, e := encode(struct {
		Version     int     `json:"version"`
		Config      string  `json:"config"`
		Bundle      string  `json:"bundle"`
		Before      *string `json:"before"`
		StateBefore *string `json:"state_before"`
		After       string  `json:"after"`
		StateAfter  string  `json:"state_after"`
	}{1, o.Config, o.Bundle, backup(before), backup(stateBefore), string(after), string(stateAfter)})
	if e != nil {
		return r, e
	}
	// A journal can hold several 1 MiB snapshots. It is not read as catalogue data.
	f, e := os.OpenFile(pending, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return r, e
	}
	_, e = f.Write(journal)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return r, e
	}
	if e = flushDir(pending); e != nil {
		return r, e
	}
	if e = write(o.Config, after, before); e != nil {
		return r, e
	}
	if e = write(state, stateAfter, stateBefore); e != nil {
		return r, e
	}
	if e = os.Rename(pending, receipt); e != nil {
		return r, e
	}
	if e = flushDir(receipt); e != nil {
		return r, e
	}
	r.Applied = true
	return r, nil
}
func backup(b []byte) *string {
	if b == nil {
		return nil
	}
	s := string(b)
	return &s
}

// Duplicate keys and deeply nested JSON are ambiguous input, not deletion intent.
func strictJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 64 {
			return errors.New("JSON nesting exceeds 64")
		}
		token, e := d.Token()
		if e != nil {
			return e
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, e := d.Token()
				if e != nil {
					return e
				}
				name, ok := key.(string)
				name = foldJSONKey(name)
				if !ok || seen[name] {
					return errors.New("duplicate or invalid JSON key")
				}
				seen[name] = true
				if e = value(depth + 1); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if e = value(depth + 1); e != nil {
					return e
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		_, e = d.Token()
		return e
	}
	if e := value(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

// Match encoding/json's Unicode simple folding, not only ASCII lowercase.
func foldJSONKey(s string) string {
	return strings.Map(func(r rune) rune {
		min := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < min {
				min = next
			}
		}
		return min
	}, s)
}
