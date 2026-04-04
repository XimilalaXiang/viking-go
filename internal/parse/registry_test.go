package parse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryCanParse(t *testing.T) {
	r := NewRegistry()

	yes := []string{
		"doc.md", "doc.txt", "main.go", "app.py", "index.html",
		"report.pdf", "resume.docx", "data.xlsx", "book.epub",
		"style.css", "config.yaml", "data.json", "README.markdown",
	}
	for _, f := range yes {
		if !r.CanParse(f) {
			t.Errorf("expected CanParse(%q) = true", f)
		}
	}

	no := []string{
		"image.png", "video.mp4", "archive.tar.gz", "binary.bin", "photo.jpg",
	}
	for _, f := range no {
		if r.CanParse(f) {
			t.Errorf("expected CanParse(%q) = false", f)
		}
	}
}

func TestMarkdownParser(t *testing.T) {
	r := NewRegistry()
	result, err := r.ParseContent("# Hello\nWorld\n\n## Section\nContent", "test.md")
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != "markdown" {
		t.Errorf("Format = %q", result.Format)
	}
	if result.Abstract != "Hello" {
		t.Errorf("Abstract = %q", result.Abstract)
	}
	if len(result.Children) < 2 {
		t.Errorf("Children = %d, want >= 2", len(result.Children))
	}
}

func TestTextParser(t *testing.T) {
	r := NewRegistry()
	result, err := r.ParseContent("First line\nSecond line", "data.txt")
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != "text" {
		t.Errorf("Format = %q", result.Format)
	}
	if result.Abstract != "First line" {
		t.Errorf("Abstract = %q", result.Abstract)
	}
}

func TestCodeParser(t *testing.T) {
	r := NewRegistry()
	content := `package main

func main() {
	fmt.Println("hello")
}

func helper() string {
	return "hi"
}
`
	tmpDir := t.TempDir()
	goFile := filepath.Join(tmpDir, "main.go")
	os.WriteFile(goFile, []byte(content), 0644)

	result, err := r.ParseFile(goFile)
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != "code" {
		t.Errorf("Format = %q", result.Format)
	}
	if !strings.Contains(result.Abstract, "package main") {
		t.Errorf("Abstract missing package: %q", result.Abstract)
	}
	if len(result.Children) < 2 {
		t.Errorf("Children = %d, want >= 2 (main + helper)", len(result.Children))
	}
}

func TestHTMLParser(t *testing.T) {
	r := NewRegistry()
	html := `<!DOCTYPE html>
<html>
<head><title>Test Page</title></head>
<body>
<h1>Main Title</h1>
<p>Hello world.</p>
<h2>Section</h2>
<p>More content here.</p>
<script>var x = 1;</script>
</body>
</html>`
	result, err := r.ParseContent(html, "page.html")
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != "html" {
		t.Errorf("Format = %q", result.Format)
	}
	if !strings.Contains(result.Content, "# Main Title") {
		t.Errorf("Content should contain markdown heading, got: %s", result.Content[:200])
	}
	if strings.Contains(result.Content, "var x = 1") {
		t.Error("Content should not contain script content")
	}
}

func TestExcelParserFile(t *testing.T) {
	r := NewRegistry()
	result, err := r.ParseContent("", "data.xlsx")
	if err == nil && result != nil {
		t.Log("ParseContent for xlsx expected error since it requires file path")
	}
	// Excel requires a file path — verify it errors on content parsing
	if err == nil {
		t.Error("expected error when parsing xlsx from content")
	}
}

func TestRegistryAllExtensions(t *testing.T) {
	r := NewRegistry()
	exts := r.AllExtensions()
	if len(exts) < 30 {
		t.Errorf("AllExtensions returned %d, expected 30+", len(exts))
	}

	// Verify key extensions are present
	extSet := make(map[string]bool)
	for _, e := range exts {
		extSet[e] = true
	}
	must := []string{".md", ".txt", ".go", ".py", ".html", ".pdf", ".docx", ".xlsx", ".epub", ".css"}
	for _, m := range must {
		if !extSet[m] {
			t.Errorf("missing extension: %s", m)
		}
	}
}

func TestCodeParserAbstract(t *testing.T) {
	r := NewRegistry()
	pyContent := `import os
import sys

def hello():
    print("hello")

def world():
    print("world")

class MyClass:
    pass
`
	result, err := r.ParseContent(pyContent, "app.py")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Abstract, "2 funcs") {
		t.Errorf("Abstract should mention 2 funcs: %q", result.Abstract)
	}
	if !strings.Contains(result.Abstract, "1 types") {
		t.Errorf("Abstract should mention 1 type: %q", result.Abstract)
	}
}

func TestParserForSelection(t *testing.T) {
	r := NewRegistry()

	tests := []struct {
		file   string
		parser string
	}{
		{"README.md", "markdown"},
		{"data.csv", "text"},
		{"main.go", "code"},
		{"index.html", "html"},
		{"report.pdf", "pdf"},
		{"resume.docx", "word"},
		{"data.xlsx", "excel"},
		{"book.epub", "epub"},
	}

	for _, tt := range tests {
		p := r.ParserFor(tt.file)
		if p == nil {
			t.Errorf("no parser for %s", tt.file)
			continue
		}
		if p.Name() != tt.parser {
			t.Errorf("ParserFor(%q).Name() = %q, want %q", tt.file, p.Name(), tt.parser)
		}
	}
}
