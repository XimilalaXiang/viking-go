package parse

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FeishuFileParser handles exported Feishu/Lark documents.
// Supports:
// - .feishu.json: Exported Feishu document blocks (JSON format)
// - .lark: Alias for Feishu format
//
// For Feishu cloud documents accessed via URL, use the Feishu API
// to export first, then parse the resulting file.
type FeishuFileParser struct{}

func (p *FeishuFileParser) Name() string { return "feishu" }

func (p *FeishuFileParser) Extensions() []string {
	return []string{".feishu", ".lark"}
}

func (p *FeishuFileParser) Parse(source string, isPath bool) (*ParseResult, error) {
	var content string
	var err error

	if isPath {
		data, readErr := os.ReadFile(source)
		if readErr != nil {
			return nil, fmt.Errorf("read feishu file: %w", readErr)
		}
		content = string(data)
	} else {
		content = source
	}

	md, title, err := feishuToMarkdown(content)
	if err != nil {
		return nil, fmt.Errorf("parse feishu content: %w", err)
	}

	if title == "" && isPath {
		title = strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
		title = strings.TrimSuffix(title, ".feishu")
	}

	abstract := feishuExtractAbstract(md, title)

	return &ParseResult{
		Title:    title,
		Abstract: truncateStr(abstract, 200),
		Content:  md,
		Format:   "feishu",
	}, nil
}

// feishuBlock represents a Feishu document block.
type feishuBlock struct {
	BlockType int             `json:"block_type"`
	BlockID   string          `json:"block_id"`
	ParentID  string          `json:"parent_id,omitempty"`
	Children  []string        `json:"children,omitempty"`
	Text      *feishuText     `json:"text,omitempty"`
	Heading1  *feishuText     `json:"heading1,omitempty"`
	Heading2  *feishuText     `json:"heading2,omitempty"`
	Heading3  *feishuText     `json:"heading3,omitempty"`
	Heading4  *feishuText     `json:"heading4,omitempty"`
	Heading5  *feishuText     `json:"heading5,omitempty"`
	Heading6  *feishuText     `json:"heading6,omitempty"`
	Heading7  *feishuText     `json:"heading7,omitempty"`
	Heading8  *feishuText     `json:"heading8,omitempty"`
	Heading9  *feishuText     `json:"heading9,omitempty"`
	Bullet    *feishuText     `json:"bullet,omitempty"`
	Ordered   *feishuText     `json:"ordered,omitempty"`
	Code      *feishuCode     `json:"code,omitempty"`
	Quote     *feishuText     `json:"quote,omitempty"`
	TodoList  *feishuTodo     `json:"todo,omitempty"`
	Divider   json.RawMessage `json:"divider,omitempty"`
	Image     *feishuImage    `json:"image,omitempty"`
	Callout   *feishuCallout  `json:"callout,omitempty"`
	Page      *feishuPage     `json:"page,omitempty"`
}

type feishuText struct {
	Elements []feishuElement `json:"elements"`
}

type feishuElement struct {
	TextRun *struct {
		Content string `json:"content"`
	} `json:"text_run,omitempty"`
	MentionUser *struct {
		UserID string `json:"user_id"`
	} `json:"mention_user,omitempty"`
	Equation *struct {
		Content string `json:"content"`
	} `json:"equation,omitempty"`
}

type feishuCode struct {
	Elements []feishuElement `json:"elements,omitempty"`
	Language int             `json:"language,omitempty"`
	Style    json.RawMessage `json:"style,omitempty"`
}

type feishuTodo struct {
	Elements []feishuElement `json:"elements"`
	Done     bool            `json:"done,omitempty"`
}

type feishuImage struct {
	Token  string `json:"token,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

type feishuCallout struct {
	BackgroundColor int `json:"background_color,omitempty"`
}

type feishuPage struct {
	Elements []feishuElement `json:"elements,omitempty"`
}

type feishuDocument struct {
	Document struct {
		Title   string `json:"title"`
		DocID   string `json:"document_id"`
		Version int    `json:"revision_id,omitempty"`
	} `json:"document,omitempty"`
	Blocks []feishuBlock `json:"blocks,omitempty"`
}

func feishuToMarkdown(content string) (markdown, title string, err error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", "", fmt.Errorf("empty content")
	}

	var doc feishuDocument
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		return plainTextFallback(content), "", nil
	}

	title = doc.Document.Title

	if len(doc.Blocks) == 0 {
		return plainTextFallback(content), title, nil
	}

	var sb strings.Builder
	orderedCounter := 0

	for _, block := range doc.Blocks {
		line := ""
		switch {
		case block.Page != nil:
			if title == "" {
				title = extractText(block.Page.Elements)
			}
			continue
		case block.Heading1 != nil:
			line = "# " + extractText(block.Heading1.Elements)
			orderedCounter = 0
		case block.Heading2 != nil:
			line = "## " + extractText(block.Heading2.Elements)
			orderedCounter = 0
		case block.Heading3 != nil:
			line = "### " + extractText(block.Heading3.Elements)
			orderedCounter = 0
		case block.Heading4 != nil:
			line = "#### " + extractText(block.Heading4.Elements)
			orderedCounter = 0
		case block.Heading5 != nil:
			line = "##### " + extractText(block.Heading5.Elements)
			orderedCounter = 0
		case block.Heading6 != nil:
			line = "###### " + extractText(block.Heading6.Elements)
			orderedCounter = 0
		case block.Text != nil:
			line = extractText(block.Text.Elements)
			orderedCounter = 0
		case block.Bullet != nil:
			line = "- " + extractText(block.Bullet.Elements)
			orderedCounter = 0
		case block.Ordered != nil:
			orderedCounter++
			line = fmt.Sprintf("%d. %s", orderedCounter, extractText(block.Ordered.Elements))
		case block.Code != nil:
			text := extractText(block.Code.Elements)
			lang := feishuCodeLang(block.Code.Language)
			line = fmt.Sprintf("```%s\n%s\n```", lang, text)
			orderedCounter = 0
		case block.Quote != nil:
			line = "> " + extractText(block.Quote.Elements)
			orderedCounter = 0
		case block.TodoList != nil:
			check := " "
			if block.TodoList.Done {
				check = "x"
			}
			line = fmt.Sprintf("- [%s] %s", check, extractText(block.TodoList.Elements))
			orderedCounter = 0
		case block.Divider != nil:
			line = "---"
			orderedCounter = 0
		case block.Image != nil:
			alt := "image"
			if block.Image.Token != "" {
				alt = block.Image.Token
			}
			line = fmt.Sprintf("![%s](feishu://image/%s)", alt, block.Image.Token)
			orderedCounter = 0
		default:
			orderedCounter = 0
			continue
		}

		sb.WriteString(line)
		sb.WriteString("\n\n")
	}

	return strings.TrimSpace(sb.String()), title, nil
}

func extractText(elements []feishuElement) string {
	var sb strings.Builder
	for _, el := range elements {
		if el.TextRun != nil {
			sb.WriteString(el.TextRun.Content)
		} else if el.Equation != nil {
			sb.WriteString("$" + el.Equation.Content + "$")
		} else if el.MentionUser != nil {
			sb.WriteString("@" + el.MentionUser.UserID)
		}
	}
	return sb.String()
}

func feishuCodeLang(langID int) string {
	langs := map[int]string{
		1: "plaintext", 2: "abap", 3: "ada", 4: "apache", 5: "apex",
		22: "go", 33: "java", 34: "javascript", 40: "markdown",
		43: "objective-c", 49: "python", 52: "rust", 55: "shell",
		60: "swift", 62: "typescript", 69: "sql", 73: "c", 76: "csharp",
		78: "cpp", 81: "json", 84: "yaml", 87: "xml", 90: "html",
	}
	if name, ok := langs[langID]; ok {
		return name
	}
	return ""
}

func plainTextFallback(content string) string {
	return content
}

func feishuExtractAbstract(content, title string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "---" {
			continue
		}
		trimmed = strings.TrimLeft(trimmed, "#> -")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed != "" {
			return trimmed
		}
	}
	if title != "" {
		return title
	}
	return "Feishu document"
}
