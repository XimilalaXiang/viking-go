package parse

import (
	"os"
	"path/filepath"
	"testing"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

func TestExtractAbstract(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		filename string
		want     string
	}{
		{"heading", "# My Title\nContent here", "test.md", "My Title"},
		{"first line", "Some first line\nSecond line", "test.txt", "Some first line"},
		{"empty", "", "test.txt", "test.txt"},
		{"blank lines", "\n\n\nReal content", "test.txt", "Real content"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAbstract(tt.content, tt.filename)
			if got != tt.want {
				t.Errorf("extractAbstract() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractMarkdownSections(t *testing.T) {
	content := `# Title
Introduction text.

## Section One
Content of section one.

## Section Two
Content of section two.

### Subsection
Subsection content.
`
	sections := ExtractMarkdownSections(content)
	if len(sections) < 3 {
		t.Fatalf("got %d sections, want >= 3", len(sections))
	}
	if sections[0].Heading != "Title" {
		t.Errorf("sections[0].Heading = %q", sections[0].Heading)
	}
	if sections[0].Level != 1 {
		t.Errorf("sections[0].Level = %d", sections[0].Level)
	}
}

func TestImportFile(t *testing.T) {
	tmpDir := t.TempDir()
	vfs, _ := vikingfs.New(filepath.Join(tmpDir, "vfs"))

	testFile := filepath.Join(tmpDir, "test.md")
	os.WriteFile(testFile, []byte("# Test\nHello world"), 0644)

	p := NewParser(vfs, nil)
	rc := ctx.RootContext()

	result, err := p.ImportFile(testFile, "viking://resources/test.md", rc)
	if err != nil {
		t.Fatalf("ImportFile: %v", err)
	}
	if result.Title != "test.md" {
		t.Errorf("Title = %q", result.Title)
	}
	if result.Abstract != "Test" {
		t.Errorf("Abstract = %q", result.Abstract)
	}

	// WriteContext creates a directory; verify the abstract
	abs, err := vfs.Abstract("viking://resources/test.md", rc)
	if err != nil {
		t.Fatalf("Abstract: %v", err)
	}
	if abs != "Test" {
		t.Errorf("abstract mismatch: %q", abs)
	}
}

func TestImportDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	vfs, _ := vikingfs.New(filepath.Join(tmpDir, "vfs"))

	srcDir := filepath.Join(tmpDir, "src")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "a.md"), []byte("# A\nContent A"), 0644)
	os.WriteFile(filepath.Join(srcDir, "b.txt"), []byte("Content B"), 0644)
	os.WriteFile(filepath.Join(srcDir, "c.bin"), []byte("binary"), 0644) // unsupported

	p := NewParser(vfs, nil)
	rc := ctx.RootContext()

	result, err := p.ImportDirectory(srcDir, "viking://resources/src", rc)
	if err != nil {
		t.Fatalf("ImportDirectory: %v", err)
	}
	if !result.IsDir {
		t.Error("expected IsDir=true")
	}
	if len(result.Children) != 2 {
		t.Errorf("children count = %d, want 2 (excluding .bin)", len(result.Children))
	}
}

func TestUnsupportedExtension(t *testing.T) {
	tmpDir := t.TempDir()
	vfs, _ := vikingfs.New(filepath.Join(tmpDir, "vfs"))

	testFile := filepath.Join(tmpDir, "test.bin")
	os.WriteFile(testFile, []byte("binary data"), 0644)

	p := NewParser(vfs, nil)
	rc := ctx.RootContext()

	_, err := p.ImportFile(testFile, "viking://resources/test.bin", rc)
	if err == nil {
		t.Error("expected error for unsupported extension")
	}
}
