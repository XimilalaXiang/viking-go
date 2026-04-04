package parse

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ZipFileParser handles .zip archive files by listing their contents.
type ZipFileParser struct{}

func (p *ZipFileParser) Name() string       { return "zip" }
func (p *ZipFileParser) Extensions() []string { return []string{".zip"} }

func (p *ZipFileParser) Parse(source string, isPath bool) (*ParseResult, error) {
	if !isPath {
		return &ParseResult{
			Title:    "zip",
			Content:  source,
			Abstract: "ZIP archive content",
			Format:   "zip",
		}, nil
	}

	f, err := os.Open(source)
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return nil, fmt.Errorf("invalid zip: %w", err)
	}

	md := convertZipToMarkdown(zr, filepath.Base(source))

	abstract := fmt.Sprintf("ZIP Archive: %s (%d files)", filepath.Base(source), len(zr.File))

	return &ParseResult{
		Title:    filepath.Base(source),
		Content:  md,
		Abstract: abstract,
		Format:   "zip",
	}, nil
}

func convertZipToMarkdown(zr *zip.Reader, filename string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# ZIP Archive: %s\n\n", filename))
	sb.WriteString("## Archive Information\n\n")

	var files []*zip.File
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() {
			files = append(files, f)
		}
	}

	sb.WriteString(fmt.Sprintf("- **File:** %s\n", filename))
	sb.WriteString(fmt.Sprintf("- **Total files:** %d\n\n", len(files)))

	extCounts := make(map[string]int)
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Name))
		if ext == "" {
			ext = "(no extension)"
		}
		extCounts[ext]++
	}

	if len(extCounts) > 0 {
		sb.WriteString("### File Types Summary\n\n")
		sb.WriteString("| Extension | Count |\n")
		sb.WriteString("|-----------|-------|\n")

		type extEntry struct {
			ext   string
			count int
		}
		var entries []extEntry
		for ext, count := range extCounts {
			entries = append(entries, extEntry{ext, count})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].count > entries[j].count })
		for _, e := range entries {
			sb.WriteString(fmt.Sprintf("| %s | %d |\n", e.ext, e.count))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("### File List\n\n")
	sb.WriteString("| File | Size | Modified |\n")
	sb.WriteString("|------|------|----------|\n")

	for _, f := range files {
		name := strings.ReplaceAll(f.Name, "|", "\\|")
		size := formatSize(int64(f.UncompressedSize64))
		modified := f.Modified.Format(time.DateTime)
		sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n", name, size, modified))
	}

	return sb.String()
}

func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
