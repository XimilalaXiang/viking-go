package parse

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PPTXFileParser handles .pptx PowerPoint files.
// PPTX is an Office Open XML format (zip of XML files).
type PPTXFileParser struct{}

func (p *PPTXFileParser) Name() string       { return "pptx" }
func (p *PPTXFileParser) Extensions() []string { return []string{".pptx"} }

func (p *PPTXFileParser) Parse(source string, isPath bool) (*ParseResult, error) {
	if !isPath {
		return &ParseResult{
			Title:    "presentation",
			Content:  source,
			Abstract: "PowerPoint presentation",
			Format:   "pptx",
		}, nil
	}

	f, err := os.Open(source)
	if err != nil {
		return nil, fmt.Errorf("open pptx: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		return nil, fmt.Errorf("invalid pptx (not a zip): %w", err)
	}

	slides := extractPPTXSlides(zr)
	md := slidesToMarkdown(slides, filepath.Base(source))

	abstract := fmt.Sprintf("PowerPoint: %s (%d slides)", filepath.Base(source), len(slides))

	return &ParseResult{
		Title:    filepath.Base(source),
		Content:  md,
		Abstract: abstract,
		Format:   "pptx",
	}, nil
}

type pptxSlide struct {
	Number int
	Title  string
	Body   string
}

func extractPPTXSlides(zr *zip.Reader) []pptxSlide {
	slideFiles := make(map[string]*zip.File)
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			slideFiles[f.Name] = f
		}
	}

	var names []string
	for name := range slideFiles {
		names = append(names, name)
	}
	sort.Strings(names)

	var slides []pptxSlide
	for i, name := range names {
		f := slideFiles[name]
		title, body := parsePPTXSlideXML(f)
		slides = append(slides, pptxSlide{
			Number: i + 1,
			Title:  title,
			Body:   body,
		})
	}
	return slides
}

// xmlSld is a minimal representation of slide XML for text extraction.
type xmlSld struct {
	XMLName xml.Name `xml:"sld"`
	CSld    struct {
		SpTree struct {
			SPs []xmlSP `xml:"sp"`
		} `xml:"spTree"`
	} `xml:"cSld"`
}

type xmlSP struct {
	NvSpPr struct {
		NvPr struct {
			Ph *struct {
				Type string `xml:"type,attr"`
			} `xml:"ph"`
		} `xml:"nvPr"`
	} `xml:"nvSpPr"`
	TxBody *xmlTxBody `xml:"txBody"`
}

type xmlTxBody struct {
	Paragraphs []xmlP `xml:"p"`
}

type xmlP struct {
	Runs []xmlR `xml:"r"`
}

type xmlR struct {
	T string `xml:"t"`
}

func parsePPTXSlideXML(f *zip.File) (title, body string) {
	rc, err := f.Open()
	if err != nil {
		return "", ""
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		return "", ""
	}

	var sld xmlSld
	if err := xml.Unmarshal(data, &sld); err != nil {
		return "", extractPlainText(data)
	}

	var titleParts []string
	var bodyParts []string

	for _, sp := range sld.CSld.SpTree.SPs {
		text := extractSPText(sp)
		if text == "" {
			continue
		}

		isTitle := false
		if sp.NvSpPr.NvPr.Ph != nil {
			phType := strings.ToLower(sp.NvSpPr.NvPr.Ph.Type)
			if phType == "title" || phType == "ctrTitle" || phType == "ctrtitle" {
				isTitle = true
			}
		}

		if isTitle {
			titleParts = append(titleParts, text)
		} else {
			bodyParts = append(bodyParts, text)
		}
	}

	return strings.Join(titleParts, " "), strings.Join(bodyParts, "\n\n")
}

func extractSPText(sp xmlSP) string {
	if sp.TxBody == nil {
		return ""
	}
	var lines []string
	for _, p := range sp.TxBody.Paragraphs {
		var parts []string
		for _, r := range p.Runs {
			if r.T != "" {
				parts = append(parts, r.T)
			}
		}
		line := strings.Join(parts, "")
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func extractPlainText(data []byte) string {
	// Fallback: extract all <a:t> text elements
	type aT struct {
		XMLName xml.Name `xml:"t"`
		Text    string   `xml:",chardata"`
	}

	var parts []string
	d := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "t" {
			var t aT
			if d.DecodeElement(&t, &se) == nil && t.Text != "" {
				parts = append(parts, t.Text)
			}
		}
	}
	return strings.Join(parts, " ")
}

func slidesToMarkdown(slides []pptxSlide, filename string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", filename))

	total := len(slides)
	for _, slide := range slides {
		sb.WriteString(fmt.Sprintf("## Slide %d/%d\n\n", slide.Number, total))
		if slide.Title != "" {
			sb.WriteString(fmt.Sprintf("### %s\n\n", slide.Title))
		}
		if slide.Body != "" {
			sb.WriteString(slide.Body)
			sb.WriteString("\n\n")
		}
		sb.WriteString("---\n\n")
	}

	return sb.String()
}
