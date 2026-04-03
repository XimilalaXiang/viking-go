package memory

import (
	"testing"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name     string
		messages []SessionMessage
		want     string
	}{
		{
			name:     "empty",
			messages: nil,
			want:     "en",
		},
		{
			name: "english",
			messages: []SessionMessage{
				{Role: "user", Content: "Hello, how are you?"},
			},
			want: "en",
		},
		{
			name: "chinese",
			messages: []SessionMessage{
				{Role: "user", Content: "你好，请帮我看看这个问题"},
			},
			want: "zh-CN",
		},
		{
			name: "japanese",
			messages: []SessionMessage{
				{Role: "user", Content: "こんにちは、問題を確認してください"},
			},
			want: "ja",
		},
		{
			name: "only user messages",
			messages: []SessionMessage{
				{Role: "assistant", Content: "你好"},
				{Role: "user", Content: "Hello there"},
			},
			want: "en",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectLanguage(tt.messages)
			if got != tt.want {
				t.Errorf("detectLanguage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseJSONFromResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantKey string
		wantErr bool
	}{
		{
			name:    "plain json",
			input:   `{"decision": "create"}`,
			wantKey: "decision",
		},
		{
			name:    "wrapped in markdown",
			input:   "```json\n{\"decision\": \"skip\"}\n```",
			wantKey: "decision",
		},
		{
			name:    "with prefix text",
			input:   "Here's the result: {\"memories\": []}",
			wantKey: "memories",
		},
		{
			name:    "no json",
			input:   "no json here",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseJSONFromResponse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if _, ok := result[tt.wantKey]; !ok {
				t.Errorf("missing key %q in result", tt.wantKey)
			}
		})
	}
}

func TestParseCandidates(t *testing.T) {
	data := map[string]any{
		"memories": []any{
			map[string]any{
				"category": "profile",
				"abstract": "User is a Go developer",
				"overview": "Detailed profile",
				"content":  "Full content about user preferences",
			},
			map[string]any{
				"category": "unknown_cat",
				"abstract": "Some pattern",
				"content":  "Pattern content",
			},
		},
	}

	candidates := parseCandidates(data, "session-1", "alice", "en")
	if len(candidates) != 2 {
		t.Fatalf("got %d candidates, want 2", len(candidates))
	}

	if candidates[0].Category != CatProfile {
		t.Errorf("candidates[0].Category = %q, want profile", candidates[0].Category)
	}
	if candidates[1].Category != CatPatterns {
		t.Errorf("candidates[1].Category = %q, want patterns (fallback)", candidates[1].Category)
	}
}

func TestExtractFacetKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"preferred language: Go", "preferred language"},
		{"主题：Go开发", "主题"},
		{"Short", "short"},
	}

	for _, tt := range tests {
		got := ExtractFacetKey(tt.input)
		if got != tt.want {
			t.Errorf("ExtractFacetKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGetOwnerSpace(t *testing.T) {
	// Verify user vs agent category routing
	// Note: using default context would require identity setup; just test the logic
	if !userCategories[CatProfile] {
		t.Error("profile should be a user category")
	}
	if !userCategories[CatPreferences] {
		t.Error("preferences should be a user category")
	}
	if userCategories[CatCases] {
		t.Error("cases should not be a user category")
	}
	if userCategories[CatPatterns] {
		t.Error("patterns should not be a user category")
	}
}
