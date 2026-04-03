package storage

import (
	"fmt"
	"strings"
)

// FilterExpr represents a composable filter expression that compiles to SQL.
type FilterExpr interface {
	ToSQL() (clause string, args []any)
}

// Eq matches a field equal to a value.
type Eq struct {
	Field string
	Value any
}

func (e Eq) ToSQL() (string, []any) {
	return fmt.Sprintf("%s = ?", e.Field), []any{e.Value}
}

// In matches a field against a set of values.
// For URI prefix matching (when Field is "uri" and values contain "/"),
// this generates LIKE clauses.
type In struct {
	Field  string
	Values []any
}

func (e In) ToSQL() (string, []any) {
	if len(e.Values) == 0 {
		return "1 = 0", nil
	}
	if len(e.Values) == 1 {
		return fmt.Sprintf("%s = ?", e.Field), e.Values
	}
	placeholders := make([]string, len(e.Values))
	for i := range e.Values {
		placeholders[i] = "?"
	}
	return fmt.Sprintf("%s IN (%s)", e.Field, strings.Join(placeholders, ", ")), e.Values
}

// And combines multiple filters with AND.
type And struct {
	Filters []FilterExpr
}

func (a And) ToSQL() (string, []any) {
	if len(a.Filters) == 0 {
		return "1 = 1", nil
	}
	clauses := make([]string, 0, len(a.Filters))
	args := make([]any, 0)
	for _, f := range a.Filters {
		if f == nil {
			continue
		}
		c, a2 := f.ToSQL()
		if c != "" {
			clauses = append(clauses, "("+c+")")
			args = append(args, a2...)
		}
	}
	if len(clauses) == 0 {
		return "1 = 1", nil
	}
	return strings.Join(clauses, " AND "), args
}

// Or combines multiple filters with OR.
type Or struct {
	Filters []FilterExpr
}

func (o Or) ToSQL() (string, []any) {
	if len(o.Filters) == 0 {
		return "1 = 0", nil
	}
	clauses := make([]string, 0, len(o.Filters))
	args := make([]any, 0)
	for _, f := range o.Filters {
		if f == nil {
			continue
		}
		c, a2 := f.ToSQL()
		if c != "" {
			clauses = append(clauses, "("+c+")")
			args = append(args, a2...)
		}
	}
	if len(clauses) == 0 {
		return "1 = 0", nil
	}
	return strings.Join(clauses, " OR "), args
}

// PathScope matches URIs within a given prefix path.
// depth=0 means exact match, depth=1 means direct children, depth=-1 means all descendants.
type PathScope struct {
	Field    string
	BasePath string
	Depth    int
}

func (p PathScope) ToSQL() (string, []any) {
	base := strings.TrimRight(p.BasePath, "/")
	switch p.Depth {
	case 0:
		return fmt.Sprintf("%s = ?", p.Field), []any{base}
	case 1:
		// Direct children: match base/X but not base/X/Y
		prefix := base + "/"
		return fmt.Sprintf("(%s LIKE ? AND %s NOT LIKE ?)", p.Field, p.Field),
			[]any{prefix + "%", prefix + "%/%"}
	default:
		// All descendants
		prefix := base + "/"
		return fmt.Sprintf("(%s = ? OR %s LIKE ?)", p.Field, p.Field),
			[]any{base, prefix + "%"}
	}
}

// MergeFilters combines non-nil filters with AND.
func MergeFilters(filters ...FilterExpr) FilterExpr {
	var nonEmpty []FilterExpr
	for _, f := range filters {
		if f != nil {
			nonEmpty = append(nonEmpty, f)
		}
	}
	if len(nonEmpty) == 0 {
		return nil
	}
	if len(nonEmpty) == 1 {
		return nonEmpty[0]
	}
	return And{Filters: nonEmpty}
}
