package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ximilala/viking-go/internal/memory/mergeop"
	"gopkg.in/yaml.v3"
)

// MemoryField defines a single field within a memory type schema.
type MemoryField struct {
	Name        string              `yaml:"name"        json:"name"`
	FieldType   mergeop.FieldType   `yaml:"field_type"  json:"field_type"`
	Description string              `yaml:"description" json:"description"`
	MergeOp     mergeop.MergeOpType `yaml:"merge_op"    json:"merge_op"`
}

// MemoryTypeSchema defines the schema for a memory type, loaded from YAML.
type MemoryTypeSchema struct {
	MemoryType       string        `yaml:"memory_type"       json:"memory_type"`
	Description      string        `yaml:"description"       json:"description"`
	Fields           []MemoryField `yaml:"fields"            json:"fields"`
	FilenameTemplate string        `yaml:"filename_template" json:"filename_template"`
	ContentTemplate  string        `yaml:"content_template"  json:"content_template,omitempty"`
	Directory        string        `yaml:"directory"         json:"directory"`
	Enabled          bool          `yaml:"enabled"           json:"enabled"`
	OperationMode    string        `yaml:"operation_mode"    json:"operation_mode"`
}

// MemoryTypeRegistry holds all loaded memory type schemas.
type MemoryTypeRegistry struct {
	schemas map[string]*MemoryTypeSchema
}

// NewMemoryTypeRegistry creates an empty registry.
func NewMemoryTypeRegistry() *MemoryTypeRegistry {
	return &MemoryTypeRegistry{schemas: make(map[string]*MemoryTypeSchema)}
}

// Register adds a schema to the registry.
func (r *MemoryTypeRegistry) Register(schema *MemoryTypeSchema) {
	r.schemas[schema.MemoryType] = schema
}

// Get retrieves a schema by memory type name.
func (r *MemoryTypeRegistry) Get(memoryType string) *MemoryTypeSchema {
	return r.schemas[memoryType]
}

// All returns all registered schemas.
func (r *MemoryTypeRegistry) All() []*MemoryTypeSchema {
	result := make([]*MemoryTypeSchema, 0, len(r.schemas))
	for _, s := range r.schemas {
		if s.Enabled {
			result = append(result, s)
		}
	}
	return result
}

// FieldSchemaMap returns a map of field name to MemoryField for a given memory type.
func (r *MemoryTypeRegistry) FieldSchemaMap(memoryType string) map[string]*MemoryField {
	schema := r.Get(memoryType)
	if schema == nil {
		return nil
	}
	m := make(map[string]*MemoryField, len(schema.Fields))
	for i := range schema.Fields {
		m[schema.Fields[i].Name] = &schema.Fields[i]
	}
	return m
}

// LoadFromDir loads all .yaml and .yml files from the given directory into the registry.
func (r *MemoryTypeRegistry) LoadFromDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read schema dir %s: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read schema %s: %w", path, err)
		}
		var schema MemoryTypeSchema
		if err := yaml.Unmarshal(data, &schema); err != nil {
			return fmt.Errorf("parse schema %s: %w", path, err)
		}
		if schema.MemoryType == "" {
			schema.MemoryType = strings.TrimSuffix(name, filepath.Ext(name))
		}
		if schema.OperationMode == "" {
			schema.OperationMode = "upsert"
		}
		r.Register(&schema)
	}
	return nil
}

// LoadFromYAML parses a single YAML schema and registers it.
func (r *MemoryTypeRegistry) LoadFromYAML(data []byte) error {
	var schema MemoryTypeSchema
	if err := yaml.Unmarshal(data, &schema); err != nil {
		return err
	}
	if schema.OperationMode == "" {
		schema.OperationMode = "upsert"
	}
	r.Register(&schema)
	return nil
}
