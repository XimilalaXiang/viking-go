package prompts

import (
	"bytes"
	"fmt"
	"sync"
	"text/template"
)

// Manager manages prompt templates with variable interpolation and caching.
type Manager struct {
	templates map[string]string
	cache     map[string]*template.Template
	mu        sync.RWMutex
}

// NewManager creates a prompt manager with built-in templates.
func NewManager() *Manager {
	m := &Manager{
		templates: make(map[string]string),
		cache:     make(map[string]*template.Template),
	}
	m.registerDefaults()
	return m
}

// Register adds or overrides a prompt template.
func (m *Manager) Register(id, tmpl string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.templates[id] = tmpl
	delete(m.cache, id)
}

// Render renders a prompt template with the given variables.
func (m *Manager) Render(id string, vars map[string]any) (string, error) {
	m.mu.RLock()
	tmpl, cached := m.cache[id]
	m.mu.RUnlock()

	if !cached {
		m.mu.Lock()
		raw, ok := m.templates[id]
		if !ok {
			m.mu.Unlock()
			return "", fmt.Errorf("prompt template %q not found", id)
		}
		var err error
		tmpl, err = template.New(id).Parse(raw)
		if err != nil {
			m.mu.Unlock()
			return "", fmt.Errorf("parse template %q: %w", id, err)
		}
		m.cache[id] = tmpl
		m.mu.Unlock()
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("execute template %q: %w", id, err)
	}
	return buf.String(), nil
}

// List returns all registered template IDs.
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.templates))
	for id := range m.templates {
		ids = append(ids, id)
	}
	return ids
}

func (m *Manager) registerDefaults() {
	m.templates["compression.memory_extraction"] = memoryExtractionPrompt
	m.templates["compression.memory_merge_bundle"] = memoryMergeBundlePrompt
	m.templates["compression.dedup_decision"] = dedupDecisionPrompt
	m.templates["compression.field_compress"] = fieldCompressPrompt
	m.templates["retrieval.intent_analysis"] = intentAnalysisPrompt
	m.templates["semantic.generate_abstract"] = generateAbstractPrompt
	m.templates["semantic.generate_overview"] = generateOverviewPrompt
}

const memoryExtractionPrompt = `You are a memory extraction engine. Analyze the conversation and extract important memories.

Session Summary: {{.summary}}

Recent Messages:
{{.recent_messages}}

User: {{.user}}
{{if .feedback}}Feedback: {{.feedback}}{{end}}

Output language: {{.output_language}}

Extract memories in these categories:
- profile: User personal information
- preferences: User preferences (tools, styles, languages)
- entities: Important entities (projects, people, concepts)
- events: Significant events (decisions, milestones)
- cases: Problem-solution cases
- patterns: Reusable processes/methods

Return JSON:
{"memories": [{"category": "...", "abstract": "one-line summary", "overview": "medium detail", "content": "full narrative"}]}

Only extract genuinely important, reusable information. Return empty memories array if nothing worth remembering.`

const memoryMergeBundlePrompt = `Merge these two memory entries into one cohesive entry.

Existing:
- Abstract: {{.existing_abstract}}
- Overview: {{.existing_overview}}
- Content: {{.existing_content}}

New:
- Abstract: {{.new_abstract}}
- Overview: {{.new_overview}}
- Content: {{.new_content}}

Category: {{.category}}
Output language: {{.output_language}}

Return JSON: {"decision": "merge", "abstract": "...", "overview": "...", "content": "...", "reason": "..."}`

const dedupDecisionPrompt = `You are a memory deduplication engine. Decide how to handle this new memory candidate.

New Candidate:
- Abstract: {{.candidate_abstract}}
- Overview: {{.candidate_overview}}
- Content: {{.candidate_content}}

Existing Similar Memories:
{{.existing_memories}}

Decisions:
- "skip": The candidate is redundant, don't create it
- "create": The candidate adds new information, create it
- "none": Don't create candidate, but take actions on existing memories

For each existing memory, you can specify actions:
- "merge": Merge candidate info into this existing memory
- "delete": Delete this outdated/conflicting memory

Return JSON:
{"decision": "skip|create|none", "reason": "...", "list": [{"uri": "...", "decide": "merge|delete", "reason": "..."}]}`

const fieldCompressPrompt = `Compress the following field content to fit within {{.max_length}} characters while preserving key information.

Field: {{.field_name}}
Content: {{.content}}

Return only the compressed text, no JSON wrapper.`

const intentAnalysisPrompt = `You are a retrieval intent analyzer. Analyze the session context and generate search queries.

Session Summary:
{{.compression_summary}}

Recent Messages:
{{.recent_messages}}

Current Message: {{.current_message}}

{{if .context_type}}Context Type Constraint: {{.context_type}}{{end}}
{{if .target_abstract}}Target Abstract: {{.target_abstract}}{{end}}

Generate queries for these context types:
- "memory": User memories, preferences, profile
- "skill": Agent skills, tools, patterns
- "resource": Files, documents, data

Return JSON:
{"queries": [{"query": "search text", "context_type": "memory|skill|resource", "intent": "why this query", "priority": 1-5}], "reasoning": "analysis reasoning"}

Rules:
- Generate 1-5 queries covering different aspects
- Higher priority = more important (1=highest)
- If context_type constraint is given, only generate that type`

const generateAbstractPrompt = `Generate a one-line abstract (L0 summary) for this directory based on its contents.

Directory URI: {{.uri}}
Children files:
{{.children}}

Return only the abstract text, no JSON.`

const generateOverviewPrompt = `Generate a medium-detail overview (L1) for this directory based on its contents.

Directory URI: {{.uri}}
Abstract: {{.abstract}}
Children files:
{{.children}}

Return only the overview text (2-5 sentences), no JSON.`
