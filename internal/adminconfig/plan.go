package adminconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/fsstore"
)

type fileWrite struct {
	Path string
	Data []byte
}

// plan collects staged file writes, removals, and directory creations.
// __general.json is always committed last.
type plan struct {
	writes  []fileWrite
	removes []string
	dirs    []string
}

func (p *plan) addWrite(path string, data []byte) {
	p.writes = append(p.writes, fileWrite{Path: path, Data: data})
}

func (p *plan) addJSONWrite(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	p.addWrite(path, append(data, '\n'))
	return nil
}

func (p *plan) addRemove(path string) {
	p.removes = append(p.removes, path)
}

func (p *plan) addDir(path string) {
	p.dirs = append(p.dirs, path)
}

func (p *plan) orderedWrites() []fileWrite {
	ordered := make([]fileWrite, 0, len(p.writes))
	var general []fileWrite
	for _, w := range p.writes {
		if filepath.Base(w.Path) == "__general.json" {
			general = append(general, w)
			continue
		}
		ordered = append(ordered, w)
	}
	return append(ordered, general...)
}

func (p *plan) commit() error {
	dirs := append([]string{}, p.dirs...)
	sort.Strings(dirs)
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	for _, r := range p.removes {
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

func stageGeneralBump(p *plan, cfg *config.Config) (int, error) {
	next := cfg.General
	next.General.Version = cfg.General.General.Version + 1
	if err := p.addJSONWrite(generalPath(cfg), next); err != nil {
		return 0, err
	}
	return next.General.Version, nil
}
