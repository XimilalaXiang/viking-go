package parse

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// ExcelFileParser handles .xlsx and .xls spreadsheets.
type ExcelFileParser struct{}

func (p *ExcelFileParser) Name() string { return "excel" }

func (p *ExcelFileParser) Extensions() []string { return []string{".xlsx", ".xlsm", ".xltx"} }

func (p *ExcelFileParser) Parse(source string, isPath bool) (*ParseResult, error) {
	if !isPath {
		return nil, fmt.Errorf("Excel parsing requires a file path")
	}

	content, sheets, err := extractExcelText(source)
	if err != nil {
		return nil, fmt.Errorf("extract Excel: %w", err)
	}

	title := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	abstract := fmt.Sprintf("%s (%d sheets)", title, len(sheets))

	children := make([]ParseResult, 0, len(sheets))
	for _, sh := range sheets {
		children = append(children, ParseResult{
			Title:    sh.Name,
			Abstract: fmt.Sprintf("Sheet: %s (%d rows)", sh.Name, sh.RowCount),
			Content:  sh.Content,
			Format:   "excel_sheet",
		})
	}

	return &ParseResult{
		Title:    title,
		Abstract: abstract,
		Content:  content,
		Children: children,
		Format:   "excel",
	}, nil
}

type sheetData struct {
	Name     string
	Content  string
	RowCount int
}

func extractExcelText(path string) (string, []sheetData, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	sheetNames := f.GetSheetList()
	if len(sheetNames) == 0 {
		return "", nil, fmt.Errorf("no sheets in workbook")
	}

	var fullContent strings.Builder
	var sheets []sheetData

	for _, name := range sheetNames {
		rows, err := f.GetRows(name)
		if err != nil {
			continue
		}

		var sheetContent strings.Builder
		sheetContent.WriteString(fmt.Sprintf("## %s\n\n", name))

		if len(rows) == 0 {
			sheets = append(sheets, sheetData{Name: name, Content: "(empty)", RowCount: 0})
			continue
		}

		// Convert to markdown table
		for i, row := range rows {
			sheetContent.WriteString("| ")
			for _, cell := range row {
				sheetContent.WriteString(strings.ReplaceAll(cell, "|", "\\|"))
				sheetContent.WriteString(" | ")
			}
			sheetContent.WriteString("\n")

			if i == 0 {
				sheetContent.WriteString("|")
				for range row {
					sheetContent.WriteString(" --- |")
				}
				sheetContent.WriteString("\n")
			}
		}

		sheetStr := sheetContent.String()
		sheets = append(sheets, sheetData{
			Name:     name,
			Content:  sheetStr,
			RowCount: len(rows),
		})

		if fullContent.Len() > 0 {
			fullContent.WriteString("\n\n")
		}
		fullContent.WriteString(sheetStr)
	}

	return fullContent.String(), sheets, nil
}
