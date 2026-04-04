package parse

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestZipParser(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "test.zip")

	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)

	files := map[string]string{
		"readme.md":     "# Hello\nThis is a test",
		"src/main.go":   "package main\nfunc main() {}",
		"src/utils.go":  "package main\nfunc helper() {}",
		"data/config.json": `{"key": "value"}`,
	}
	for name, content := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte(content))
	}
	w.Close()
	f.Close()

	parser := &ZipFileParser{}
	result, err := parser.Parse(zipPath, true)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !strings.Contains(result.Abstract, "4 files") {
		t.Errorf("abstract = %s, want '4 files'", result.Abstract)
	}
	if !strings.Contains(result.Content, "readme.md") {
		t.Errorf("content missing 'readme.md'")
	}
	if !strings.Contains(result.Content, ".go") {
		t.Errorf("content missing '.go' extension")
	}
}

func TestZipParserRegistered(t *testing.T) {
	r := NewRegistry()
	if !r.CanParse("test.zip") {
		t.Error(".zip not registered")
	}
}
