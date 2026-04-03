package tree

import (
	"testing"

	ctx "github.com/ximilala/viking-go/internal/context"
)

func TestAddAndGet(t *testing.T) {
	bt := New("", "")

	c1 := ctx.NewContext("viking://user/alice/docs",
		ctx.WithAbstract("Docs root"),
		ctx.WithContextType("resource"),
	)
	c2 := ctx.NewContext("viking://user/alice/docs/readme.md",
		ctx.WithParentURI("viking://user/alice/docs"),
		ctx.WithAbstract("README"),
		ctx.WithContextType("resource"),
	)

	bt.Add(c1)
	bt.Add(c2)

	if bt.Len() != 2 {
		t.Errorf("Len = %d, want 2", bt.Len())
	}

	got := bt.Get("viking://user/alice/docs")
	if got == nil || got.Abstract != "Docs root" {
		t.Errorf("Get returned %v", got)
	}

	if bt.Get("nonexistent") != nil {
		t.Error("Get should return nil for missing URI")
	}
}

func TestRootAndChildren(t *testing.T) {
	bt := New("", "")

	root := ctx.NewContext("viking://user/alice/project",
		ctx.WithAbstract("Project root"),
	)
	child1 := ctx.NewContext("viking://user/alice/project/src",
		ctx.WithParentURI("viking://user/alice/project"),
		ctx.WithAbstract("Source"),
	)
	child2 := ctx.NewContext("viking://user/alice/project/tests",
		ctx.WithParentURI("viking://user/alice/project"),
		ctx.WithAbstract("Tests"),
	)
	grandchild := ctx.NewContext("viking://user/alice/project/src/main.go",
		ctx.WithParentURI("viking://user/alice/project/src"),
		ctx.WithAbstract("Main entry"),
	)

	bt.Add(root)
	bt.Add(child1)
	bt.Add(child2)
	bt.Add(grandchild)
	bt.SetRoot("viking://user/alice/project")

	if bt.Root() == nil {
		t.Fatal("Root is nil")
	}
	if bt.Root().Abstract != "Project root" {
		t.Errorf("Root abstract = %q", bt.Root().Abstract)
	}

	children := bt.Children("viking://user/alice/project")
	if len(children) != 2 {
		t.Errorf("Children count = %d, want 2", len(children))
	}

	parent := bt.Parent("viking://user/alice/project/src/main.go")
	if parent == nil || parent.Abstract != "Source" {
		t.Errorf("Parent = %v", parent)
	}
}

func TestPathToRoot(t *testing.T) {
	bt := New("", "")

	c1 := ctx.NewContext("viking://user/alice/a", ctx.WithAbstract("A"))
	c2 := ctx.NewContext("viking://user/alice/a/b", ctx.WithParentURI("viking://user/alice/a"), ctx.WithAbstract("B"))
	c3 := ctx.NewContext("viking://user/alice/a/b/c", ctx.WithParentURI("viking://user/alice/a/b"), ctx.WithAbstract("C"))

	bt.Add(c1)
	bt.Add(c2)
	bt.Add(c3)

	path := bt.PathToRoot("viking://user/alice/a/b/c")
	if len(path) != 3 {
		t.Fatalf("PathToRoot length = %d, want 3", len(path))
	}
	if path[0].Abstract != "C" || path[1].Abstract != "B" || path[2].Abstract != "A" {
		t.Errorf("PathToRoot order wrong: %s, %s, %s", path[0].Abstract, path[1].Abstract, path[2].Abstract)
	}
}

func TestToDirectoryStructure(t *testing.T) {
	bt := New("", "")

	root := ctx.NewContext("viking://user/alice/project",
		ctx.WithMeta(map[string]any{"semantic_title": "My Project"}),
		ctx.WithContextType("resource"),
	)
	child := ctx.NewContext("viking://user/alice/project/readme",
		ctx.WithParentURI("viking://user/alice/project"),
		ctx.WithMeta(map[string]any{"source_title": "README"}),
		ctx.WithContextType("resource"),
	)

	bt.Add(root)
	bt.Add(child)
	bt.SetRoot("viking://user/alice/project")

	dir := bt.ToDirectoryStructure()
	if dir == nil {
		t.Fatal("dir is nil")
	}
	if dir.Title != "My Project" {
		t.Errorf("root title = %q", dir.Title)
	}
	if len(dir.Children) != 1 {
		t.Fatalf("children count = %d", len(dir.Children))
	}
	if dir.Children[0].Title != "README" {
		t.Errorf("child title = %q", dir.Children[0].Title)
	}
}
