package parse

import (
	"os"
	"path/filepath"
	"testing"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

func newTestVFS(t *testing.T) *vikingfs.VikingFS {
	t.Helper()
	dir := t.TempDir()
	vfs, err := vikingfs.New(dir)
	if err != nil {
		t.Fatalf("new vikingfs: %v", err)
	}
	return vfs
}

func TestSanitizeSegment(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello world", "hello_world"},
		{"path/to/file", "path_to_file"},
		{"  spaces  ", "spaces"},
		{"", "unnamed"},
		{"normal", "normal"},
	}
	for _, tc := range tests {
		got := sanitizeSegment(tc.input)
		if got != tc.want {
			t.Errorf("sanitize(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestParseCodeHostingURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/volcengine/OpenViking", "volcengine/OpenViking"},
		{"https://github.com/user/repo.git", "user/repo"},
		{"https://gitlab.com/org/project", "org/project"},
		{"https://bitbucket.org/team/repo", "team/repo"},
		{"https://example.com/foo/bar", ""},
		{"not-a-url", ""},
	}
	for _, tc := range tests {
		got := parseCodeHostingURL(tc.url)
		if got != tc.want {
			t.Errorf("parseCodeHostingURL(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestFinalizeFromTemp(t *testing.T) {
	vfs := newTestVFS(t)
	tb := NewTreeBuilder(vfs)
	reqCtx := &ctx.RequestContext{}

	tempURI := "viking://tmp/temp123"
	docURI := tempURI + "/my_document"

	vfs.Mkdir(tempURI, reqCtx)
	vfs.Mkdir(docURI, reqCtx)
	vfs.WriteString(docURI+"/content.txt", "hello world", reqCtx)

	result, err := tb.FinalizeFromTemp(FinalizeConfig{
		TempDirPath: tempURI,
		Scope:       "resources",
		ReqCtx:      reqCtx,
	})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if result.RootURI != "viking://resources/my_document" {
		t.Errorf("root URI = %s", result.RootURI)
	}

	if !vfs.Exists(result.RootURI+"/content.txt", reqCtx) {
		t.Error("content.txt not found at final URI")
	}
}

func TestFinalizeFromTempWithToURI(t *testing.T) {
	vfs := newTestVFS(t)
	tb := NewTreeBuilder(vfs)
	reqCtx := &ctx.RequestContext{}

	tempURI := "viking://tmp/temp456"
	docURI := tempURI + "/doc"

	vfs.Mkdir(tempURI, reqCtx)
	vfs.Mkdir(docURI, reqCtx)
	vfs.WriteString(docURI+"/file.md", "content", reqCtx)

	result, err := tb.FinalizeFromTemp(FinalizeConfig{
		TempDirPath: tempURI,
		Scope:       "resources",
		ToURI:       "viking://resources/exact_target",
		ReqCtx:      reqCtx,
	})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if result.RootURI != "viking://resources/exact_target" {
		t.Errorf("root URI = %s", result.RootURI)
	}
}

func TestFinalizeFromTempUniqueURI(t *testing.T) {
	vfs := newTestVFS(t)
	tb := NewTreeBuilder(vfs)
	reqCtx := &ctx.RequestContext{}

	vfs.Mkdir("viking://resources/my_doc", reqCtx)

	tempURI := "viking://tmp/temp789"
	vfs.Mkdir(tempURI, reqCtx)
	vfs.Mkdir(tempURI+"/my_doc", reqCtx)
	vfs.WriteString(tempURI+"/my_doc/file.txt", "data", reqCtx)

	result, err := tb.FinalizeFromTemp(FinalizeConfig{
		TempDirPath: tempURI,
		Scope:       "resources",
		ReqCtx:      reqCtx,
	})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if result.RootURI != "viking://resources/my_doc_1" {
		t.Errorf("expected unique URI, got %s", result.RootURI)
	}
}

func TestFinalizeFromTempRepository(t *testing.T) {
	vfs := newTestVFS(t)
	tb := NewTreeBuilder(vfs)
	reqCtx := &ctx.RequestContext{}

	tempURI := "viking://tmp/tempRepo"
	vfs.Mkdir(tempURI, reqCtx)
	vfs.Mkdir(tempURI+"/repo-name", reqCtx)

	result, err := tb.FinalizeFromTemp(FinalizeConfig{
		TempDirPath:  tempURI,
		Scope:        "resources",
		SourcePath:   "https://github.com/volcengine/OpenViking",
		SourceFormat: "repository",
		ReqCtx:       reqCtx,
	})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if result.RootURI != "viking://resources/volcengine/OpenViking" {
		t.Errorf("root URI = %s", result.RootURI)
	}
}

func TestFinalizeFromTempNoDocDir(t *testing.T) {
	vfs := newTestVFS(t)
	tb := NewTreeBuilder(vfs)
	reqCtx := &ctx.RequestContext{}

	tempURI := "viking://tmp/empty"
	vfs.Mkdir(tempURI, reqCtx)

	_, err := tb.FinalizeFromTemp(FinalizeConfig{
		TempDirPath: tempURI,
		ReqCtx:      reqCtx,
	})
	if err == nil {
		t.Error("expected error for empty temp dir")
	}
}

func TestBuildParseTree(t *testing.T) {
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "test.md")
	os.WriteFile(srcFile, []byte("# Hello\n\nContent here"), 0644)

	vfs := newTestVFS(t)
	tb := NewTreeBuilder(vfs)
	reqCtx := &ctx.RequestContext{}

	tempURI, err := tb.BuildParseTree(srcFile, reqCtx)
	if err != nil {
		t.Fatalf("build parse tree: %v", err)
	}
	if tempURI == "" {
		t.Fatal("empty temp URI")
	}

	entries, err := vfs.Ls(tempURI, reqCtx)
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	if len(entries) == 0 {
		t.Error("no entries in temp URI")
	}
}

func TestGetBaseURI(t *testing.T) {
	vfs := newTestVFS(t)
	tb := NewTreeBuilder(vfs)

	tests := []struct {
		scope string
		want  string
	}{
		{"resources", "viking://resources"},
		{"user", "viking://user"},
		{"agent", "viking://agent"},
		{"unknown", "viking://resources"},
	}
	for _, tc := range tests {
		got := tb.getBaseURI(tc.scope)
		if got != tc.want {
			t.Errorf("getBaseURI(%q) = %q, want %q", tc.scope, got, tc.want)
		}
	}
}
