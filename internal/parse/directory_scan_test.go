package parse

import (
	"os"
	"path/filepath"
	"testing"
)

func createTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Hello"), 0644)
	os.WriteFile(filepath.Join(dir, "main.py"), []byte("print('hi')"), 0644)
	os.WriteFile(filepath.Join(dir, "data.bin"), []byte{0x00, 0x01}, 0644)
	os.WriteFile(filepath.Join(dir, ".hidden"), []byte("secret"), 0644)

	sub := filepath.Join(dir, "src")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "app.js"), []byte("const x=1"), 0644)

	nodeModules := filepath.Join(dir, "node_modules")
	os.MkdirAll(nodeModules, 0755)
	os.WriteFile(filepath.Join(nodeModules, "dep.js"), []byte("module"), 0644)

	return dir
}

func TestScanDirectoryBasic(t *testing.T) {
	dir := createTestDir(t)
	result, err := ScanDirectory(dir, ScanConfig{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(result.Processable) < 3 {
		t.Errorf("processable = %d, want >= 3 (readme.md, main.py, app.js)", len(result.Processable))
	}

	hasSkippedHidden := false
	hasSkippedNodeModules := false
	for _, s := range result.Skipped {
		if s == "skip:.hidden" {
			hasSkippedHidden = true
		}
		if s == "ignored_dir:node_modules" {
			hasSkippedNodeModules = true
		}
	}
	if !hasSkippedHidden {
		t.Error("expected .hidden to be skipped")
	}
	if !hasSkippedNodeModules {
		t.Error("expected node_modules to be skipped")
	}
}

func TestScanDirectoryWithInclude(t *testing.T) {
	dir := createTestDir(t)
	result, err := ScanDirectory(dir, ScanConfig{
		Include: "*.md",
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	for _, f := range result.Processable {
		if !filepath.HasPrefix(f.RelPath, "readme") && f.RelPath != "readme.md" {
			if filepath.Ext(f.Path) != ".md" {
				t.Errorf("non-md file in processable with include=*.md: %s", f.RelPath)
			}
		}
	}
}

func TestScanDirectoryWithExclude(t *testing.T) {
	dir := createTestDir(t)
	result, err := ScanDirectory(dir, ScanConfig{
		Exclude: "*.py",
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	for _, f := range result.Processable {
		if filepath.Ext(f.Path) == ".py" {
			t.Errorf(".py file not excluded: %s", f.RelPath)
		}
	}
}

func TestScanDirectoryWithIgnoreDirs(t *testing.T) {
	dir := createTestDir(t)
	result, err := ScanDirectory(dir, ScanConfig{
		IgnoreDirs: []string{"src"},
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	for _, f := range result.Processable {
		if filepath.Dir(f.RelPath) == "src" {
			t.Errorf("file from ignored dir 'src': %s", f.RelPath)
		}
	}
}

func TestScanDirectoryStrict(t *testing.T) {
	dir := createTestDir(t)
	result, err := ScanDirectory(dir, ScanConfig{Strict: true})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	if len(result.Unsupported) > 0 && len(result.Warnings) == 0 {
		t.Error("strict mode with unsupported files should produce warnings")
	}
}

func TestShouldSkipFile(t *testing.T) {
	tmpDir := t.TempDir()

	dotFile := filepath.Join(tmpDir, ".env")
	os.WriteFile(dotFile, []byte("X=1"), 0644)
	info, _ := os.Stat(dotFile)
	if !shouldSkipFile(info) {
		t.Error("dot file should be skipped")
	}

	emptyFile := filepath.Join(tmpDir, "empty.txt")
	os.WriteFile(emptyFile, nil, 0644)
	info, _ = os.Stat(emptyFile)
	if !shouldSkipFile(info) {
		t.Error("empty file should be skipped")
	}

	normalFile := filepath.Join(tmpDir, "normal.txt")
	os.WriteFile(normalFile, []byte("data"), 0644)
	info, _ = os.Stat(normalFile)
	if shouldSkipFile(info) {
		t.Error("normal file should not be skipped")
	}
}
