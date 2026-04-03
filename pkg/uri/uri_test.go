package uri

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"viking://user/memories", "viking://user/memories"},
		{"viking:/user/memories", "viking://user/memories"},
		{"user/memories", "viking://user/memories"},
		{"", "viking://"},
		{"viking://", "viking://"},
	}
	for _, tc := range cases {
		got := Normalize(tc.input)
		if got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestParse(t *testing.T) {
	vu, err := Parse("viking://user/memories/hello")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if vu.Scope != "user" {
		t.Errorf("Scope = %q, want user", vu.Scope)
	}
	if len(vu.Parts) != 3 {
		t.Errorf("Parts = %v, want 3 parts", vu.Parts)
	}
	if vu.Parent == nil {
		t.Fatal("Parent should not be nil")
	}
	if vu.Parent.URI() != "viking://user/memories" {
		t.Errorf("Parent URI = %q", vu.Parent.URI())
	}
}

func TestParseUnsafe(t *testing.T) {
	_, err := Parse("viking://user/../etc/passwd")
	if err == nil {
		t.Error("expected error for traversal segment")
	}
	_, err = Parse("viking://user/foo\\bar")
	if err == nil {
		t.Error("expected error for backslash")
	}
}

func TestParseRoot(t *testing.T) {
	vu, err := Parse("viking://")
	if err != nil {
		t.Fatalf("Parse root: %v", err)
	}
	if vu.Scope != "" {
		t.Errorf("Scope = %q, want empty", vu.Scope)
	}
	if len(vu.Parts) != 0 {
		t.Errorf("Parts = %v, want empty", vu.Parts)
	}
}

func TestExtractSpace(t *testing.T) {
	cases := []struct {
		uri  string
		want string
	}{
		{"viking://user/myspace/memories/test", "myspace"},
		{"viking://user/memories/test", ""},
		{"viking://agent/agentspace/skills/test", "agentspace"},
		{"viking://agent/skills/test", ""},
		{"viking://session/sess123/data", "sess123"},
		{"viking://resources/doc", ""},
		{"viking://user/.abstract.md", ""},
	}
	for _, tc := range cases {
		vu, err := Parse(tc.uri)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.uri, err)
			continue
		}
		got := vu.ExtractSpace()
		if got != tc.want {
			t.Errorf("ExtractSpace(%q) = %q, want %q", tc.uri, got, tc.want)
		}
	}
}

func TestInferContextType(t *testing.T) {
	cases := []struct {
		uri  string
		want string
	}{
		{"viking://user/space/memories/hello", "memory"},
		{"viking://agent/space/skills/greet", "skill"},
		{"viking://resources/doc", "resource"},
		{"viking://temp/abc123", ""},
	}
	for _, tc := range cases {
		vu, _ := Parse(tc.uri)
		got := vu.InferContextType()
		if got != tc.want {
			t.Errorf("InferContextType(%q) = %q, want %q", tc.uri, got, tc.want)
		}
	}
}

func TestCreateTempURI(t *testing.T) {
	uri := CreateTempURI()
	if !strings.HasPrefix(uri, "viking://temp/") {
		t.Errorf("CreateTempURI() = %q, want prefix viking://temp/", uri)
	}
}
