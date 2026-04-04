package mergeop

import (
	"testing"
)

func TestPatchOpString_FullReplace(t *testing.T) {
	op := NewPatchOp(FieldString)
	result := op.Apply("old content", "new content")
	if result != "new content" {
		t.Errorf("got %v, want 'new content'", result)
	}
}

func TestPatchOpString_EmptyPatchKeepsCurrent(t *testing.T) {
	op := NewPatchOp(FieldString)
	result := op.Apply("keep me", "")
	if result != "keep me" {
		t.Errorf("got %v, want 'keep me'", result)
	}
}

func TestPatchOpString_NilPatchKeepsCurrent(t *testing.T) {
	op := NewPatchOp(FieldString)
	result := op.Apply("keep me", nil)
	if result != "keep me" {
		t.Errorf("got %v, want 'keep me'", result)
	}
}

func TestPatchOpString_StrPatch(t *testing.T) {
	op := NewPatchOp(FieldString)
	patch := &StrPatch{
		Blocks: []SearchReplaceBlock{
			{Search: "world", Replace: "Go"},
		},
	}
	result := op.Apply("hello world", patch)
	if result != "hello Go" {
		t.Errorf("got %v, want 'hello Go'", result)
	}
}

func TestPatchOpString_StrPatchMultiBlock(t *testing.T) {
	op := NewPatchOp(FieldString)
	patch := &StrPatch{
		Blocks: []SearchReplaceBlock{
			{Search: "foo", Replace: "bar"},
			{Search: "baz", Replace: "qux"},
		},
	}
	result := op.Apply("foo baz", patch)
	if result != "bar qux" {
		t.Errorf("got %v, want 'bar qux'", result)
	}
}

func TestPatchOpString_StrPatchAppend(t *testing.T) {
	op := NewPatchOp(FieldString)
	patch := &StrPatch{
		Blocks: []SearchReplaceBlock{
			{Search: "", Replace: "appended line"},
		},
	}
	result := op.Apply("existing", patch)
	if result != "existing\nappended line" {
		t.Errorf("got %v", result)
	}
}

func TestPatchOpString_MapPatch(t *testing.T) {
	op := NewPatchOp(FieldString)
	m := map[string]any{
		"blocks": []any{
			map[string]any{"search": "old", "replace": "new"},
		},
	}
	result := op.Apply("this is old text", m)
	if result != "this is new text" {
		t.Errorf("got %v, want 'this is new text'", result)
	}
}

func TestPatchOpNonString(t *testing.T) {
	op := NewPatchOp(FieldInt64)
	result := op.Apply(42, 99)
	if result != 99 {
		t.Errorf("got %v, want 99", result)
	}
}

func TestImmutableOp_FirstSet(t *testing.T) {
	op := NewImmutableOp()
	result := op.Apply(nil, "initial")
	if result != "initial" {
		t.Errorf("got %v, want 'initial'", result)
	}
}

func TestImmutableOp_AlreadySet(t *testing.T) {
	op := NewImmutableOp()
	result := op.Apply("existing", "new value")
	if result != "existing" {
		t.Errorf("got %v, want 'existing'", result)
	}
}

func TestImmutableOp_EmptyStringTreatedAsUnset(t *testing.T) {
	op := NewImmutableOp()
	result := op.Apply("", "initial")
	if result != "initial" {
		t.Errorf("got %v, want 'initial'", result)
	}
}

func TestSumOp_Integers(t *testing.T) {
	op := NewSumOp()
	result := op.Apply(int64(10), int64(5))
	if result != int64(15) {
		t.Errorf("got %v (type %T), want int64(15)", result, result)
	}
}

func TestSumOp_Floats(t *testing.T) {
	op := NewSumOp()
	result := op.Apply(float64(1.5), float64(2.5))
	if result != float64(4.0) {
		t.Errorf("got %v, want 4.0", result)
	}
}

func TestSumOp_MixedTypes(t *testing.T) {
	op := NewSumOp()
	result := op.Apply(int64(3), float64(1.5))
	if result != float64(4.5) {
		t.Errorf("got %v, want 4.5", result)
	}
}

func TestSumOp_NilPatch(t *testing.T) {
	op := NewSumOp()
	result := op.Apply(int64(10), nil)
	if result != int64(10) {
		t.Errorf("got %v, want 10", result)
	}
}

func TestSumOp_NilCurrent(t *testing.T) {
	op := NewSumOp()
	result := op.Apply(nil, int64(5))
	if result != int64(5) {
		t.Errorf("got %v, want 5", result)
	}
}

func TestNewMergeOp_Patch(t *testing.T) {
	op := NewMergeOp(OpPatch, FieldString)
	if op.Type() != OpPatch {
		t.Errorf("type = %v", op.Type())
	}
}

func TestNewMergeOp_Immutable(t *testing.T) {
	op := NewMergeOp(OpImmutable, FieldString)
	if op.Type() != OpImmutable {
		t.Errorf("type = %v", op.Type())
	}
}

func TestNewMergeOp_Sum(t *testing.T) {
	op := NewMergeOp(OpSum, FieldInt64)
	if op.Type() != OpSum {
		t.Errorf("type = %v", op.Type())
	}
}
