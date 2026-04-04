package parse

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type VisitType string

const (
	VisitDirectContent   VisitType = "DIRECT_CONTENT"
	VisitFileSys         VisitType = "FILE_SYS"
	VisitNeedDownload    VisitType = "NEED_DOWNLOAD"
	VisitReadyContextPack VisitType = "READY_CONTEXT_PACK"
)

type SizeType string

const (
	SizeInMem            SizeType = "IN_MEM"
	SizeExternal         SizeType = "EXTERNAL"
	SizeTooLargeToProcess SizeType = "TOO_LARGE_TO_PROCESS"
)

type RecursiveType string

const (
	RecursiveSingle          RecursiveType = "SINGLE"
	RecursiveRecursive       RecursiveType = "RECURSIVE"
	RecursiveExpandToRecursive RecursiveType = "EXPAND_TO_RECURSIVE"
)

type DetectInfo struct {
	VisitType     VisitType     `json:"visit_type"`
	SizeType      SizeType      `json:"size_type"`
	RecursiveType RecursiveType `json:"recursive_type"`
	Path          string        `json:"path"`
	IsDir         bool          `json:"is_dir"`
	TotalSize     int64         `json:"total_size"`
	FileCount     int           `json:"file_count"`
}

const (
	maxInMemSize      = 10 * 1024 * 1024   // 10 MB
	maxProcessingSize = 10 * 1024 * 1024 * 1024 // 10 GB
)

// DetectResource analyzes a resource path and returns classification info.
func DetectResource(path string) (*DetectInfo, error) {
	info := &DetectInfo{Path: path}

	if isURL(path) {
		info.VisitType = VisitNeedDownload
		info.SizeType = SizeExternal
		if isGitRepoURL(path) {
			info.RecursiveType = RecursiveRecursive
		} else {
			info.RecursiveType = RecursiveSingle
		}
		return info, nil
	}

	stat, err := os.Stat(path)
	if err != nil {
		info.VisitType = VisitDirectContent
		info.SizeType = SizeInMem
		info.RecursiveType = RecursiveSingle
		return info, nil
	}

	info.VisitType = VisitFileSys

	if stat.IsDir() {
		info.IsDir = true
		info.RecursiveType = RecursiveRecursive
		totalSize, fileCount := scanDirSize(path)
		info.TotalSize = totalSize
		info.FileCount = fileCount
		info.SizeType = classifySize(totalSize)
		return info, nil
	}

	info.TotalSize = stat.Size()
	info.FileCount = 1
	info.SizeType = classifySize(stat.Size())

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".zip", ".tar", ".gz", ".tgz", ".tar.gz":
		info.RecursiveType = RecursiveExpandToRecursive
	case ".ovpack":
		info.VisitType = VisitReadyContextPack
		info.RecursiveType = RecursiveExpandToRecursive
	default:
		info.RecursiveType = RecursiveSingle
	}

	return info, nil
}

func classifySize(totalBytes int64) SizeType {
	if totalBytes <= maxInMemSize {
		return SizeInMem
	}
	if totalBytes <= maxProcessingSize {
		return SizeExternal
	}
	return SizeTooLargeToProcess
}

func scanDirSize(root string) (int64, int) {
	var totalSize int64
	var fileCount int
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		totalSize += info.Size()
		fileCount++
		return nil
	})
	return totalSize, fileCount
}

func isURL(path string) bool {
	u, err := url.Parse(path)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func isGitRepoURL(path string) bool {
	for _, prefix := range []string{"https://github.com/", "https://gitlab.com/", "https://bitbucket.org/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return strings.HasSuffix(path, ".git")
}
