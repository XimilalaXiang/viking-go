package prompts

import (
	"strings"
	"testing"
)

func TestNewManager(t *testing.T) {
	m := NewManager()
	ids := m.List()
	if len(ids) == 0 {
		t.Error("expected default templates to be registered")
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

	if !strings.Contains(result, "memory extraction engine") {
		t.Error("rendered prompt missing expected content")
	}
	if !strings.Contains(result, "alice") {
		t.Error("rendered prompt missing user variable")
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

func TestAllDefaultTemplatesRender(t *testing.T) {
	m := NewManager()
	for _, id := range m.List() {
		_, err := m.Render(id, map[string]any{
			"summary": "", "recent_messages": "", "user": "",
			"feedback": "", "output_language": "",
			"existing_abstract": "", "existing_overview": "", "existing_content": "",
			"new_abstract": "", "new_overview": "", "new_content": "",
			"category": "", "candidate_abstract": "", "candidate_overview": "",
			"candidate_content": "", "existing_memories": "",
			"field_name": "", "content": "", "max_length": 100,
			"compression_summary": "", "current_message": "",
			"context_type": "", "target_abstract": "",
			"uri": "", "children": "", "abstract": "",
		})
		if err != nil {
			t.Errorf("template %q failed to render: %v", id, err)
		}
	}
}
