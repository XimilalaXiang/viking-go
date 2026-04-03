package intent

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/ximilala/viking-go/internal/llm"
	"github.com/ximilala/viking-go/internal/retriever"
)

const maxCompressionChars = 30000

// QueryPlan is the result of intent analysis containing typed queries.
type QueryPlan struct {
	Queries        []retriever.TypedQuery `json:"queries"`
	SessionContext string                 `json:"session_context"`
	Reasoning      string                 `json:"reasoning"`
}

// Analyzer generates query plans from session context via LLM.
type Analyzer struct {
	llm               llm.Client
	maxRecentMessages int
}

// NewAnalyzer creates a new intent analyzer.
func NewAnalyzer(llmClient llm.Client, maxRecentMessages int) *Analyzer {
	if maxRecentMessages <= 0 {
		maxRecentMessages = 5
	}
	return &Analyzer{llm: llmClient, maxRecentMessages: maxRecentMessages}
}

// Message is a minimal chat message for analysis.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Analyze processes session context and generates a query plan.
func (a *Analyzer) Analyze(
	compressionSummary string,
	messages []Message,
	currentMessage string,
	contextType string,
	targetAbstract string,
) (*QueryPlan, error) {
	if a.llm == nil {
		return nil, fmt.Errorf("LLM not available")
	}

	prompt := a.buildPrompt(compressionSummary, messages, currentMessage, contextType, targetAbstract)

	resp, err := a.llm.CompleteWithPrompt(prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM intent analysis: %w", err)
	}

	data, err := parseJSONFromResp(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("parse intent analysis: %w", err)
	}

	queries := parseQueries(data)

	for i, q := range queries {
		log.Printf("  [%d] type=%s, priority=%d, query=%q", i+1, q.ContextType, q.Priority, q.Query)
	}

	reasoning := ""
	if r, ok := data["reasoning"].(string); ok {
		reasoning = r
	}

	return &QueryPlan{
		Queries:        queries,
		SessionContext: a.summarizeContext(compressionSummary, currentMessage),
		Reasoning:      reasoning,
	}, nil
}

func (a *Analyzer) buildPrompt(
	summary string,
	messages []Message,
	currentMessage string,
	contextType string,
	targetAbstract string,
) string {
	truncSummary := truncateText(summary, maxCompressionChars)
	if truncSummary == "" {
		truncSummary = "None"
	}

	start := 0
	if len(messages) > a.maxRecentMessages {
		start = len(messages) - a.maxRecentMessages
	}
	recent := messages[start:]
	var recentLines []string
	for _, m := range recent {
		if m.Content != "" {
			recentLines = append(recentLines, fmt.Sprintf("[%s]: %s", m.Role, m.Content))
		}
	}
	recentStr := "None"
	if len(recentLines) > 0 {
		recentStr = strings.Join(recentLines, "\n")
	}

	current := currentMessage
	if current == "" {
		current = "None"
	}

	return fmt.Sprintf(`You are a retrieval intent analyzer. Analyze the session context and generate search queries.

Session Summary:
%s

Recent Messages:
%s

Current Message: %s

Context Type Constraint: %s
Target Abstract: %s

Generate queries for these context types:
- "memory": User memories, preferences, profile
- "skill": Agent skills, tools, patterns
- "resource": Files, documents, data

Return JSON:
{"queries": [{"query": "search text", "context_type": "memory|skill|resource", "intent": "why this query", "priority": 1-5}], "reasoning": "analysis reasoning"}

Rules:
- Generate 1-5 queries covering different aspects
- Higher priority = more important (1=highest)
- If context_type constraint is given, only generate that type
- Focus on what information would be most helpful`, truncSummary, recentStr, current, contextType, targetAbstract)
}

func (a *Analyzer) summarizeContext(summary, currentMessage string) string {
	var parts []string
	if summary != "" {
		parts = append(parts, "Session summary: "+summary)
	}
	if currentMessage != "" {
		cm := currentMessage
		if len(cm) > 100 {
			cm = cm[:100]
		}
		parts = append(parts, "Current message: "+cm)
	}
	if len(parts) == 0 {
		return "No context"
	}
	return strings.Join(parts, " | ")
}

func parseQueries(data map[string]any) []retriever.TypedQuery {
	queriesRaw, ok := data["queries"]
	if !ok {
		return nil
	}
	queries, ok := queriesRaw.([]any)
	if !ok {
		return nil
	}

	var result []retriever.TypedQuery
	for _, qRaw := range queries {
		q, ok := qRaw.(map[string]any)
		if !ok {
			continue
		}

		query := getStr(q, "query")
		if query == "" {
			continue
		}

		ctStr := getStr(q, "context_type")
		if ctStr == "" {
			ctStr = "resource"
		}

		priority := 3
		if p, ok := q["priority"].(float64); ok {
			priority = int(p)
			if priority < 1 {
				priority = 1
			}
			if priority > 5 {
				priority = 5
			}
		}

		result = append(result, retriever.TypedQuery{
			Query:       query,
			ContextType: ctStr,
			Intent:      getStr(q, "intent"),
			Priority:    priority,
		})
	}
	return result
}

func truncateText(text string, maxChars int) string {
	if text == "" || len(text) <= maxChars {
		return text
	}
	return text[:maxChars-15] + "\n...(truncated)"
}

func getStr(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func parseJSONFromResp(text string) (map[string]any, error) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) > 2 {
			lines = lines[1:]
			for i := len(lines) - 1; i >= 0; i-- {
				if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
					lines = lines[:i]
					break
				}
			}
			text = strings.Join(lines, "\n")
		}
	}
	start := strings.Index(text, "{")
	if start < 0 {
		return nil, fmt.Errorf("no JSON object found")
	}
	depth := 0
	end := -1
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i + 1
			}
		}
		if end > 0 {
			break
		}
	}
	if end < 0 {
		return nil, fmt.Errorf("unmatched JSON braces")
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(text[start:end]), &result); err != nil {
		return nil, err
	}
	return result, nil
}
