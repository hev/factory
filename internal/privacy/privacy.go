// Package privacy provides bounded remediation of offline factory artifacts.
package privacy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unicode/utf8"
)

const maxSize = 16 << 20

var invalid = errors.New("privacy: invalid, changed, unsafe or unsupported input (content withheld)")
var allowed = regexp.MustCompile(`^(sessions/[a-z2-7]{6}/(prompt\.md|log\.jsonl)|jobs/[a-z2-7]{6}/log\.md)$`)

type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
}
type Target struct {
	Path   string  `json:"path"`
	Before string  `json:"before_sha256"`
	After  string  `json:"after_sha256,omitempty"`
	Ranges []Range `json:"ranges"`
}
type Plan struct {
	Phase    string   `json:"phase,omitempty"`
	Version  int      `json:"version"`
	Targets  []Target `json:"targets"`
	Index    string   `json:"indexed_copies"`
	Native   string   `json:"native_harness_sources"`
	Complete bool     `json:"full_remediation"`
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func read(path string) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, invalid
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil || !s.Mode().IsRegular() || s.Size() > maxSize {
		return nil, invalid
	}
	b, e := io.ReadAll(io.LimitReader(f, maxSize+1))
	if e != nil || len(b) > maxSize {
		return nil, invalid
	}
	return b, nil
}
func decode(path string) (Plan, error) {
	var p Plan
	b, e := read(path)
	if e != nil {
		return p, e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return p, invalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return p, invalid
	}
	return p, nil
}
func safe(root, rel string) (string, error) {
	p := root
	for _, part := range strings.Split(rel, "/") {
		p = filepath.Join(p, part)
		s, e := os.Lstat(p)
		if e != nil || s.Mode()&os.ModeSymlink != 0 {
			return "", invalid
		}
	}
	return p, nil
}
func transform(b []byte, t Target) ([]byte, error) {
	if len(t.Ranges) == 0 || len(t.Ranges) > 128 {
		return nil, invalid
	}
	if !utf8.Valid(b) {
		return nil, invalid
	}
	out := append([]byte(nil), b...)
	last := 0
	for _, r := range t.Ranges {
		if r.Start < last || r.End <= r.Start || r.End > len(b) {
			return nil, invalid
		}
		if (r.Start < len(b) && !utf8.RuneStart(b[r.Start])) || (r.End < len(b) && !utf8.RuneStart(b[r.End])) {
			return nil, invalid
		}
		last = r.End
		// Keep line boundaries and JSON encoding bytes intact. Requests must select
		// literal string content, never syntax or escape sequences.
		for i := r.Start; i < r.End; i++ {
			if b[i] == '\n' || b[i] == '\r' || b[i] == '"' || b[i] == '\\' {
				return nil, invalid
			}
			out[i] = 'x'
		}
	}
	if strings.HasSuffix(t.Path, ".jsonl") {
		if !bytes.HasSuffix(b, []byte("\n")) {
			return nil, invalid
		}
		for _, line := range bytes.Split(bytes.TrimSuffix(out, []byte("\n")), []byte("\n")) {
			if !uniqueJSON(line) {
				return nil, invalid
			}
		}
		// Each changed byte must be inside a JSON string, not a key or scalar.
		in, escape := false, 0
		for i, c := range b {
			if escape > 0 {
				if b[i] != out[i] {
					return nil, invalid
				}
				escape--
				continue
			}
			if c == '\\' && in {
				escape = 1
				if i+1 < len(b) && b[i+1] == 'u' {
					escape = 5
				}
				continue
			}
			if c == '"' {
				in = !in
				continue
			}
			if b[i] != out[i] && !in {
				return nil, invalid
			}
		}
		// Reject edits to keys by comparing decoded object structure and keys.
		var a, z any
		for i, line := range bytes.Split(bytes.TrimSuffix(b, []byte("\n")), []byte("\n")) {
			if !uniqueJSON(line) || json.Unmarshal(line, &a) != nil {
				return nil, invalid
			}
			json.Unmarshal(bytes.Split(bytes.TrimSuffix(out, []byte("\n")), []byte("\n"))[i], &z)
			if !sameShape(a, z) {
				return nil, invalid
			}
		}
	}
	return out, nil
}
func sameShape(a, b any) bool {
	switch x := a.(type) {
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			w, ok := y[k]
			if !ok {
				return false
			}
			if vs, ok := v.(string); ok {
				ws, ok := w.(string)
				if !ok || (vs != ws && k != "text" && k != "result") {
					return false
				}
			} else if !sameShape(v, w) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i, v := range x {
			if !sameShape(v, y[i]) {
				return false
			}
		}
		return true
	case string:
		_, ok := b.(string)
		return ok && a == b
	default:
		return a == b
	}
}
func persist(path string, p Plan) error {
	b, _ := json.MarshalIndent(p, "", "  ")
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return invalid
	}
	defer f.Close()
	if _, e = f.Write(append(b, '\n')); e != nil {
		return invalid
	}
	if f.Sync() != nil {
		return invalid
	}
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return invalid
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return invalid
	}
	return nil
}

// uniqueJSON rejects duplicate keys, including nested objects. A duplicate-key
// stream has ambiguous meaning across readers and cannot be safely remediated.
func uniqueJSON(b []byte) bool {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var value func() bool
	value = func() bool {
		tok, e := d.Token()
		if e != nil {
			return false
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, e := d.Token()
				if e != nil {
					return false
				}
				k, ok := key.(string)
				if !ok || seen[k] {
					return false
				}
				seen[k] = true
				if !value() {
					return false
				}
			}
			end, e := d.Token()
			return e == nil && end == json.Delim('}')
		case '[':
			for d.More() {
				if !value() {
					return false
				}
			}
			end, e := d.Token()
			return e == nil && end == json.Delim(']')
		}
		return false
	}
	if !value() {
		return false
	}
	_, e := d.Token()
	return e == io.EOF
}

// Execute requires an explicitly offline root. It never searches for targets.
// Apply uses the persisted plan as its write-ahead journal; rerunning recovers
// targets independently, including a crash between a rename and the receipt.
func Execute(mode, root, input, output string) (Plan, error) {
	var empty Plan
	if mode != "plan" && mode != "apply" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return empty, invalid
	}
	// Marker is an operator attestation, not a claim that factory stopped writers.
	if _, e := safe(root, ".privacy-offline"); e != nil {
		return empty, invalid
	}
	resolved, e := filepath.EvalSymlinks(root)
	if e != nil || resolved != root {
		return empty, invalid
	}
	lockPath := filepath.Join(root, ".privacy-lock")
	if st, e := os.Lstat(lockPath); e == nil {
		if !st.Mode().IsRegular() {
			return empty, invalid
		}
	} else if !os.IsNotExist(e) {
		return empty, invalid
	}
	lock, e := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return empty, invalid
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return empty, invalid
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if _, e := os.Lstat(output); !os.IsNotExist(e) {
		return empty, invalid
	}
	p, e := decode(input)
	if e != nil {
		return empty, e
	}
	if mode == "apply" && (p.Index != "unknown" || p.Native != "unsupported" || p.Complete) {
		return empty, invalid
	}
	if p.Version != 1 || len(p.Targets) == 0 || len(p.Targets) > 32 {
		return empty, invalid
	}
	seen := map[string]bool{}
	contents := make([][]byte, len(p.Targets))
	paths := make([]string, len(p.Targets))
	for i, t := range p.Targets {
		if !allowed.MatchString(t.Path) || seen[t.Path] {
			return empty, invalid
		}
		seen[t.Path] = true
		paths[i], e = safe(root, t.Path)
		if e != nil {
			return empty, e
		}
		st, e := os.Stat(paths[i])
		if e != nil {
			return empty, invalid
		}
		stat, ok := st.Sys().(*syscall.Stat_t)
		if !ok || stat.Nlink != 1 || int(stat.Uid) != os.Geteuid() {
			return empty, invalid
		}
		b, e := read(paths[i])
		if e != nil {
			return empty, e
		}
		if mode == "apply" && t.After != "" && digest(b) == t.After {
			continue
		}
		if digest(b) != t.Before {
			return empty, invalid
		}
		out, e := transform(b, t)
		if e != nil {
			return empty, e
		}
		h := digest(out)
		if mode == "apply" && (t.After != h || p.Index != "unknown" || p.Native != "unsupported" || p.Complete) {
			return empty, invalid
		}
		p.Targets[i].After = h
		contents[i] = out
	}
	p.Phase = mode
	p.Index = "unknown"
	p.Native = "unsupported"
	p.Complete = false
	if mode == "plan" {
		return p, persist(output, p)
	}
	// All targets validated before any mutation. No original text in the journal.
	for i, b := range contents {
		if b == nil {
			continue
		}
		s, e := os.Stat(paths[i])
		if e != nil {
			return empty, invalid
		}
		f, e := os.CreateTemp(filepath.Dir(paths[i]), ".privacy-stage-")
		if e != nil {
			return empty, invalid
		}
		tmp := f.Name()
		if f.Chmod(s.Mode().Perm()) != nil {
			f.Close()
			os.Remove(tmp)
			return empty, invalid
		}
		_, e = f.Write(b)
		if e == nil {
			e = f.Sync()
		}
		f.Close()
		if e == nil {
			e = os.Rename(tmp, paths[i])
		}
		os.Remove(tmp)
		if e != nil {
			return empty, invalid
		}
		dir, e := os.Open(filepath.Dir(paths[i]))
		if e != nil {
			return empty, invalid
		}
		e = dir.Sync()
		dir.Close()
		if e != nil {
			return empty, invalid
		}
	}
	return p, persist(output, p)
}
