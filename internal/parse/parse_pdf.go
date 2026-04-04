package parse

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
)

// PDFFileParser extracts text from PDF files using ledongthuc/pdf.
type PDFFileParser struct{}

func (p *PDFFileParser) Name() string { return "pdf" }

func (p *PDFFileParser) Extensions() []string { return []string{".pdf"} }

func (p *PDFFileParser) Parse(source string, isPath bool) (*ParseResult, error) {
	if !isPath {
		return nil, fmt.Errorf("PDF parsing requires a file path, not raw content")
	}

	content, err := extractPDFText(source)
	if err != nil {
		return nil, fmt.Errorf("extract PDF text: %w", err)
	}

	title := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	abstract := truncateStr(extractTextAbstract(content, title), 200)

	return &ParseResult{
		Title:    title,
		Abstract: abstract,
		Content:  content,
		Format:   "pdf",
	}, nil
}

func extractPDFText(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("open PDF: %w", err)
	}
	defer f.Close()

	var b strings.Builder
	totalPages := r.NumPage()

	for i := 1; i <= totalPages; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}

		trimmed := strings.TrimSpace(text)
		if trimmed != "" {
			if b.Len() > 0 {
				b.WriteString("\n\n---\n\n")
			}
			b.WriteString(fmt.Sprintf("## Page %d\n\n", i))
			b.WriteString(trimmed)
		}
	}

	if b.Len() == 0 {
		return "", fmt.Errorf("no extractable text in PDF (may be scanned/image-only)")
	}

	return b.String(), nil
}

// PDFPageCount returns the number of pages without full text extraction.
func PDFPageCount(path string) (int, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return r.NumPage(), nil
}

// IsPDF checks whether a file appears to be a PDF by magic bytes.
func IsPDF(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	header := make([]byte, 5)
	n, _ := f.Read(header)
	return n >= 5 && string(header) == "%PDF-"
}
