// Package skill provides SKILL.md parsing and MCP-to-Skill conversion.
// A Skill defines a reusable capability with triggers, instructions,
// and optional MCP tool bindings.
package skill

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill represents a parsed skill definition.
type Skill struct {
	Name        string            `json:"name" yaml:"name"`
	Description string            `json:"description" yaml:"description"`
	Version     string            `json:"version,omitempty" yaml:"version"`
	Author      string            `json:"author,omitempty" yaml:"author"`
	Triggers    []string          `json:"triggers,omitempty" yaml:"triggers"`
	Tags        []string          `json:"tags,omitempty" yaml:"tags"`
	Tools       []ToolBinding     `json:"tools,omitempty" yaml:"tools"`
	Sections    map[string]string `json:"sections,omitempty"`
	RawBody     string            `json:"raw_body,omitempty"`
	FilePath    string            `json:"file_path,omitempty"`
}

// ToolBinding links a skill to an MCP tool.
type ToolBinding struct {
	Name        string         `json:"name" yaml:"name"`
	Server      string         `json:"server,omitempty" yaml:"server"`
	Description string         `json:"description,omitempty" yaml:"description"`
	Parameters  map[string]any `json:"parameters,omitempty" yaml:"parameters"`
}

// LoadSkillMD parses a SKILL.md file. The file uses YAML frontmatter
// (delimited by ---) followed by a Markdown body.
func LoadSkillMD(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read skill: %w", err)
	}

	skill, err := ParseSkillMD(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	skill.FilePath = path
	if skill.Name == "" {
		skill.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return skill, nil
}

// ParseSkillMD parses SKILL.md content from a string.
func ParseSkillMD(content string) (*Skill, error) {
	frontmatter, body := splitFrontmatter(content)

	var skill Skill
	if frontmatter != "" {
		if err := yaml.Unmarshal([]byte(frontmatter), &skill); err != nil {
			return nil, fmt.Errorf("parse frontmatter: %w", err)
		}
	}

	skill.RawBody = body
	skill.Sections = parseSections(body)

	if desc, ok := skill.Sections["description"]; ok && skill.Description == "" {
		skill.Description = desc
	}

	return &skill, nil
}

// LoadSkillDir loads all SKILL.md files from a directory (non-recursive).
func LoadSkillDir(dir string) ([]*Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read skill dir: %w", err)
	}

	var skills []*Skill
	for _, entry := range entries {
		if entry.IsDir() {
			skillPath := filepath.Join(dir, entry.Name(), "SKILL.md")
			if _, err := os.Stat(skillPath); err == nil {
				s, err := LoadSkillMD(skillPath)
				if err != nil {
					continue
				}
				skills = append(skills, s)
			}
			continue
		}
		name := strings.ToUpper(entry.Name())
		if name == "SKILL.MD" {
			s, err := LoadSkillMD(filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}
			skills = append(skills, s)
		}
	}
	return skills, nil
}

// MCPToolToSkill converts an MCP tool definition into a Skill.
func MCPToolToSkill(name, description string, params map[string]any, server string) *Skill {
	return &Skill{
		Name:        name,
		Description: description,
		Tools: []ToolBinding{
			{
				Name:        name,
				Server:      server,
				Description: description,
				Parameters:  params,
			},
		},
		Tags: []string{"mcp", "auto-generated"},
	}
}

// MCPToolsToSkills converts multiple MCP tools from a server into skills.
func MCPToolsToSkills(tools []MCPToolDef, server string) []*Skill {
	skills := make([]*Skill, 0, len(tools))
	for _, t := range tools {
		skills = append(skills, MCPToolToSkill(t.Name, t.Description, t.Parameters, server))
	}
	return skills
}

// MCPToolDef represents an MCP tool definition for conversion.
type MCPToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ToMarkdown serializes a Skill back to SKILL.md format.
func (s *Skill) ToMarkdown() string {
	var sb strings.Builder

	fm := struct {
		Name        string        `yaml:"name,omitempty"`
		Description string        `yaml:"description,omitempty"`
		Version     string        `yaml:"version,omitempty"`
		Author      string        `yaml:"author,omitempty"`
		Triggers    []string      `yaml:"triggers,omitempty"`
		Tags        []string      `yaml:"tags,omitempty"`
		Tools       []ToolBinding `yaml:"tools,omitempty"`
	}{
		Name: s.Name, Description: s.Description,
		Version: s.Version, Author: s.Author,
		Triggers: s.Triggers, Tags: s.Tags, Tools: s.Tools,
	}

	fmBytes, err := yaml.Marshal(fm)
	if err == nil && len(fmBytes) > 0 {
		sb.WriteString("---\n")
		sb.Write(fmBytes)
		sb.WriteString("---\n\n")
	}

	if s.RawBody != "" {
		sb.WriteString(s.RawBody)
	} else {
		sb.WriteString("# " + s.Name + "\n\n")
		sb.WriteString(s.Description + "\n")
	}

	return sb.String()
}

// --- internal helpers ---

func splitFrontmatter(content string) (frontmatter, body string) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return "", content
	}

	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", content
	}

	frontmatter = strings.TrimSpace(rest[:idx])
	body = strings.TrimSpace(rest[idx+4:])
	return frontmatter, body
}

func parseSections(body string) map[string]string {
	sections := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(body))

	var currentSection string
	var currentContent strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") {
			if currentSection != "" {
				sections[currentSection] = strings.TrimSpace(currentContent.String())
			}
			heading := strings.TrimLeft(line, "# ")
			currentSection = strings.ToLower(strings.TrimSpace(heading))
			currentContent.Reset()
		} else if currentSection != "" {
			currentContent.WriteString(line)
			currentContent.WriteString("\n")
		}
	}

	if currentSection != "" {
		sections[currentSection] = strings.TrimSpace(currentContent.String())
	}

	return sections
}
