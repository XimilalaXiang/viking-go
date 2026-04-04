package parse

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// CodeSkeleton holds the extracted structural information from source code.
type CodeSkeleton struct {
	Language   string       `json:"language"`
	Package    string       `json:"package,omitempty"`
	Imports    []string     `json:"imports,omitempty"`
	Classes    []ClassInfo  `json:"classes,omitempty"`
	Functions  []FuncInfo   `json:"functions,omitempty"`
	Interfaces []string     `json:"interfaces,omitempty"`
	Constants  []string     `json:"constants,omitempty"`
	LineCount  int          `json:"line_count"`
}

// ClassInfo holds info about a class or type declaration.
type ClassInfo struct {
	Name       string     `json:"name"`
	Extends    string     `json:"extends,omitempty"`
	Implements []string   `json:"implements,omitempty"`
	Methods    []FuncInfo `json:"methods,omitempty"`
	DocComment string     `json:"doc,omitempty"`
}

// FuncInfo holds info about a function or method.
type FuncInfo struct {
	Name       string `json:"name"`
	Params     string `json:"params,omitempty"`
	ReturnType string `json:"return_type,omitempty"`
	Receiver   string `json:"receiver,omitempty"`
	IsAsync    bool   `json:"is_async,omitempty"`
	IsExported bool   `json:"is_exported,omitempty"`
	DocComment string `json:"doc,omitempty"`
}

// LanguageExtractor extracts code skeleton from source text for a given language.
type LanguageExtractor interface {
	Extract(source string) *CodeSkeleton
	Language() string
}

var extToLang = map[string]string{
	".py":    "python",
	".js":    "javascript",
	".jsx":   "javascript",
	".ts":    "typescript",
	".tsx":   "typescript",
	".java":  "java",
	".rs":    "rust",
	".rb":    "ruby",
	".php":   "php",
	".go":    "go",
	".c":     "c",
	".cpp":   "cpp",
	".cc":    "cpp",
	".h":     "c",
	".hpp":   "cpp",
	".cs":    "csharp",
	".kt":    "kotlin",
	".swift": "swift",
}

// ExtractSkeleton extracts a code skeleton from the given source.
func ExtractSkeleton(source, filename string) *CodeSkeleton {
	ext := strings.ToLower(filepath.Ext(filename))
	lang := extToLang[ext]
	if lang == "" {
		return nil
	}

	var extractor LanguageExtractor
	switch lang {
	case "python":
		extractor = &pythonExtractor{}
	case "javascript", "typescript":
		extractor = &jsTsExtractor{lang: lang}
	case "java":
		extractor = &javaExtractor{}
	case "rust":
		extractor = &rustExtractor{}
	case "ruby":
		extractor = &rubyExtractor{}
	case "csharp":
		extractor = &csharpExtractor{}
	case "go":
		extractor = &goExtractor{}
	case "php":
		extractor = &phpExtractor{}
	case "c", "cpp":
		extractor = &cppExtractor{lang: lang}
	default:
		return nil
	}

	return extractor.Extract(source)
}

// SkeletonToAbstract converts a CodeSkeleton to a human-readable abstract string.
func SkeletonToAbstract(sk *CodeSkeleton, filename string) string {
	if sk == nil {
		return ""
	}
	var parts []string
	if sk.Package != "" {
		parts = append(parts, sk.Package)
	}
	for _, c := range sk.Classes {
		desc := "class " + c.Name
		if c.Extends != "" {
			desc += " extends " + c.Extends
		}
		if len(c.Methods) > 0 {
			var methods []string
			for _, m := range c.Methods {
				methods = append(methods, m.Name)
			}
			desc += fmt.Sprintf(" [%s]", strings.Join(methods, ", "))
		}
		parts = append(parts, desc)
	}
	for _, f := range sk.Functions {
		sig := "func " + f.Name
		if f.Receiver != "" {
			sig = fmt.Sprintf("(%s) %s", f.Receiver, sig)
		}
		if f.Params != "" {
			sig += "(" + f.Params + ")"
		}
		parts = append(parts, sig)
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%s (%s, %d lines)", filename, sk.Language, sk.LineCount)
	}
	return strings.Join(parts, "\n")
}

// --- Python extractor ---

type pythonExtractor struct{}

func (e *pythonExtractor) Language() string { return "python" }

var (
	pyClassRe    = regexp.MustCompile(`(?m)^class\s+(\w+)(?:\(([^)]*)\))?:`)
	pyFuncRe     = regexp.MustCompile(`(?m)^(async\s+)?def\s+(\w+)\(([^)]*)\)(?:\s*->\s*([^:]+))?:`)
	pyMethodRe   = regexp.MustCompile(`(?m)^\s{4}(async\s+)?def\s+(\w+)\(([^)]*)\)(?:\s*->\s*([^:]+))?:`)
	pyImportRe   = regexp.MustCompile(`(?m)^(?:import\s+(\S+)|from\s+(\S+)\s+import)`)
)

func (e *pythonExtractor) Extract(source string) *CodeSkeleton {
	sk := &CodeSkeleton{Language: "python", LineCount: countLines(source)}

	for _, m := range pyImportRe.FindAllStringSubmatch(source, -1) {
		imp := m[1]
		if imp == "" {
			imp = m[2]
		}
		sk.Imports = append(sk.Imports, imp)
	}

	classes := pyClassRe.FindAllStringSubmatchIndex(source, -1)
	classEndMap := make(map[int]int)
	for i, match := range classes {
		start := match[0]
		end := len(source)
		if i+1 < len(classes) {
			end = classes[i+1][0]
		}
		classEndMap[start] = end
	}

	for _, m := range pyClassRe.FindAllStringSubmatch(source, -1) {
		ci := ClassInfo{Name: m[1]}
		if m[2] != "" {
			ci.Extends = strings.TrimSpace(m[2])
		}
		sk.Classes = append(sk.Classes, ci)
	}

	classRanges := pyClassRe.FindAllStringIndex(source, -1)
	for _, m := range pyMethodRe.FindAllStringSubmatch(source, -1) {
		_ = classRanges
		fi := FuncInfo{Name: m[2], Params: simplifyParams(m[3]), IsAsync: m[1] != ""}
		if m[4] != "" {
			fi.ReturnType = strings.TrimSpace(m[4])
		}
		if len(sk.Classes) > 0 {
			last := &sk.Classes[len(sk.Classes)-1]
			last.Methods = append(last.Methods, fi)
		}
	}

	for _, m := range pyFuncRe.FindAllStringSubmatch(source, -1) {
		fi := FuncInfo{Name: m[2], Params: simplifyParams(m[3]), IsAsync: m[1] != ""}
		if m[4] != "" {
			fi.ReturnType = strings.TrimSpace(m[4])
		}
		sk.Functions = append(sk.Functions, fi)
	}

	return sk
}

// --- JavaScript/TypeScript extractor ---

type jsTsExtractor struct {
	lang string
}

func (e *jsTsExtractor) Language() string { return e.lang }

var (
	jsClassRe  = regexp.MustCompile(`(?m)^(?:export\s+)?class\s+(\w+)(?:\s+extends\s+(\w+))?`)
	jsFuncRe   = regexp.MustCompile(`(?m)^(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*\(([^)]*)\)`)
	jsArrowRe  = regexp.MustCompile(`(?m)^(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?\(([^)]*)\)\s*(?::\s*(\w+))?\s*=>`)
	jsMethodRe = regexp.MustCompile(`(?m)^\s+(?:async\s+)?(\w+)\s*\(([^)]*)\)`)
	jsImportRe = regexp.MustCompile(`(?m)^import\s+.*?from\s+['"]([^'"]+)['"]`)
)

func (e *jsTsExtractor) Extract(source string) *CodeSkeleton {
	sk := &CodeSkeleton{Language: e.lang, LineCount: countLines(source)}

	for _, m := range jsImportRe.FindAllStringSubmatch(source, -1) {
		sk.Imports = append(sk.Imports, m[1])
	}

	for _, m := range jsClassRe.FindAllStringSubmatch(source, -1) {
		ci := ClassInfo{Name: m[1]}
		if m[2] != "" {
			ci.Extends = m[2]
		}
		sk.Classes = append(sk.Classes, ci)
	}

	for _, m := range jsFuncRe.FindAllStringSubmatch(source, -1) {
		sk.Functions = append(sk.Functions, FuncInfo{Name: m[1], Params: simplifyParams(m[2])})
	}
	for _, m := range jsArrowRe.FindAllStringSubmatch(source, -1) {
		fi := FuncInfo{Name: m[1], Params: simplifyParams(m[2])}
		if m[3] != "" {
			fi.ReturnType = m[3]
		}
		sk.Functions = append(sk.Functions, fi)
	}

	return sk
}

// --- Java extractor ---

type javaExtractor struct{}

func (e *javaExtractor) Language() string { return "java" }

var (
	javaClassRe   = regexp.MustCompile(`(?m)^(?:public|protected|private)?\s*(?:abstract\s+)?(?:class|interface|enum)\s+(\w+)(?:\s+extends\s+(\w+))?(?:\s+implements\s+([^{]+))?`)
	javaMethodRe  = regexp.MustCompile(`(?m)^\s+(?:public|protected|private)?\s*(?:static\s+)?(?:abstract\s+)?(\w+(?:<[^>]+>)?)\s+(\w+)\s*\(([^)]*)\)`)
	javaPackageRe = regexp.MustCompile(`(?m)^package\s+([\w.]+);`)
	javaImportRe  = regexp.MustCompile(`(?m)^import\s+([\w.]+);`)
)

func (e *javaExtractor) Extract(source string) *CodeSkeleton {
	sk := &CodeSkeleton{Language: "java", LineCount: countLines(source)}

	if m := javaPackageRe.FindStringSubmatch(source); m != nil {
		sk.Package = "package " + m[1]
	}
	for _, m := range javaImportRe.FindAllStringSubmatch(source, -1) {
		sk.Imports = append(sk.Imports, m[1])
	}
	for _, m := range javaClassRe.FindAllStringSubmatch(source, -1) {
		ci := ClassInfo{Name: m[1]}
		if m[2] != "" {
			ci.Extends = m[2]
		}
		if m[3] != "" {
			for _, impl := range strings.Split(m[3], ",") {
				ci.Implements = append(ci.Implements, strings.TrimSpace(impl))
			}
		}
		sk.Classes = append(sk.Classes, ci)
	}
	for _, m := range javaMethodRe.FindAllStringSubmatch(source, -1) {
		sk.Functions = append(sk.Functions, FuncInfo{
			Name:       m[2],
			ReturnType: m[1],
			Params:     simplifyParams(m[3]),
		})
	}
	return sk
}

// --- Rust extractor ---

type rustExtractor struct{}

func (e *rustExtractor) Language() string { return "rust" }

var (
	rustFnRe     = regexp.MustCompile(`(?m)^\s*(?:pub\s+)?(?:async\s+)?fn\s+(\w+)\s*(?:<[^>]*>)?\(([^)]*)\)(?:\s*->\s*(\S+))?`)
	rustStructRe = regexp.MustCompile(`(?m)^(?:pub\s+)?struct\s+(\w+)`)
	rustEnumRe   = regexp.MustCompile(`(?m)^(?:pub\s+)?enum\s+(\w+)`)
	rustTraitRe  = regexp.MustCompile(`(?m)^(?:pub\s+)?trait\s+(\w+)`)
	rustImplRe   = regexp.MustCompile(`(?m)^impl(?:<[^>]*>)?\s+(\w+)`)
	rustUseRe    = regexp.MustCompile(`(?m)^use\s+([\w:]+)`)
)

func (e *rustExtractor) Extract(source string) *CodeSkeleton {
	sk := &CodeSkeleton{Language: "rust", LineCount: countLines(source)}

	for _, m := range rustUseRe.FindAllStringSubmatch(source, -1) {
		sk.Imports = append(sk.Imports, m[1])
	}
	for _, m := range rustStructRe.FindAllStringSubmatch(source, -1) {
		sk.Classes = append(sk.Classes, ClassInfo{Name: m[1]})
	}
	for _, m := range rustEnumRe.FindAllStringSubmatch(source, -1) {
		sk.Classes = append(sk.Classes, ClassInfo{Name: m[1]})
	}
	for _, m := range rustTraitRe.FindAllStringSubmatch(source, -1) {
		sk.Interfaces = append(sk.Interfaces, m[1])
	}
	for _, m := range rustFnRe.FindAllStringSubmatch(source, -1) {
		fi := FuncInfo{Name: m[1], Params: simplifyParams(m[2])}
		if m[3] != "" {
			fi.ReturnType = m[3]
		}
		sk.Functions = append(sk.Functions, fi)
	}
	for _, m := range rustImplRe.FindAllStringSubmatch(source, -1) {
		for i := range sk.Classes {
			if sk.Classes[i].Name == m[1] {
				break
			}
		}
	}
	return sk
}

// --- Ruby extractor ---

type rubyExtractor struct{}

func (e *rubyExtractor) Language() string { return "ruby" }

var (
	rbClassRe  = regexp.MustCompile(`(?m)^class\s+(\w+)(?:\s*<\s*(\w+))?`)
	rbMethodRe = regexp.MustCompile(`(?m)^\s+def\s+(\w+)(?:\(([^)]*)\))?`)
	rbModuleRe = regexp.MustCompile(`(?m)^module\s+(\w+)`)
)

func (e *rubyExtractor) Extract(source string) *CodeSkeleton {
	sk := &CodeSkeleton{Language: "ruby", LineCount: countLines(source)}
	for _, m := range rbModuleRe.FindAllStringSubmatch(source, -1) {
		sk.Package = "module " + m[1]
	}
	for _, m := range rbClassRe.FindAllStringSubmatch(source, -1) {
		ci := ClassInfo{Name: m[1]}
		if m[2] != "" {
			ci.Extends = m[2]
		}
		sk.Classes = append(sk.Classes, ci)
	}
	for _, m := range rbMethodRe.FindAllStringSubmatch(source, -1) {
		sk.Functions = append(sk.Functions, FuncInfo{Name: m[1], Params: m[2]})
	}
	return sk
}

// --- C# extractor ---

type csharpExtractor struct{}

func (e *csharpExtractor) Language() string { return "csharp" }

var (
	csClassRe     = regexp.MustCompile(`(?m)^\s*(?:public|internal|private|protected)?\s*(?:abstract\s+|sealed\s+|static\s+|partial\s+)?class\s+(\w+)(?:\s*:\s*([^{]+))?`)
	csMethodRe    = regexp.MustCompile(`(?m)^\s+(?:public|protected|private|internal)?\s*(?:static\s+)?(?:async\s+)?(\w+(?:<[^>]+>)?)\s+(\w+)\s*\(([^)]*)\)`)
	csNamespaceRe = regexp.MustCompile(`(?m)^\s*namespace\s+([\w.]+)`)
)

func (e *csharpExtractor) Extract(source string) *CodeSkeleton {
	sk := &CodeSkeleton{Language: "csharp", LineCount: countLines(source)}
	if m := csNamespaceRe.FindStringSubmatch(source); m != nil {
		sk.Package = "namespace " + m[1]
	}
	for _, m := range csClassRe.FindAllStringSubmatch(source, -1) {
		ci := ClassInfo{Name: m[1]}
		if m[2] != "" {
			ci.Extends = strings.TrimSpace(m[2])
		}
		sk.Classes = append(sk.Classes, ci)
	}
	for _, m := range csMethodRe.FindAllStringSubmatch(source, -1) {
		sk.Functions = append(sk.Functions, FuncInfo{
			Name:       m[2],
			ReturnType: m[1],
			Params:     simplifyParams(m[3]),
		})
	}
	return sk
}

// --- Go extractor ---

type goExtractor struct{}

func (e *goExtractor) Language() string { return "go" }

var (
	goPackageRe   = regexp.MustCompile(`(?m)^package\s+(\w+)`)
	goImportRe    = regexp.MustCompile(`(?m)^\s*"([^"]+)"`)
	goFuncRe      = regexp.MustCompile(`(?m)^func\s+(\w+)\s*\(([^)]*)\)(?:\s*(\([^)]*\)|\S+))?`)
	goMethodRe    = regexp.MustCompile(`(?m)^func\s+\((\w+)\s+\*?(\w+)\)\s+(\w+)\s*\(([^)]*)\)(?:\s*(\([^)]*\)|\S+))?`)
	goStructRe    = regexp.MustCompile(`(?m)^type\s+(\w+)\s+struct\b`)
	goInterfaceRe = regexp.MustCompile(`(?m)^type\s+(\w+)\s+interface\b`)
	goConstRe     = regexp.MustCompile(`(?m)^const\s+(\w+)`)
)

func (e *goExtractor) Extract(source string) *CodeSkeleton {
	sk := &CodeSkeleton{Language: "go", LineCount: countLines(source)}

	if m := goPackageRe.FindStringSubmatch(source); m != nil {
		sk.Package = "package " + m[1]
	}

	for _, m := range goImportRe.FindAllStringSubmatch(source, -1) {
		sk.Imports = append(sk.Imports, m[1])
	}

	for _, m := range goStructRe.FindAllStringSubmatch(source, -1) {
		sk.Classes = append(sk.Classes, ClassInfo{Name: m[1]})
	}

	for _, m := range goInterfaceRe.FindAllStringSubmatch(source, -1) {
		sk.Interfaces = append(sk.Interfaces, m[1])
	}

	for _, m := range goConstRe.FindAllStringSubmatch(source, -1) {
		sk.Constants = append(sk.Constants, m[1])
	}

	for _, m := range goMethodRe.FindAllStringSubmatch(source, -1) {
		fi := FuncInfo{
			Name:       m[3],
			Receiver:   m[2],
			Params:     simplifyParams(m[4]),
			IsExported: m[3] != "" && m[3][0] >= 'A' && m[3][0] <= 'Z',
		}
		if m[5] != "" {
			fi.ReturnType = strings.TrimSpace(m[5])
		}
		sk.Functions = append(sk.Functions, fi)
	}

	for _, m := range goFuncRe.FindAllStringSubmatch(source, -1) {
		fi := FuncInfo{
			Name:       m[1],
			Params:     simplifyParams(m[2]),
			IsExported: m[1] != "" && m[1][0] >= 'A' && m[1][0] <= 'Z',
		}
		if m[3] != "" {
			fi.ReturnType = strings.TrimSpace(m[3])
		}
		sk.Functions = append(sk.Functions, fi)
	}

	return sk
}

// --- PHP extractor ---

type phpExtractor struct{}

func (e *phpExtractor) Language() string { return "php" }

var (
	phpClassRe     = regexp.MustCompile(`(?m)^\s*(?:abstract\s+|final\s+)?class\s+(\w+)(?:\s+extends\s+(\w+))?(?:\s+implements\s+([^{]+))?`)
	phpFuncRe      = regexp.MustCompile(`(?m)^\s*(?:public|protected|private)?\s*(?:static\s+)?function\s+(\w+)\s*\(([^)]*)\)(?:\s*:\s*(\S+))?`)
	phpNamespaceRe = regexp.MustCompile(`(?m)^namespace\s+([\w\\]+);`)
	phpUseRe       = regexp.MustCompile(`(?m)^use\s+([\w\\]+)`)
	phpInterfaceRe = regexp.MustCompile(`(?m)^\s*interface\s+(\w+)`)
	phpTraitRe     = regexp.MustCompile(`(?m)^\s*trait\s+(\w+)`)
)

func (e *phpExtractor) Extract(source string) *CodeSkeleton {
	sk := &CodeSkeleton{Language: "php", LineCount: countLines(source)}

	if m := phpNamespaceRe.FindStringSubmatch(source); m != nil {
		sk.Package = "namespace " + m[1]
	}

	for _, m := range phpUseRe.FindAllStringSubmatch(source, -1) {
		sk.Imports = append(sk.Imports, m[1])
	}

	for _, m := range phpClassRe.FindAllStringSubmatch(source, -1) {
		ci := ClassInfo{Name: m[1]}
		if m[2] != "" {
			ci.Extends = m[2]
		}
		if m[3] != "" {
			for _, impl := range strings.Split(m[3], ",") {
				ci.Implements = append(ci.Implements, strings.TrimSpace(impl))
			}
		}
		sk.Classes = append(sk.Classes, ci)
	}

	for _, m := range phpInterfaceRe.FindAllStringSubmatch(source, -1) {
		sk.Interfaces = append(sk.Interfaces, m[1])
	}

	for _, m := range phpTraitRe.FindAllStringSubmatch(source, -1) {
		sk.Classes = append(sk.Classes, ClassInfo{Name: m[1] + " (trait)"})
	}

	for _, m := range phpFuncRe.FindAllStringSubmatch(source, -1) {
		fi := FuncInfo{Name: m[1], Params: simplifyParams(m[2])}
		if m[3] != "" {
			fi.ReturnType = m[3]
		}
		sk.Functions = append(sk.Functions, fi)
	}

	return sk
}

// --- C/C++ extractor ---

type cppExtractor struct {
	lang string
}

func (e *cppExtractor) Language() string { return e.lang }

var (
	cppClassRe    = regexp.MustCompile(`(?m)^\s*(?:class|struct)\s+(\w+)(?:\s*:\s*(?:public|protected|private)\s+(\w+))?`)
	cppFuncRe     = regexp.MustCompile(`(?m)^(?:(?:static|inline|virtual|extern|explicit)\s+)*(\w[\w:*&<> ]*?)\s+(\w+)\s*\(([^)]*)\)`)
	cppIncludeRe  = regexp.MustCompile(`(?m)^#include\s+[<"]([^>"]+)[>"]`)
	cppNamespaceRe = regexp.MustCompile(`(?m)^namespace\s+(\w+)`)
	cppEnumRe     = regexp.MustCompile(`(?m)^\s*enum\s+(?:class\s+)?(\w+)`)
	cppTemplateRe = regexp.MustCompile(`(?m)^template\s*<`)
)

func (e *cppExtractor) Extract(source string) *CodeSkeleton {
	sk := &CodeSkeleton{Language: e.lang, LineCount: countLines(source)}

	if m := cppNamespaceRe.FindStringSubmatch(source); m != nil {
		sk.Package = "namespace " + m[1]
	}

	for _, m := range cppIncludeRe.FindAllStringSubmatch(source, -1) {
		sk.Imports = append(sk.Imports, m[1])
	}

	for _, m := range cppClassRe.FindAllStringSubmatch(source, -1) {
		ci := ClassInfo{Name: m[1]}
		if m[2] != "" {
			ci.Extends = m[2]
		}
		sk.Classes = append(sk.Classes, ci)
	}

	for _, m := range cppEnumRe.FindAllStringSubmatch(source, -1) {
		sk.Constants = append(sk.Constants, m[1])
	}

	for _, m := range cppFuncRe.FindAllStringSubmatch(source, -1) {
		name := m[2]
		if name == "if" || name == "for" || name == "while" || name == "switch" || name == "return" || name == "class" || name == "struct" {
			continue
		}
		fi := FuncInfo{
			Name:       name,
			ReturnType: strings.TrimSpace(m[1]),
			Params:     simplifyParams(m[3]),
		}
		sk.Functions = append(sk.Functions, fi)
	}

	_ = cppTemplateRe

	return sk
}

// --- helpers ---

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func simplifyParams(params string) string {
	params = strings.TrimSpace(params)
	if len(params) > 100 {
		return params[:97] + "..."
	}
	return params
}
