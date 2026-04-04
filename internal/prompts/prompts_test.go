package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewManager(t *testing.T) {
	m := NewManager()
	ids := m.List()
	if len(ids) == 0 {
		t.Error("expected templates to be registered")
	}
	t.Logf("loaded %d templates: %v", len(ids), ids)
}

func TestYAMLTemplatesLoaded(t *testing.T) {
	m := NewManager()
	expected := []string{
		"compression.memory_extraction",
		"compression.dedup_decision",
		"compression.memory_merge_bundle",
		"compression.field_compress",
		"compression.structured_summary",
		"retrieval.intent_analysis",
		"semantic.generate_abstract",
		"semantic.generate_overview",
		"semantic.code_ast_summary",
		"semantic.document_summary",
		"semantic.file_summary",
		"semantic.code_summary",
		"processing.strategy_extraction",
		"processing.interaction_learning",
		"processing.tool_chain_analysis",
		"parsing.semantic_grouping",
		"parsing.context_generation",
		"indexing.relevance_scoring",
		"skill.overview_generation",
	}
	ids := m.List()
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	for _, e := range expected {
		if !idSet[e] {
			t.Errorf("expected template %q to be loaded", e)
		}
	}
}

func TestRender(t *testing.T) {
	m := NewManager()
	result, err := m.Render("compression.memory_extraction", map[string]any{
		"summary":         "User discussed Go programming",
		"recent_messages": "[user]: How do I write tests in Go?",
		"user":            "alice",
		"feedback":        "",
		"output_language": "en",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(result, "alice") {
		t.Error("rendered prompt missing user variable")
	}
	if !strings.Contains(result, "Memory Classification") {
		t.Error("rendered prompt missing expected heading")
	}
}

func TestRenderMissing(t *testing.T) {
	m := NewManager()
	_, err := m.Render("nonexistent", nil)
	if err == nil {
		t.Error("expected error for missing template")
	}
}

func TestRegisterAndOverride(t *testing.T) {
	m := NewManager()
	m.Register("custom.test", "Hello {{.name}}!")
	result, err := m.Render("custom.test", map[string]any{"name": "world"})
	if err != nil {
		t.Fatal(err)
	}
	if result != "Hello world!" {
		t.Errorf("got %q", result)
	}
}

func TestDefaultsApplied(t *testing.T) {
	m := NewManager()
	result, err := m.Render("compression.memory_extraction", map[string]any{
		"recent_messages": "hello",
		"user":            "bob",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(result, "auto") {
		t.Error("expected default output_language 'auto' to appear")
	}
}

func TestGetLLMConfig(t *testing.T) {
	m := NewManager()
	cfg := m.GetLLMConfig("compression.memory_extraction")
	if cfg == nil {
		t.Fatal("expected LLM config")
	}
	if cfg.Temperature != 0.0 {
		t.Errorf("expected temperature 0.0, got %f", cfg.Temperature)
	}
}

func TestGetTemplate(t *testing.T) {
	m := NewManager()
	def := m.GetTemplate("compression.memory_extraction")
	if def == nil {
		t.Fatal("expected template definition")
	}
	if def.Metadata.Version == "" {
		t.Error("expected version to be set")
	}
	if len(def.Variables) == 0 {
		t.Error("expected variables to be defined")
	}
}

func TestListByCategory(t *testing.T) {
	m := NewManager()
	compressionIDs := m.ListByCategory("compression")
	if len(compressionIDs) < 4 {
		t.Errorf("expected at least 4 compression templates, got %d", len(compressionIDs))
	}
	for _, id := range compressionIDs {
		if !strings.HasPrefix(id, "compression.") {
			t.Errorf("expected compression prefix, got %q", id)
		}
	}
}

func TestAllTemplatesRender(t *testing.T) {
	m := NewManager()
	allVars := map[string]any{
		"summary": "", "recent_messages": "", "user": "", "feedback": "", "output_language": "",
		"existing_abstract": "", "existing_overview": "", "existing_content": "",
		"new_abstract": "", "new_overview": "", "new_content": "",
		"category": "", "candidate_abstract": "", "candidate_overview": "",
		"candidate_content": "", "existing_memories": "",
		"field_name": "", "content": "", "max_length": 100,
		"compression_summary": "", "current_message": "",
		"context_type": "", "target_abstract": "",
		"uri": "", "children": "", "abstract": "",
		"messages": "", "previous_summary": "",
		"reason": "", "instruction": "",
		"query": "", "chunk": "", "context": "",
		"file_name": "", "skeleton": "", "document_summary": "",
		"chunks": "", "tool_calls": "", "outcome": "",
		"agent_id": "", "skill_name": "", "files": "",
	}
	for _, id := range m.List() {
		_, err := m.Render(id, allVars)
		if err != nil {
			t.Errorf("template %q failed to render: %v", id, err)
		}
	}
}

func TestOverrideDirectory(t *testing.T) {
	dir := t.TempDir()
	catDir := filepath.Join(dir, "custom")
	os.MkdirAll(catDir, 0755)

	yamlContent := `metadata:
  id: "custom.greeting"
  name: "Greeting"
  description: "A test template"
  version: "1.0.0"
  language: "en"
  category: "custom"
variables:
  - name: "name"
    type: "string"
    required: true
template: "Hello, {{.name}}!"
`
	os.WriteFile(filepath.Join(catDir, "greeting.yaml"), []byte(yamlContent), 0644)

	m := NewManagerWithOverrides(dir)
	result, err := m.Render("custom.greeting", map[string]any{"name": "Viking"})
	if err != nil {
		t.Fatal(err)
	}
	if result != "Hello, Viking!" {
		t.Errorf("got %q", result)
	}
}

func TestMaxLengthTruncation(t *testing.T) {
	m := NewManager()
	longContent := strings.Repeat("x", 20000)
	result, err := m.Render("semantic.document_summary", map[string]any{
		"file_name":       "test.md",
		"content":         longContent,
		"output_language": "en",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result, strings.Repeat("x", 9000)) {
		t.Error("expected content to be truncated")
	}
	_ = result
}

func TestMemoryTemplatesNoMetadata(t *testing.T) {
	m := NewManager()
	ids := m.List()
	memoryFound := false
	for _, id := range ids {
		if strings.HasPrefix(id, "memory.") {
			memoryFound = true
		}
	}
	if !memoryFound {
		t.Error("expected memory.* templates to be loaded")
	}
}
