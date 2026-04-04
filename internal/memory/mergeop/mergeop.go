package mergeop

import (
	"fmt"
	"strconv"
	"strings"
)

// FieldType enumerates the supported field types for memory schemas.
type FieldType string

const (
	FieldString  FieldType = "string"
	FieldInt64   FieldType = "int64"
	FieldFloat32 FieldType = "float32"
	FieldBool    FieldType = "bool"
)

// MergeOpType enumerates the supported merge operation types.
type MergeOpType string

const (
	OpPatch     MergeOpType = "patch"
	OpSum       MergeOpType = "sum"
	OpImmutable MergeOpType = "immutable"
)

// SearchReplaceBlock represents a single SEARCH/REPLACE block for string patches.
type SearchReplaceBlock struct {
	Search    string `json:"search" yaml:"search"`
	Replace   string `json:"replace" yaml:"replace"`
	StartLine *int   `json:"start_line,omitempty" yaml:"start_line,omitempty"`
}

// StrPatch contains multiple SEARCH/REPLACE blocks for patching string content.
type StrPatch struct {
	Blocks []SearchReplaceBlock `json:"blocks" yaml:"blocks"`
}

// MergeOp is the interface that all merge operations implement.
type MergeOp interface {
	Type() MergeOpType
	Apply(currentValue, patchValue any) any
}

// PatchOp replaces the value directly for non-string fields,
// and applies SEARCH/REPLACE blocks for string fields.
type PatchOp struct {
	fieldType FieldType
}

func NewPatchOp(ft FieldType) *PatchOp {
	return &PatchOp{fieldType: ft}
}

func (p *PatchOp) Type() MergeOpType { return OpPatch }

func (p *PatchOp) Apply(currentValue, patchValue any) any {
	if p.fieldType != FieldString {
		return patchValue
	}

	currentStr, _ := currentValue.(string)

	switch v := patchValue.(type) {
	case *StrPatch:
		return applyStrPatch(currentStr, v)
	case StrPatch:
		return applyStrPatch(currentStr, &v)
	case map[string]any:
		if blocksRaw, ok := v["blocks"]; ok {
			sp := parseStrPatchFromMap(blocksRaw)
			if sp != nil {
				return applyStrPatch(currentStr, sp)
			}
		}
		return fmt.Sprintf("%v", patchValue)
	case string:
		if v == "" {
			return currentStr
		}
		return v
	case nil:
		return currentStr
	default:
		return fmt.Sprintf("%v", patchValue)
	}
}

// ImmutableOp keeps the current value if already set; only assigns on first write.
type ImmutableOp struct{}

func NewImmutableOp() *ImmutableOp { return &ImmutableOp{} }

func (i *ImmutableOp) Type() MergeOpType { return OpImmutable }

func (i *ImmutableOp) Apply(currentValue, patchValue any) any {
	if currentValue == nil || currentValue == "" {
		return patchValue
	}
	return currentValue
}

// SumOp adds numeric values together.
type SumOp struct{}

func NewSumOp() *SumOp { return &SumOp{} }

func (s *SumOp) Type() MergeOpType { return OpSum }

func (s *SumOp) Apply(currentValue, patchValue any) any {
	if patchValue == nil || patchValue == "" {
		return currentValue
	}
	if currentValue == nil {
		return patchValue
	}

	cf, cok := toFloat64(currentValue)
	pf, pok := toFloat64(patchValue)
	if cok && pok {
		result := cf + pf
		if isIntLike(currentValue) && isIntLike(patchValue) {
			return int64(result)
		}
		return result
	}
	return currentValue
}

// NewMergeOp creates a MergeOp from the given type and field type.
func NewMergeOp(opType MergeOpType, fieldType FieldType) MergeOp {
	switch opType {
	case OpImmutable:
		return NewImmutableOp()
	case OpSum:
		return NewSumOp()
	default:
		return NewPatchOp(fieldType)
	}
}

// --- helpers ---

func applyStrPatch(current string, patch *StrPatch) string {
	if patch == nil || len(patch.Blocks) == 0 {
		return current
	}
	result := current
	for _, block := range patch.Blocks {
		if block.Search == "" {
			if result == "" {
				result = block.Replace
			} else {
				result += "\n" + block.Replace
			}
			continue
		}
		if idx := strings.Index(result, block.Search); idx >= 0 {
			result = result[:idx] + block.Replace + result[idx+len(block.Search):]
		}
	}
	return result
}

func parseStrPatchFromMap(blocksRaw any) *StrPatch {
	blocks, ok := blocksRaw.([]any)
	if !ok {
		return nil
	}
	sp := &StrPatch{}
	for _, b := range blocks {
		bm, ok := b.(map[string]any)
		if !ok {
			continue
		}
		srb := SearchReplaceBlock{}
		if s, ok := bm["search"].(string); ok {
			srb.Search = s
		}
		if r, ok := bm["replace"].(string); ok {
			srb.Replace = r
		}
		sp.Blocks = append(sp.Blocks, srb)
	}
	return sp
}

func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func isIntLike(v any) bool {
	switch v.(type) {
	case int, int64, int32:
		return true
	default:
		return false
	}
}
