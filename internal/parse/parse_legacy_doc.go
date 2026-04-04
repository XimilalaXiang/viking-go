package parse

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// LegacyDocFileParser handles legacy .doc files (OLE2/CFBF format, pre-docx).
// It performs basic text extraction from the raw binary by scanning for
// the Word Document stream's text content.
type LegacyDocFileParser struct{}

func (p *LegacyDocFileParser) Name() string { return "legacy_doc" }

func (p *LegacyDocFileParser) Extensions() []string { return []string{".doc"} }

func (p *LegacyDocFileParser) Parse(source string, isPath bool) (*ParseResult, error) {
	if !isPath {
		return nil, fmt.Errorf("legacy .doc parsing requires a file path")
	}

	data, err := os.ReadFile(source)
	if err != nil {
		return nil, fmt.Errorf("read .doc: %w", err)
	}

	if len(data) < 8 || !isOLE2(data) {
		return nil, fmt.Errorf("not a valid OLE2/DOC file")
	}

	text := extractDocText(data)
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("no extractable text in .doc file")
	}

	title := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	abstract := truncateStr(extractAbstractFromText(text, title), 200)

	return &ParseResult{
		Title:    title,
		Abstract: abstract,
		Content:  text,
		Format:   "doc",
	}, nil
}

// isOLE2 checks the magic bytes for OLE2 Compound Document.
func isOLE2(data []byte) bool {
	return len(data) >= 8 &&
		data[0] == 0xD0 && data[1] == 0xCF &&
		data[2] == 0x11 && data[3] == 0xE0 &&
		data[4] == 0xA1 && data[5] == 0xB1 &&
		data[6] == 0x1A && data[7] == 0xE1
}

// extractDocText tries multiple strategies to get text from a .doc file.
func extractDocText(data []byte) string {
	text := extractUTF16Text(data)
	if text != "" {
		return cleanDocText(text)
	}

	text = extractASCIIText(data)
	return cleanDocText(text)
}

// extractUTF16Text scans for UTF-16LE encoded text runs in the binary data.
// Word .doc files store body text as UTF-16LE after the FIB header.
func extractUTF16Text(data []byte) string {
	if len(data) < 1024 {
		return ""
	}

	var result strings.Builder
	var utf16Buf []uint16
	threshold := 4

	for i := 512; i+1 < len(data); i += 2 {
		lo := data[i]
		hi := data[i+1]
		codePoint := uint16(lo) | uint16(hi)<<8

		if isUsefulUTF16Char(codePoint) {
			utf16Buf = append(utf16Buf, codePoint)
		} else {
			if len(utf16Buf) >= threshold {
				runes := utf16.Decode(utf16Buf)
				for _, r := range runes {
					result.WriteRune(r)
				}
			}
			utf16Buf = utf16Buf[:0]
		}
	}

	if len(utf16Buf) >= threshold {
		runes := utf16.Decode(utf16Buf)
		for _, r := range runes {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// extractASCIIText falls back to scanning for printable ASCII runs.
func extractASCIIText(data []byte) string {
	var result strings.Builder
	var run bytes.Buffer
	threshold := 6

	for _, b := range data[512:] {
		if b >= 0x20 && b < 0x7F || b == '\n' || b == '\r' || b == '\t' {
			run.WriteByte(b)
		} else {
			if run.Len() >= threshold {
				s := run.String()
				if utf8.ValidString(s) {
					result.WriteString(s)
					result.WriteByte('\n')
				}
			}
			run.Reset()
		}
	}

	if run.Len() >= threshold {
		s := run.String()
		if utf8.ValidString(s) {
			result.WriteString(s)
		}
	}

	return result.String()
}

func isUsefulUTF16Char(cp uint16) bool {
	if cp == 0 {
		return false
	}
	if cp >= 0x20 && cp < 0xFFFE {
		return true
	}
	if cp == '\n' || cp == '\r' || cp == '\t' {
		return true
	}
	return false
}

func cleanDocText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	lines := strings.Split(text, "\n")
	var cleaned []string
	emptyCount := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			emptyCount++
			if emptyCount <= 2 {
				cleaned = append(cleaned, "")
			}
			continue
		}
		emptyCount = 0
		cleaned = append(cleaned, trimmed)
	}

	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func extractAbstractFromText(text, title string) string {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if len(trimmed) > 200 {
			return trimmed[:200]
		}
		return trimmed
	}
	return title
}
