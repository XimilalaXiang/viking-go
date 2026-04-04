package parse

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectResourceURL(t *testing.T) {
	info, err := DetectResource("https://github.com/example/repo")
	if err != nil {
		t.Fatal(err)
	}
	if info.VisitType != VisitNeedDownload {
		t.Errorf("expected NEED_DOWNLOAD, got %s", info.VisitType)
	}
	if info.RecursiveType != RecursiveRecursive {
		t.Errorf("expected RECURSIVE for git URL, got %s", info.RecursiveType)
	}
}

func TestDetectResourceSingleURL(t *testing.T) {
	info, err := DetectResource("https://example.com/document.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if info.VisitType != VisitNeedDownload {
		t.Errorf("expected NEED_DOWNLOAD, got %s", info.VisitType)
	}
	if info.RecursiveType != RecursiveSingle {
		t.Errorf("expected SINGLE for non-git URL, got %s", info.RecursiveType)
	}
}

func TestDetectResourceLocalFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	os.WriteFile(f, []byte("hello world"), 0644)

	info, err := DetectResource(f)
	if err != nil {
		t.Fatal(err)
	}
	if info.VisitType != VisitFileSys {
		t.Errorf("expected FILE_SYS, got %s", info.VisitType)
	}
	if info.SizeType != SizeInMem {
		t.Errorf("expected IN_MEM for small file, got %s", info.SizeType)
	}
	if info.RecursiveType != RecursiveSingle {
		t.Errorf("expected SINGLE, got %s", info.RecursiveType)
	}
	if info.FileCount != 1 {
		t.Errorf("expected 1 file, got %d", info.FileCount)
	}
}

func TestDetectResourceDirectory(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0644)
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "c.txt"), []byte("nested"), 0644)

	info, err := DetectResource(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.VisitType != VisitFileSys {
		t.Errorf("expected FILE_SYS, got %s", info.VisitType)
	}
	if info.RecursiveType != RecursiveRecursive {
		t.Errorf("expected RECURSIVE for dir, got %s", info.RecursiveType)
	}
	if info.FileCount != 3 {
		t.Errorf("expected 3 files, got %d", info.FileCount)
	}
	if !info.IsDir {
		t.Error("expected IsDir=true")
	}
}

func TestDetectResourceZipFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "archive.zip")
	os.WriteFile(f, []byte("fake zip"), 0644)

	info, err := DetectResource(f)
	if err != nil {
		t.Fatal(err)
	}
	if info.RecursiveType != RecursiveExpandToRecursive {
		t.Errorf("expected EXPAND_TO_RECURSIVE for zip, got %s", info.RecursiveType)
	}
}

func TestDetectResourceNonExistent(t *testing.T) {
	info, err := DetectResource("/nonexistent/path/to/something")
	if err != nil {
		t.Fatal(err)
	}
	if info.VisitType != VisitDirectContent {
		t.Errorf("expected DIRECT_CONTENT for non-existent path, got %s", info.VisitType)
	}
}

func TestClassifySize(t *testing.T) {
	if classifySize(100) != SizeInMem {
		t.Error("100 bytes should be IN_MEM")
	}
	if classifySize(100*1024*1024) != SizeExternal {
		t.Error("100MB should be EXTERNAL")
	}
	if classifySize(20*1024*1024*1024) != SizeTooLargeToProcess {
		t.Error("20GB should be TOO_LARGE_TO_PROCESS")
	}
}

func TestIsURL(t *testing.T) {
	if !isURL("https://example.com") {
		t.Error("https://example.com should be URL")
	}
	if isURL("/local/path") {
		t.Error("/local/path should not be URL")
	}
	if isURL("relative/path") {
		t.Error("relative/path should not be URL")
	}
}
