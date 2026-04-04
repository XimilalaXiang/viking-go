package watch

import (
	"testing"
)

func TestDetectSourceType(t *testing.T) {
	tests := []struct {
		input string
		want  SourceType
	}{
		{"/home/user/docs", SourceLocal},
		{"./relative/path", SourceLocal},
		{"https://example.com/page.html", SourceURL},
		{"http://example.com/data.json", SourceURL},
		{"https://github.com/org/repo", SourceGit},
		{"https://gitlab.com/org/project", SourceGit},
		{"git@github.com:org/repo.git", SourceGit},
		{"https://example.com/random/path", SourceURL},
	}

	for _, tt := range tests {
		got := DetectSourceType(tt.input)
		if got != tt.want {
			t.Errorf("DetectSourceType(%q) = %s, want %s", tt.input, got, tt.want)
		}
	}
}

func TestIsGitRepoURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://github.com/org/repo", true},
		{"https://github.com/org/repo.git", true},
		{"https://gitlab.com/org/project", true},
		{"git@github.com:org/repo.git", true},
		{"git@gitlab.com:group/project.git", true},
		{"https://example.com/path", false},
		{"https://github.com/org/repo/issues/123", false},
		{"not-a-url", false},
		{"ftp://github.com/org/repo", false},
	}

	for _, tt := range tests {
		got := IsGitRepoURL(tt.input)
		if got != tt.want {
			t.Errorf("IsGitRepoURL(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseGitRepoPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://github.com/volcengine/OpenViking", "volcengine/OpenViking"},
		{"https://github.com/volcengine/OpenViking.git", "volcengine/OpenViking"},
		{"git@github.com:volcengine/OpenViking.git", "volcengine/OpenViking"},
		{"https://gitlab.com/group/project", "group/project"},
	}

	for _, tt := range tests {
		got := ParseGitRepoPath(tt.input)
		if got != tt.want {
			t.Errorf("ParseGitRepoPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsURL(t *testing.T) {
	if !IsURL("https://example.com") {
		t.Error("https should be URL")
	}
	if !IsURL("http://example.com") {
		t.Error("http should be URL")
	}
	if IsURL("/local/path") {
		t.Error("local path should not be URL")
	}
	if IsURL("git@github.com:org/repo.git") {
		t.Error("git SSH should not be URL")
	}
}

func TestFilenameFromURL(t *testing.T) {
	tests := []struct {
		rawURL      string
		contentType string
		wantSuffix  string
	}{
		{"https://example.com/doc.pdf", "application/pdf", ".pdf"},
		{"https://example.com/page", "text/html", ".html"},
		{"https://example.com/data", "application/json", ".json"},
	}

	for _, tt := range tests {
		got := filenameFromURL(tt.rawURL, tt.contentType)
		if got == "" {
			t.Errorf("filenameFromURL(%q) returned empty", tt.rawURL)
		}
	}
}
