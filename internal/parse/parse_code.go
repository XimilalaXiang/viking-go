package parse

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

var codeExtensions = []string{
	".go", ".py", ".js", ".ts", ".jsx", ".tsx",
	".java", ".c", ".cpp", ".h", ".hpp", ".cc", ".cxx",
	".rs", ".rb", ".php", ".swift", ".kt", ".kts",
	".sh", ".bash", ".zsh", ".fish",
	".sql", ".r", ".R", ".lua", ".pl", ".pm",
	".css", ".scss", ".less", ".sass",
	".vue", ".svelte",
	".proto", ".graphql", ".gql",
	".tf", ".hcl",
	".dart", ".scala", ".groovy", ".clj", ".ex", ".exs",
	".zig", ".nim", ".v", ".d",
	".Makefile", ".cmake",
}

// CodeFileParser handles source code files with optional AST extraction.
type CodeFileParser struct{}

func (p *CodeFileParser) Name() string { return "code" }

func (p *CodeFileParser) Extensions() []string { return codeExtensions }

func (p *CodeFileParser) Parse(source string, isPath bool) (*ParseResult, error) {
	var content string
	var title string
	var ext string

	if isPath {
		data, err := os.ReadFile(source)
		if err != nil {
			return nil, err
		}
		content = string(data)
		title = filepath.Base(source)
		ext = strings.ToLower(filepath.Ext(source))
	} else {
		content = source
		title = "untitled"
		ext = ""
	}

	result := &ParseResult{
		Title:   title,
		Content: content,
		Format:  "code",
	}

	if ext == ".go" && isPath {
		skeleton, err := extractGoSkeleton(source)
		if err == nil && skeleton != "" {
			result.Abstract = skeleton
			result.Children = extractGoSymbols(source)
			return result, nil
		}
	}

	sk := ExtractSkeleton(content, title)
	if sk != nil {
		abstract := SkeletonToAbstract(sk, title)
		if abstract != "" {
			result.Abstract = abstract
			return result, nil
		}
	}

	result.Abstract = buildCodeAbstract(content, title, ext)
	return result, nil
}

func buildCodeAbstract(content, filename, ext string) string {
	lines := strings.Split(content, "\n")
	lineCount := len(lines)

	var imports, funcs, classes int
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "from "):
			imports++
		case strings.HasPrefix(trimmed, "func ") || strings.HasPrefix(trimmed, "def ") ||
			strings.HasPrefix(trimmed, "function "):
			funcs++
		case strings.HasPrefix(trimmed, "class ") || strings.HasPrefix(trimmed, "type ") ||
			strings.HasPrefix(trimmed, "struct "):
			classes++
		}
	}

	lang := strings.TrimPrefix(ext, ".")
	return fmt.Sprintf("%s (%s, %d lines, %d funcs, %d types)", filename, lang, lineCount, funcs, classes)
}

// extractGoSkeleton uses Go's AST parser for first-class Go support.
func extractGoSkeleton(path string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return "", err
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("package %s", f.Name.Name))

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sig := formatFuncSignature(d)
			parts = append(parts, sig)
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						parts = append(parts, fmt.Sprintf("type %s", ts.Name.Name))
					}
				}
			}
		}
	}

	return strings.Join(parts, "\n"), nil
}

func formatFuncSignature(d *ast.FuncDecl) string {
	name := d.Name.Name
	recv := ""
	if d.Recv != nil && len(d.Recv.List) > 0 {
		r := d.Recv.List[0]
		if len(r.Names) > 0 {
			recv = fmt.Sprintf("(%s) ", r.Names[0].Name)
		}
	}
	return fmt.Sprintf("func %s%s(...)", recv, name)
}

func extractGoSymbols(path string) []ParseResult {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil
	}

	var results []ParseResult
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			pr := ParseResult{
				Title:   d.Name.Name,
				Content: formatFuncSignature(d),
				Format:  "go_func",
			}
			if d.Doc != nil {
				pr.Abstract = d.Doc.Text()
			}
			results = append(results, pr)
		case *ast.GenDecl:
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						pr := ParseResult{
							Title:  ts.Name.Name,
							Format: "go_type",
						}
						if d.Doc != nil {
							pr.Abstract = d.Doc.Text()
						}
						results = append(results, pr)
					}
				}
			}
		}
	}
	return results
}
