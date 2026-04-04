package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	"gopkg.in/yaml.v3"
)

//go:embed templates/*/*.yaml
var embeddedTemplates embed.FS

// PromptMetadata holds metadata about a prompt template.
type PromptMetadata struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Version     string `yaml:"version"`
	Language    string `yaml:"language"`
	Category    string `yaml:"category"`
}

// PromptVariable defines a template variable with validation rules.
type PromptVariable struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
	Default     any    `yaml:"default"`
	Required    bool   `yaml:"required"`
	MaxLength   int    `yaml:"max_length,omitempty"`
}

// LLMConfig holds LLM-specific configuration for a prompt.
type LLMConfig struct {
	Temperature float64 `yaml:"temperature"`
}

// PromptTemplate is the full YAML prompt definition.
type PromptTemplate struct {
	Metadata  PromptMetadata   `yaml:"metadata"`
	Variables []PromptVariable `yaml:"variables"`
	Template  string           `yaml:"template"`
	LLMConfig *LLMConfig       `yaml:"llm_config,omitempty"`
}

// Manager manages prompt templates with YAML loading, variable validation, and caching.
type Manager struct {
	rawTemplates map[string]string
	yamlDefs     map[string]*PromptTemplate
	cache        map[string]*template.Template
	mu           sync.RWMutex
	overrideDir  string
}

// NewManager creates a prompt manager with bundled YAML templates.
func NewManager() *Manager {
	m := &Manager{
		rawTemplates: make(map[string]string),
		yamlDefs:     make(map[string]*PromptTemplate),
		cache:        make(map[string]*template.Template),
	}
	m.loadEmbedded()
	return m
}

// NewManagerWithOverrides creates a manager that also loads templates from a directory.
// Templates in overrideDir take precedence over embedded ones.
func NewManagerWithOverrides(overrideDir string) *Manager {
	m := NewManager()
	m.overrideDir = overrideDir
	m.loadOverrides()
	return m
}

// Register adds or overrides a raw Go text/template string.
func (m *Manager) Register(id, tmpl string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rawTemplates[id] = tmpl
	delete(m.cache, id)
}

// Render renders a prompt template with the given variables.
// It applies defaults, validates required variables, and truncates to max_length.
func (m *Manager) Render(id string, vars map[string]any) (string, error) {
	m.mu.RLock()
	tmpl, cached := m.cache[id]
	m.mu.RUnlock()

	if !cached {
		m.mu.Lock()
		raw, yamlDef, found := m.lookupTemplate(id)
		if !found {
			m.mu.Unlock()
			return "", fmt.Errorf("prompt template %q not found", id)
		}

		if yamlDef != nil {
			vars = m.applyDefaults(yamlDef, vars)
		}

		var err error
		tmpl, err = template.New(id).Option("missingkey=zero").Parse(raw)
		if err != nil {
			m.mu.Unlock()
			return "", fmt.Errorf("parse template %q: %w", id, err)
		}
		m.cache[id] = tmpl
		m.mu.Unlock()
	} else {
		m.mu.RLock()
		if def, ok := m.yamlDefs[id]; ok {
			vars = m.applyDefaults(def, vars)
		}
		m.mu.RUnlock()
	}

	if def, ok := m.yamlDefs[id]; ok {
		if err := m.validateVars(def, vars); err != nil {
			return "", fmt.Errorf("validate template %q: %w", id, err)
		}
		vars = m.truncateVars(def, vars)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("execute template %q: %w", id, err)
	}
	return buf.String(), nil
}

// GetLLMConfig returns the LLM configuration for a prompt template.
func (m *Manager) GetLLMConfig(id string) *LLMConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if def, ok := m.yamlDefs[id]; ok {
		return def.LLMConfig
	}
	return nil
}

// GetTemplate returns the full PromptTemplate definition, or nil if not found.
func (m *Manager) GetTemplate(id string) *PromptTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.yamlDefs[id]
}

// List returns all registered template IDs.
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := make(map[string]bool)
	ids := make([]string, 0)
	for id := range m.yamlDefs {
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	for id := range m.rawTemplates {
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	return ids
}

// ListByCategory returns template IDs filtered by category.
func (m *Manager) ListByCategory(category string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var ids []string
	for id, def := range m.yamlDefs {
		if def.Metadata.Category == category {
			ids = append(ids, id)
		}
	}
	return ids
}

func (m *Manager) lookupTemplate(id string) (string, *PromptTemplate, bool) {
	if raw, ok := m.rawTemplates[id]; ok {
		return raw, nil, true
	}
	if def, ok := m.yamlDefs[id]; ok {
		return def.Template, def, true
	}
	return "", nil, false
}

func (m *Manager) applyDefaults(def *PromptTemplate, vars map[string]any) map[string]any {
	if vars == nil {
		vars = make(map[string]any)
	}
	result := make(map[string]any, len(vars))
	for k, v := range vars {
		result[k] = v
	}
	for _, v := range def.Variables {
		if _, exists := result[v.Name]; !exists && v.Default != nil {
			result[v.Name] = v.Default
		}
	}
	return result
}

func (m *Manager) validateVars(def *PromptTemplate, vars map[string]any) error {
	for _, v := range def.Variables {
		if v.Required {
			val, exists := vars[v.Name]
			if !exists || val == nil {
				return fmt.Errorf("required variable %q not provided", v.Name)
			}
		}
	}
	return nil
}

func (m *Manager) truncateVars(def *PromptTemplate, vars map[string]any) map[string]any {
	for _, v := range def.Variables {
		if v.MaxLength > 0 {
			if s, ok := vars[v.Name].(string); ok && len(s) > v.MaxLength {
				vars[v.Name] = s[:v.MaxLength]
			}
		}
	}
	return vars
}

func (m *Manager) loadEmbedded() {
	entries, err := embeddedTemplates.ReadDir("templates")
	if err != nil {
		return
	}
	for _, catDir := range entries {
		if !catDir.IsDir() {
			continue
		}
		category := catDir.Name()
		subEntries, err := embeddedTemplates.ReadDir("templates/" + category)
		if err != nil {
			continue
		}
		for _, f := range subEntries {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".yaml") {
				continue
			}
			path := "templates/" + category + "/" + f.Name()
			data, err := embeddedTemplates.ReadFile(path)
			if err != nil {
				continue
			}
			m.loadYAML(data, category, f.Name())
		}
	}
}

func (m *Manager) loadOverrides() {
	if m.overrideDir == "" {
		return
	}
	err := filepath.Walk(m.overrideDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(info.Name(), ".yaml") {
			return err
		}
		rel, _ := filepath.Rel(m.overrideDir, path)
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) < 2 {
			return nil
		}
		category := parts[0]
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		m.loadYAML(data, category, info.Name())
		return nil
	})
	_ = err
}

func (m *Manager) loadYAML(data []byte, category, fileName string) {
	var def PromptTemplate
	if err := yaml.Unmarshal(data, &def); err != nil {
		return
	}

	id := def.Metadata.ID
	if id == "" {
		stem := strings.TrimSuffix(fileName, ".yaml")
		id = category + "." + stem
	}

	m.yamlDefs[id] = &def
	delete(m.cache, id)
}
