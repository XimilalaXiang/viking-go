package vikingfs

import (
	"os"
	"path/filepath"
	"testing"

	ctx "github.com/ximilala/viking-go/internal/context"
)

func testCtx() *ctx.RequestContext {
	return ctx.RootContext()
}

func tempVFS(t *testing.T) *VikingFS {
	t.Helper()
	dir := t.TempDir()
	vfs, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return vfs
}

func TestURIToPathRoundTrip(t *testing.T) {
	vfs := tempVFS(t)
	rc := testCtx()

	cases := []struct {
		uri  string
		tail string
	}{
		{"viking://user/memories/hello", "default/user/memories/hello"},
		{"viking://resources/doc.md", "default/resources/doc.md"},
		{"viking://", "default"},
	}

	for _, tc := range cases {
		p, err := vfs.URIToPath(tc.uri, rc)
		if err != nil {
			t.Errorf("URIToPath(%q): %v", tc.uri, err)
			continue
		}
		want := filepath.Join(vfs.rootDir, tc.tail)
		if p != want {
			t.Errorf("URIToPath(%q) = %q, want %q", tc.uri, p, want)
		}
		gotURI := vfs.PathToURI(p, rc)
		if gotURI != tc.uri {
			t.Errorf("PathToURI round-trip: got %q, want %q", gotURI, tc.uri)
		}
	}
}

func TestWriteReadFile(t *testing.T) {
	vfs := tempVFS(t)
	rc := testCtx()

	uri := "viking://resources/test.md"
	content := "Hello, Viking!"

	if err := vfs.WriteString(uri, content, rc); err != nil {
		t.Fatalf("WriteString: %v", err)
	}

	got, err := vfs.ReadFile(uri, rc)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got != content {
		t.Errorf("ReadFile = %q, want %q", got, content)
	}
}

func TestWriteContext(t *testing.T) {
	vfs := tempVFS(t)
	rc := testCtx()

	uri := "viking://user/memories/greetings"
	if err := vfs.WriteContext(uri, "TL;DR: greetings", "Overview text", "Full details", "", rc); err != nil {
		t.Fatalf("WriteContext: %v", err)
	}

	abs, err := vfs.Abstract(uri, rc)
	if err != nil {
		t.Fatalf("Abstract: %v", err)
	}
	if abs != "TL;DR: greetings" {
		t.Errorf("Abstract = %q", abs)
	}

	ov, err := vfs.Overview(uri, rc)
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if ov != "Overview text" {
		t.Errorf("Overview = %q", ov)
	}

	content, err := vfs.ReadFile(uri+"/content.md", rc)
	if err != nil {
		t.Fatalf("ReadFile content.md: %v", err)
	}
	if content != "Full details" {
		t.Errorf("content.md = %q", content)
	}
}

func TestLs(t *testing.T) {
	vfs := tempVFS(t)
	rc := testCtx()

	vfs.WriteString("viking://resources/a.md", "a", rc)
	vfs.WriteString("viking://resources/b.md", "b", rc)

	entries, err := vfs.Ls("viking://resources", rc)
	if err != nil {
		t.Fatalf("Ls: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Ls count = %d, want 2", len(entries))
	}
}

func TestTree(t *testing.T) {
	vfs := tempVFS(t)
	rc := testCtx()

	vfs.WriteString("viking://resources/a/doc.md", "a", rc)
	vfs.WriteString("viking://resources/b/doc.md", "b", rc)

	entries, err := vfs.Tree("viking://resources", 100, 5, rc)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if len(entries) < 4 {
		t.Fatalf("Tree count = %d, want >= 4 (2 dirs + 2 files)", len(entries))
	}
}

func TestRelations(t *testing.T) {
	vfs := tempVFS(t)
	rc := testCtx()

	vfs.Mkdir("viking://user/memories/a", rc)
	vfs.Mkdir("viking://user/memories/b", rc)

	err := vfs.Link("viking://user/memories/a", []string{"viking://user/memories/b"}, "related", rc)
	if err != nil {
		t.Fatalf("Link: %v", err)
	}

	uris, err := vfs.RelatedURIs("viking://user/memories/a", rc)
	if err != nil {
		t.Fatalf("RelatedURIs: %v", err)
	}
	if len(uris) != 1 || uris[0] != "viking://user/memories/b" {
		t.Errorf("RelatedURIs = %v", uris)
	}

	err = vfs.Unlink("viking://user/memories/a", "viking://user/memories/b", rc)
	if err != nil {
		t.Fatalf("Unlink: %v", err)
	}

	uris, err = vfs.RelatedURIs("viking://user/memories/a", rc)
	if err != nil {
		t.Fatalf("RelatedURIs after unlink: %v", err)
	}
	if len(uris) != 0 {
		t.Errorf("RelatedURIs after unlink = %v, want empty", uris)
	}
}

func TestMvAndExists(t *testing.T) {
	vfs := tempVFS(t)
	rc := testCtx()

	vfs.WriteString("viking://resources/old.md", "data", rc)
	if !vfs.Exists("viking://resources/old.md", rc) {
		t.Fatal("old.md should exist")
	}

	if err := vfs.Mv("viking://resources/old.md", "viking://resources/new.md", rc); err != nil {
		t.Fatalf("Mv: %v", err)
	}

	if vfs.Exists("viking://resources/old.md", rc) {
		t.Error("old.md should not exist after mv")
	}
	if !vfs.Exists("viking://resources/new.md", rc) {
		t.Error("new.md should exist after mv")
	}
}

func TestRm(t *testing.T) {
	vfs := tempVFS(t)
	rc := testCtx()

	vfs.WriteString("viking://resources/delete-me.md", "bye", rc)
	if err := vfs.Rm("viking://resources/delete-me.md", false, rc); err != nil {
		t.Fatalf("Rm: %v", err)
	}
	if vfs.Exists("viking://resources/delete-me.md", rc) {
		t.Error("file should be deleted")
	}
}

func TestGrep(t *testing.T) {
	vfs := tempVFS(t)
	rc := testCtx()

	vfs.WriteString("viking://resources/a.md", "Hello World", rc)
	vfs.WriteString("viking://resources/b.md", "Goodbye World", rc)

	matches, err := vfs.Grep("viking://resources", "Hello", false, 100, rc)
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("Grep matches = %d, want 1", len(matches))
	}
}

func TestAccessControl(t *testing.T) {
	vfs := tempVFS(t)
	user := &ctx.UserIdentifier{
		AccountID: "acct1",
		UserID:    "user1",
		AgentID:   "agent1",
	}
	userCtx := ctx.NewRequestContext(user, ctx.RoleUser)

	uri := "viking://user/acct1_user1/memories/test"
	if !vfs.isAccessible(uri, userCtx) {
		t.Errorf("should be accessible: %s", uri)
	}

	otherURI := "viking://user/other_space/memories/test"
	if vfs.isAccessible(otherURI, userCtx) {
		t.Errorf("should NOT be accessible: %s", otherURI)
	}

	resourceURI := "viking://resources/public"
	if !vfs.isAccessible(resourceURI, userCtx) {
		t.Errorf("resources should always be accessible")
	}
}

func TestCreateTempURI(t *testing.T) {
	vfs := tempVFS(t)
	rc := testCtx()

	tempURI, err := vfs.CreateTempURI(rc)
	if err != nil {
		t.Fatalf("CreateTempURI: %v", err)
	}

	p, _ := vfs.URIToPath(tempURI, rc)
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("temp dir should exist: %v", err)
	}
	if !info.IsDir() {
		t.Error("temp URI should point to a directory")
	}
}
