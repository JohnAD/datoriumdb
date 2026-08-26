package adminconfig

import (
	"encoding/json"
	"path/filepath"

	"github.com/JohnAD/datoriumdb/internal/config"
	"github.com/JohnAD/datoriumdb/internal/envelope"
	"github.com/JohnAD/datoriumdb/internal/schemapatch"
)

// EnsureCollectionResult reports the outcome of a declarative collection ensure.
type EnsureCollectionResult struct {
	Changed        bool
	SchemaVersion  int
	GeneralVersion int
	NewVerID       string
	Created        bool
	Upgraded       bool
}

// EnsureCollection implements declarative ensure for a collection schema.
func (s *Store) EnsureCollection(collection string, schemaBytes, upgradeRaw []byte) (EnsureCollectionResult, []envelope.Error) {
	result, errs := s.runMutation(func(cfg *config.Config) (*plan, any, []envelope.Error) {
		fields := EnsureCollectionResult{}
		if !config.ValidCollectionName(collection) {
			return nil, fields, []envelope.Error{{Code: "invalidCollectionName", Message: "collection name violates naming conventions", Actual: collection}}
		}

		currentSchema, exists := cfg.Schemas[collection]
		if !exists {
			return s.ensureCreateCollection(cfg, collection, schemaBytes, upgradeRaw)
		}
		if len(upgradeRaw) > 0 {
			return s.ensureUpgradeCollection(cfg, collection, upgradeRaw)
		}
		return s.ensureCollectionSchemaMatch(cfg, collection, currentSchema, schemaBytes)
	})
	if len(errs) > 0 {
		if result != nil {
			return result.(EnsureCollectionResult), errs
		}
		return EnsureCollectionResult{}, errs
	}
	return result.(EnsureCollectionResult), nil
}

func (s *Store) ensureCreateCollection(cfg *config.Config, collection string, schemaBytes, upgradeRaw []byte) (*plan, EnsureCollectionResult, []envelope.Error) {
	fields := EnsureCollectionResult{}
	if len(upgradeRaw) > 0 {
		return nil, fields, []envelope.Error{{Code: "invalidArguments", Message: "upgrade cannot be applied when creating a collection"}}
	}
	if len(schemaBytes) == 0 {
		return nil, fields, []envelope.Error{{Code: "schemaRequired", Message: "schema is required to create a collection"}}
	}
	if err := config.ValidateOJSONSchemaBytes(schemaBytes); err != nil {
		return nil, fields, []envelope.Error{{Code: "invalidSchema", Message: err.Error()}}
	}
	if errs := config.ValidateCollectionSchemaRules(schemaBytes, cfg.Schemas); len(errs) > 0 {
		return nil, fields, errs
	}
	pretty, err := reindentJSON(schemaBytes)
	if err != nil {
		return nil, fields, []envelope.Error{{Code: "invalidJSON", Message: err.Error()}}
	}

	cfg.Schemas[collection] = json.RawMessage(pretty)
	if cfg.SchemaHistory[collection] == nil {
		cfg.SchemaHistory[collection] = map[int]json.RawMessage{}
	}
	cfg.SchemaHistory[collection][0] = json.RawMessage(pretty)
	cfg.SchemaVersions[collection] = 0
	if errs := cfg.ValidateDetailed(); len(errs) > 0 {
		return nil, fields, errs
	}

	p := &plan{}
	p.addWrite(schemaPath(cfg, collection), pretty)
	p.addWrite(schemaVersionPath(cfg, collection, 0), pretty)
	p.addDir(filepath.Join(s.DataDir, collection))
	nextVersion, err := stageGeneralBump(p, cfg)
	if err != nil {
		return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
	}
	fields.Changed = true
	fields.Created = true
	fields.SchemaVersion = 0
	fields.GeneralVersion = nextVersion
	return p, fields, nil
}

func (s *Store) ensureCollectionSchemaMatch(cfg *config.Config, collection string, currentSchema json.RawMessage, schemaBytes []byte) (*plan, EnsureCollectionResult, []envelope.Error) {
	fields := EnsureCollectionResult{SchemaVersion: cfg.SchemaVersion(collection)}
	if len(schemaBytes) == 0 {
		return nil, fields, nil
	}
	equal, err := jsonBytesEqual(currentSchema, schemaBytes)
	if err != nil {
		return nil, fields, []envelope.Error{{Code: "invalidJSON", Message: err.Error()}}
	}
	if equal {
		return nil, fields, nil
	}
	return nil, fields, []envelope.Error{{Code: "schemaDrift", Message: "provided schema does not match the current collection schema", Actual: collection}}
}

func (s *Store) ensureUpgradeCollection(cfg *config.Config, collection string, upgradeRaw []byte) (*plan, EnsureCollectionResult, []envelope.Error) {
	fields := EnsureCollectionResult{}
	spec, err := schemapatch.ParseUpdateSpec(upgradeRaw)
	if err != nil {
		return nil, fields, []envelope.Error{{Code: "invalidSchemaUpgrade", Message: err.Error()}}
	}

	currentVersion := cfg.SchemaVersion(collection)
	fields.SchemaVersion = currentVersion

	if upgradeAlreadyApplied(cfg, collection, spec, upgradeRaw) {
		return nil, fields, nil
	}

	if errs := spec.Validate(currentVersion); len(errs) > 0 {
		if hasErrCode(errs, "staleSchemaVersion") && currentVersion > spec.From {
			if upgradeAlreadyApplied(cfg, collection, spec, upgradeRaw) {
				return nil, fields, nil
			}
		}
		return nil, fields, errs
	}

	currentSchema := cfg.Schemas[collection]
	newSchemaBytes, err := schemapatch.Apply(currentSchema, spec)
	if err != nil {
		return nil, fields, []envelope.Error{{Code: "invalidSchemaUpgrade", Message: err.Error()}}
	}
	if err := config.ValidateOJSONSchemaBytes(newSchemaBytes); err != nil {
		return nil, fields, []envelope.Error{{Code: "invalidSchemaUpgrade", Message: err.Error()}}
	}
	tempSchemas := make(map[string]json.RawMessage, len(cfg.Schemas))
	for k, v := range cfg.Schemas {
		tempSchemas[k] = v
	}
	tempSchemas[collection] = newSchemaBytes
	if errs := config.ValidateCollectionSchemaRules(newSchemaBytes, tempSchemas); len(errs) > 0 {
		return nil, fields, errs
	}

	newVer := currentVersion + 1
	newSchemaBytes = ensureTrailingNewline(newSchemaBytes)
	cfg.Schemas[collection] = json.RawMessage(newSchemaBytes)
	if cfg.SchemaHistory[collection] == nil {
		cfg.SchemaHistory[collection] = map[int]json.RawMessage{}
	}
	cfg.SchemaHistory[collection][newVer] = json.RawMessage(newSchemaBytes)
	cfg.SchemaVersions[collection] = newVer
	if cfg.SchemaUpdateHistory[collection] == nil {
		cfg.SchemaUpdateHistory[collection] = map[int]json.RawMessage{}
	}
	normalizedUpgrade := ensureTrailingNewline(upgradeRaw)
	cfg.SchemaUpdateHistory[collection][newVer] = json.RawMessage(normalizedUpgrade)
	if errs := cfg.ValidateDetailed(); len(errs) > 0 {
		return nil, fields, errs
	}

	p := &plan{}
	p.addWrite(schemaPath(cfg, collection), newSchemaBytes)
	p.addWrite(schemaVersionPath(cfg, collection, newVer), newSchemaBytes)
	p.addWrite(schemaUpdatePath(cfg, collection, newVer), normalizedUpgrade)
	nextGeneralVersion, err := stageGeneralBump(p, cfg)
	if err != nil {
		return nil, fields, []envelope.Error{{Code: "filesystemError", Message: err.Error()}}
	}
	fields.Changed = true
	fields.Upgraded = true
	fields.SchemaVersion = newVer
	fields.GeneralVersion = nextGeneralVersion
	fields.NewVerID = spec.NewVerID
	return p, fields, nil
}

func upgradeAlreadyApplied(cfg *config.Config, collection string, spec *schemapatch.UpdateSpec, _ []byte) bool {
	targetVer := spec.From + 1
	currentVersion := cfg.SchemaVersion(collection)
	if currentVersion < targetVer {
		return false
	}
	storedRaw, ok := cfg.SchemaUpdateHistory[collection][targetVer]
	if !ok {
		return false
	}
	storedSpec, err := schemapatch.ParseUpdateSpec(storedRaw)
	if err != nil {
		return false
	}
	return storedSpec.NewVerID == spec.NewVerID
}
