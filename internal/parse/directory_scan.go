package parse

import (
	"os"
	"path/filepath"
	"strings"
)

// ClassifiedFile represents a file with its classification.
type ClassifiedFile struct {
	Path           string `json:"path"`
	RelPath        string `json:"rel_path"`
	Classification string `json:"classification"` // "processable" or "unsupported"
}

// DirectoryScanResult holds the result of a directory pre-scan.
type DirectoryScanResult struct {
	Root        string           `json:"root"`
	Processable []ClassifiedFile `json:"processable"`
	Unsupported []ClassifiedFile `json:"unsupported"`
	Skipped     []string         `json:"skipped"`
	Warnings    []string         `json:"warnings"`
}

// ScanConfig configures a directory scan.
type ScanConfig struct {
	IgnoreDirs []string // directory names to skip
	Include    string   // glob pattern for inclusion
	Exclude    string   // glob pattern for exclusion
	Strict     bool     // if true, fail on any unsupported files
}

var defaultIgnoreDirs = map[string]bool{
	"node_modules":     true,
	".git":             true,
	".svn":             true,
	".hg":              true,
	"__pycache__":      true,
	".tox":             true,
	".pytest_cache":    true,
	".mypy_cache":      true,
	".ruff_cache":      true,
	"venv":             true,
	".venv":            true,
	"env":              true,
	".env":             true,
	"dist":             true,
	"build":            true,
	".next":            true,
	".nuxt":            true,
	"target":           true,
	"vendor":           true,
}

// ScanDirectory performs a pre-scan of a directory, classifying files as
// processable or unsupported.
func ScanDirectory(root string, cfg ScanConfig) (*DirectoryScanResult, error) {
	reg := NewRegistry()
	ignoreDirs := buildIgnoreSet(cfg.IgnoreDirs)

	result := &DirectoryScanResult{Root: root}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		relPath, _ := filepath.Rel(root, path)

		if info.IsDir() {
			dirName := info.Name()
			if dirName == "." {
				return nil
			}
			if dirName == ".." {
				return filepath.SkipDir
			}
			if ignoreDirs[dirName] {
				result.Skipped = append(result.Skipped, "ignored_dir:"+relPath)
				return filepath.SkipDir
			}
			return nil
		}

		if shouldSkipFile(info) {
			result.Skipped = append(result.Skipped, "skip:"+relPath)
			return nil
		}

		if cfg.Exclude != "" {
			if matched, _ := filepath.Match(cfg.Exclude, info.Name()); matched {
				result.Skipped = append(result.Skipped, "excluded:"+relPath)
				return nil
			}
		}

		if cfg.Include != "" {
			matched, _ := filepath.Match(cfg.Include, info.Name())
			if !matched {
				result.Skipped = append(result.Skipped, "not_included:"+relPath)
				return nil
			}
		}

		cf := ClassifiedFile{
			Path:    path,
			RelPath: relPath,
		}

		if reg.CanParse(info.Name()) {
			cf.Classification = "processable"
			result.Processable = append(result.Processable, cf)
		} else {
			cf.Classification = "unsupported"
			result.Unsupported = append(result.Unsupported, cf)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if cfg.Strict && len(result.Unsupported) > 0 {
		names := make([]string, 0, len(result.Unsupported))
		for _, f := range result.Unsupported {
			names = append(names, f.RelPath)
		}
		result.Warnings = append(result.Warnings,
			"unsupported files in strict mode: "+strings.Join(names, ", "))
	}

	return result, nil
}

func buildIgnoreSet(extra []string) map[string]bool {
	m := make(map[string]bool, len(defaultIgnoreDirs)+len(extra))
	for k, v := range defaultIgnoreDirs {
		m[k] = v
	}
	for _, d := range extra {
		d = strings.TrimSpace(d)
		if d != "" {
			m[d] = true
		}
	}
	return m
}

func shouldSkipFile(info os.FileInfo) bool {
	if strings.HasPrefix(info.Name(), ".") {
		return true
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	if info.Size() == 0 {
		return true
	}
	return false
}
