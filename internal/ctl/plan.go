package ctl

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

// FileWrite is one staged file write.
type FileWrite struct {
	Path string
	Data []byte
}

// Plan collects a set of file writes, removals, and directory creations
// that a mutating command wants to apply atomically. __general.json is
// always committed last, per COMMAND-LINE-TOOLS.md.
type Plan struct {
	Writes  []FileWrite
	Removes []string
	Dirs    []string
}

// AddWrite stages a file write.
func (p *Plan) AddWrite(path string, data []byte) {
	p.Writes = append(p.Writes, FileWrite{Path: path, Data: data})
}

// AddJSONWrite stages a pretty-printed JSON file write.
func (p *Plan) AddJSONWrite(path string, v any) error {
	data, err := MarshalPretty(v)
	if err != nil {
		return err
	}
	p.AddWrite(path, data)
	return nil
}

// AddRemove stages a file removal.
func (p *Plan) AddRemove(path string) {
	p.Removes = append(p.Removes, path)
}

// AddDir stages a directory to create.
func (p *Plan) AddDir(path string) {
	p.Dirs = append(p.Dirs, path)
}

// FilesWritten returns the basenames of every staged write, in commit order.
func (p *Plan) FilesWritten() []string {
	ordered := p.orderedWrites()
	names := make([]string, 0, len(ordered))
	for _, w := range ordered {
		names = append(names, filepath.Base(w.Path))
	}
	return names
}

// FilesRemoved returns the basenames of every staged removal.
func (p *Plan) FilesRemoved() []string {
	names := make([]string, 0, len(p.Removes))
	for _, r := range p.Removes {
		names = append(names, filepath.Base(r))
	}
	return names
}

// orderedWrites returns Writes with __general.json moved to the end,
// otherwise preserving staging order.
func (p *Plan) orderedWrites() []FileWrite {
	ordered := make([]FileWrite, 0, len(p.Writes))
	var general []FileWrite
	for _, w := range p.Writes {
		if filepath.Base(w.Path) == "__general.json" {
			general = append(general, w)
			continue
		}
		ordered = append(ordered, w)
	}
	return append(ordered, general...)
}

// Commit creates directories, removes files, and writes files, in that
// order, with __general.json written last among writes. It uses atomic
// same-directory rename so readers never see a partial write.
func (p *Plan) Commit() error {
	dirs := append([]string{}, p.Dirs...)
	sort.Strings(dirs)
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	for _, r := range p.Removes {
		if err := os.Remove(r); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for _, w := range p.orderedWrites() {
		if err := fsstore.WriteFileAtomic(w.Path, w.Data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// MarshalPretty renders v as 2-space-indented JSON with a trailing newline.
func MarshalPretty(v any) ([]byte, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// ReindentJSON reformats raw JSON bytes with 2-space indentation while
// preserving the original object field order, and appends a trailing
// newline. It fails if raw is not valid JSON.
func ReindentJSON(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	out = append(out, '\n')
	return out, nil
}
