package adminconfig

import (
	"encoding/json"
	"path/filepath"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/datoriumdb/internal/search"
)

// EnsureSearchResult reports the outcome of a declarative search ensure or delete.
type EnsureSearchResult struct {
	Changed        bool
	SearchVersion  int
	GeneralVersion int
}

// EnsureSearch implements declarative ensure for a search definition.
func (s *Store) EnsureSearch(collection, name string, defBytes []byte) (EnsureSearchResult, []envelope.Error) {
	result, errs := s.runMutation(func(cfg *config.Config) (*plan, any, []envelope.Error) {
		fields := EnsureSearchResult{}
		if _, exists := cfg.Schemas[collection]; !exists {
			return nil, fields, []envelope.Error{{Code: "collectionNotFound", Message: "collection does not exist", Actual: collection}}
		}
		current, exists := cfg.Searches[collection][name]
		if !exists {
			return s.ensureCreateSearch(cfg, collection, name, defBytes)
		}
		equal, err := jsonBytesEqual(current, defBytes)
		if err != nil {
			return nil, fields, []envelope.Error{{Code: "invalidJSON", Message: err.Error()}}
		}
		if equal {
			fields.SearchVersion = cfg.SearchVersions[collection][name]
			return nil, fields, nil
		}
		return nil, fields, []envelope.Error{{Code: "searchDefinitionConflict", Message: "search definition differs from the current definition", Actual: name}}
	})
	if len(errs) > 0 {
		if result != nil {
			return result.(EnsureSearchResult), errs
		}
		return EnsureSearchResult{}, errs
	}
	return result.(EnsureSearchResult), nil
}

func (s *Store) ensureCreateSearch(cfg *config.Config, collection, name string, raw []byte) (*plan, EnsureSearchResult, []envelope.Error) {
	fields := EnsureSearchResult{}
	if errs := config.ValidateSearchDefinition(raw, collection, name, cfg.Schemas); len(errs) > 0 {
		return nil, fields, errs
	}
	if def, err := search.ParseDefinition(raw); err == nil {
		if schemaRaw, ok := cfg.Schemas[collection]; ok {
			if compiled, cerr := config.CompileSchemaBytes(schemaRaw); cerr == nil {
				if verrs := def.Validate(compiled.Root(), cfg.Schemas); len(verrs) > 0 {
					return nil, fields, verrs
				}
			}
		}
	}
	pretty, err := reindentJSON(raw)
	if err != nil {
		return nil, fields, []envelope.Error{{Code: "invalidJSON", Message: err.Error()}}
	}
	if cfg.Searches[collection] == nil {
		cfg.Searches[collection] = map[string]json.RawMessage{}
	}
	cfg.Searches[collection][name] = json.RawMessage(pretty)
	if cfg.SearchHistory[collection] == nil {
		cfg.SearchHistory[collection] = map[string]map[int]json.RawMessage{}
	}
	if cfg.SearchHistory[collection][name] == nil {
		cfg.SearchHistory[collection][name] = map[int]json.RawMessage{}
	}
	cfg.SearchHistory[collection][name][1] = json.RawMessage(pretty)
	if cfg.SearchVersions[collection] == nil {
		cfg.SearchVersions[collection] = map[string]int{}
	}
	cfg.SearchVersions[collection][name] = 1
	if errs := cfg.ValidateDetailed(); len(errs) > 0 {
		return nil, fields, errs
	}

	p := &plan{}
	p.addWrite(searchPath(cfg, collection, name), pretty)
	p.addWrite(searchVersionPath(cfg, collection, name, 1), pretty)
	p.addDir(filepath.Join(s.DataDir, collection, ".search", name))
	nextVersion, err := stageGeneralBump(p, cfg)
	if err != nil {
		return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
	}
	fields.Changed = true
	fields.SearchVersion = 1
	fields.GeneralVersion = nextVersion
	return p, fields, nil
}

// DeleteSearch removes a search definition when present.
func (s *Store) DeleteSearch(collection, name string) (EnsureSearchResult, []envelope.Error) {
	result, errs := s.runMutation(func(cfg *config.Config) (*plan, any, []envelope.Error) {
		fields := EnsureSearchResult{}
		if _, exists := cfg.Searches[collection][name]; !exists {
			return nil, fields, nil
		}
		delete(cfg.Searches[collection], name)
		if errs := cfg.ValidateDetailed(); len(errs) > 0 {
			return nil, fields, errs
		}
		p := &plan{}
		p.addRemove(searchPath(cfg, collection, name))
		nextVersion, err := stageGeneralBump(p, cfg)
		if err != nil {
			return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
		}
		fields.Changed = true
		fields.GeneralVersion = nextVersion
		return p, fields, nil
	})
	if len(errs) > 0 {
		if result != nil {
			return result.(EnsureSearchResult), errs
		}
		return EnsureSearchResult{}, errs
	}
	return result.(EnsureSearchResult), nil
}
