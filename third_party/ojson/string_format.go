package ojson

import (
	"fmt"
	"reflect"
	"sync"
)

// StringFormatValidator validates string values for a registered format.
type StringFormatValidator interface {
	ValidateString(value string) error
}

type stringFormatFunc func(string) error

func (f stringFormatFunc) ValidateString(value string) error {
	return f(value)
}

// StringFormatFunc adapts a function into a StringFormatValidator.
func StringFormatFunc(fn func(string) error) StringFormatValidator {
	return stringFormatFunc(fn)
}

type stringFormatDef struct {
	name      string
	validator StringFormatValidator
	goType    reflect.Type
}

// StringFormatRegistry stores application-registered string formats.
//
// Registries are concurrency-safe. Schema compilation snapshots the formats it
// needs so later registry mutations cannot change already compiled schemas.
type StringFormatRegistry struct {
	mu      sync.RWMutex
	formats map[string]stringFormatDef
}

// NewStringFormatRegistry creates an empty format registry.
func NewStringFormatRegistry() *StringFormatRegistry {
	return &StringFormatRegistry{
		formats: make(map[string]stringFormatDef),
	}
}

// Register adds a custom string format.
//
// goType is optional. When non-nil it is stored for schema-to-struct
// suggestions. Built-in format names cannot be overridden.
func (r *StringFormatRegistry) Register(name string, validator StringFormatValidator, goType reflect.Type) error {
	if r == nil {
		return fmt.Errorf("string format registry is nil")
	}
	if name == "" {
		return fmt.Errorf("string format name must not be empty")
	}
	if isBuiltinStringFormat(name) {
		return fmt.Errorf("cannot override built-in string format %q", name)
	}
	if validator == nil {
		return fmt.Errorf("string format %q validator must not be nil", name)
	}
	if goType != nil && goType.Kind() == reflect.Invalid {
		return fmt.Errorf("string format %q go type is invalid", name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.formats[name]; exists {
		return fmt.Errorf("string format %q is already registered", name)
	}
	r.formats[name] = stringFormatDef{
		name:      name,
		validator: validator,
		goType:    goType,
	}
	return nil
}

func (r *StringFormatRegistry) lookup(name string) (stringFormatDef, bool) {
	if r == nil {
		return stringFormatDef{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.formats[name]
	return def, ok
}

func (r *StringFormatRegistry) snapshot() *StringFormatRegistry {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	clone := NewStringFormatRegistry()
	for name, def := range r.formats {
		clone.formats[name] = def
	}
	return clone
}

// SchemaCompileOption configures schema compilation.
type SchemaCompileOption interface {
	applySchemaCompileOption(*schemaCompileConfig)
}

type schemaCompileConfig struct {
	formats *StringFormatRegistry
}

func newSchemaCompileConfig(opts ...SchemaCompileOption) schemaCompileConfig {
	cfg := schemaCompileConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt.applySchemaCompileOption(&cfg)
		}
	}
	if cfg.formats != nil {
		cfg.formats = cfg.formats.snapshot()
	}
	return cfg
}

type withStringFormatsOption struct {
	registry *StringFormatRegistry
}

// WithStringFormats supplies a custom string format registry for schema compilation.
func WithStringFormats(registry *StringFormatRegistry) SchemaCompileOption {
	return withStringFormatsOption{registry: registry}
}

func (o withStringFormatsOption) applySchemaCompileOption(cfg *schemaCompileConfig) {
	cfg.formats = o.registry
}

func isBuiltinStringFormat(format string) bool {
	switch format {
	case "email", "tel", "url":
		return true
	default:
		return false
	}
}
