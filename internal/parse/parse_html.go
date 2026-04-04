package parse

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// HTMLFileParser handles HTML files and URLs.
type HTMLFileParser struct{}

func (p *HTMLFileParser) Name() string { return "html" }

func (p *HTMLFileParser) Extensions() []string {
	return []string{".html", ".htm", ".xhtml", ".shtml"}
}

func (p *HTMLFileParser) Parse(source string, isPath bool) (*ParseResult, error) {
	var rawHTML string
	var title string

	if isPath {
		if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
			body, fetchTitle, err := fetchURL(source)
			if err != nil {
				return nil, fmt.Errorf("fetch URL: %w", err)
			}
			rawHTML = body
			title = fetchTitle
			if title == "" {
				title = source
			}
		} else {
			data, err := os.ReadFile(source)
			if err != nil {
				return nil, err
			}
			rawHTML = string(data)
			title = filepath.Base(source)
		}
	} else {
		rawHTML = source
		title = "untitled.html"
	}

	markdown, docTitle := htmlToMarkdown(rawHTML)
	if docTitle != "" && title == "untitled.html" {
		title = docTitle
	}

	abstract := truncateStr(stripHTML(rawHTML), 200)

	return &ParseResult{
		Title:    title,
		Abstract: abstract,
		Content:  markdown,
		Format:   "html",
	}, nil
}

func fetchURL(url string) (body, title string, err error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return "", "", err
	}
	return string(data), "", nil
}

// htmlToMarkdown converts HTML to a readable markdown-like format.
func htmlToMarkdown(rawHTML string) (string, string) {
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return rawHTML, ""
	}

	var b strings.Builder
	var docTitle string

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "title":
				if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
					docTitle = strings.TrimSpace(n.FirstChild.Data)
				}
			case "script", "style", "nav", "footer", "header", "aside", "noscript":
				return
			case "h1":
				b.WriteString("\n# ")
			case "h2":
				b.WriteString("\n## ")
			case "h3":
				b.WriteString("\n### ")
			case "h4":
				b.WriteString("\n#### ")
			case "h5":
				b.WriteString("\n##### ")
			case "h6":
				b.WriteString("\n###### ")
			case "p", "div", "section", "article":
				b.WriteString("\n")
			case "br":
				b.WriteString("\n")
			case "li":
				b.WriteString("\n- ")
			case "pre":
				b.WriteString("\n```\n")
			case "code":
				b.WriteString("`")
			case "strong", "b":
				b.WriteString("**")
			case "em", "i":
				b.WriteString("*")
			case "a":
				// handled by child text
			case "img":
				for _, attr := range n.Attr {
					if attr.Key == "alt" && attr.Val != "" {
						b.WriteString(fmt.Sprintf("[image: %s]", attr.Val))
					}
				}
			case "table":
				b.WriteString("\n")
			case "tr":
				b.WriteString("\n| ")
			case "td", "th":
				b.WriteString(" | ")
			}
		}

		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				b.WriteString(text)
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}

		if n.Type == html.ElementNode {
			switch n.Data {
			case "h1", "h2", "h3", "h4", "h5", "h6":
				b.WriteString("\n")
			case "pre":
				b.WriteString("\n```\n")
			case "code":
				b.WriteString("`")
			case "strong", "b":
				b.WriteString("**")
			case "em", "i":
				b.WriteString("*")
			case "p":
				b.WriteString("\n")
			}
		}
	}

	walk(doc)
	return strings.TrimSpace(b.String()), docTitle
}

// stripHTML removes tags and returns plain text (for abstract extraction).
func stripHTML(s string) string {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return s
	}
	var b strings.Builder
	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				b.WriteString(text)
				b.WriteString(" ")
			}
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "nav", "footer":
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(doc)
	return strings.TrimSpace(b.String())
}
