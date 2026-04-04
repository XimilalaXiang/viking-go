package skill

import (
	"testing"
)

func TestParseSkillMD(t *testing.T) {
	content := `---
name: test-skill
description: A test skill
version: "1.0"
triggers:
  - "hello"
  - "test"
tags:
  - demo
---

# Test Skill

This is the body of the skill.

## Usage

Use it like this.
`

	s, err := ParseSkillMD(content)
	if err != nil {
		t.Fatalf("ParseSkillMD: %v", err)
	}

	if s.Name != "test-skill" {
		t.Errorf("Name = %q, want test-skill", s.Name)
	}
	if s.Description != "A test skill" {
		t.Errorf("Description = %q, want 'A test skill'", s.Description)
	}
	if len(s.Triggers) != 2 {
		t.Errorf("Triggers = %d, want 2", len(s.Triggers))
	}
	if s.Sections["usage"] == "" {
		t.Error("Expected 'usage' section")
	}
}

func TestParseSkillMDNoFrontmatter(t *testing.T) {
	content := `# Simple Skill

Just a body.
`
	s, err := ParseSkillMD(content)
	if err != nil {
		t.Fatalf("ParseSkillMD: %v", err)
	}
	if s.RawBody == "" {
		t.Error("Expected non-empty body")
	}
}

func TestMCPToolToSkill(t *testing.T) {
	s := MCPToolToSkill("search", "Search documents", map[string]any{
		"query": map[string]any{"type": "string"},
	}, "viking-go")

	if s.Name != "search" {
		t.Errorf("Name = %q, want search", s.Name)
	}
	if len(s.Tools) != 1 {
		t.Errorf("Tools = %d, want 1", len(s.Tools))
	}
	if s.Tools[0].Server != "viking-go" {
		t.Errorf("Server = %q, want viking-go", s.Tools[0].Server)
	}
}

func TestMCPToolsToSkills(t *testing.T) {
	tools := []MCPToolDef{
		{Name: "tool1", Description: "desc1"},
		{Name: "tool2", Description: "desc2"},
	}
	skills := MCPToolsToSkills(tools, "server1")
	if len(skills) != 2 {
		t.Errorf("got %d skills, want 2", len(skills))
	}
}

func TestToMarkdown(t *testing.T) {
	s := &Skill{
		Name:        "test",
		Description: "A skill",
		Version:     "1.0",
		Tags:        []string{"demo"},
	}
	md := s.ToMarkdown()
	if md == "" {
		t.Error("ToMarkdown returned empty")
	}
	if !contains(md, "name: test") {
		t.Error("Expected name in frontmatter")
	}
}

func TestSplitFrontmatter(t *testing.T) {
	fm, body := splitFrontmatter("---\nkey: val\n---\nbody text")
	if fm != "key: val" {
		t.Errorf("frontmatter = %q", fm)
	}
	if body != "body text" {
		t.Errorf("body = %q", body)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
