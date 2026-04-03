package intent

import (
	"testing"
)

func TestTruncateText(t *testing.T) {
	short := "hello"
	if got := truncateText(short, 100); got != short {
		t.Errorf("short text changed: %q", got)
	}

	long := make([]byte, 200)
	for i := range long {
		long[i] = 'a'
	}
	got := truncateText(string(long), 50)
	if len(got) > 50 {
		t.Errorf("truncated length = %d, want <= 50", len(got))
	}
}

func TestParseQueries(t *testing.T) {
	data := map[string]any{
		"queries": []any{
			map[string]any{
				"query":        "user preferences for Go",
				"context_type": "memory",
				"intent":       "find user preferences",
				"priority":     float64(1),
			},
			map[string]any{
				"query":        "project documentation",
				"context_type": "resource",
				"priority":     float64(3),
			},
		},
		"reasoning": "User needs preferences and docs",
	}

	queries := parseQueries(data)
	if len(queries) != 2 {
		t.Fatalf("got %d queries, want 2", len(queries))
	}

	if queries[0].Query != "user preferences for Go" {
		t.Errorf("queries[0].Query = %q", queries[0].Query)
	}
	if queries[0].ContextType != "memory" {
		t.Errorf("queries[0].ContextType = %q", queries[0].ContextType)
	}
	if queries[0].Priority != 1 {
		t.Errorf("queries[0].Priority = %d", queries[0].Priority)
	}
	if queries[1].Priority != 3 {
		t.Errorf("queries[1].Priority = %d", queries[1].Priority)
	}
}

func TestParseQueriesEmpty(t *testing.T) {
	data := map[string]any{}
	queries := parseQueries(data)
	if len(queries) != 0 {
		t.Errorf("got %d queries for empty data", len(queries))
	}
}

func TestParseJSONFromResp(t *testing.T) {
	input := `{"queries": [{"query": "test", "context_type": "memory"}]}`
	data, err := parseJSONFromResp(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := data["queries"]; !ok {
		t.Error("missing queries key")
	}
}

func TestSummarizeContext(t *testing.T) {
	a := &Analyzer{}

	got := a.summarizeContext("", "")
	if got != "No context" {
		t.Errorf("empty context = %q", got)
	}

	got = a.summarizeContext("session info", "hello")
	if got == "No context" {
		t.Error("expected non-empty context")
	}
}
