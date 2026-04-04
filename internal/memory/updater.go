package memory

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/memory/mergeop"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

// MemoryOperation represents a single operation (write/edit/delete) on a memory.
type MemoryOperation struct {
	Type       string         `json:"type"` // "write", "edit", "delete"
	URI        string         `json:"uri"`
	MemoryType string         `json:"memory_type,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
	Content    string         `json:"content,omitempty"`
}

// MemoryOperations is the structured output from the LLM's memory extraction.
type MemoryOperations struct {
	Reasoning     string            `json:"reasoning"`
	WriteOps      []MemoryOperation `json:"write_ops"`
	EditOps       []MemoryOperation `json:"edit_ops"`
	EditOverviews []MemoryOperation `json:"edit_overview_ops"`
	DeleteURIs    []string          `json:"delete_uris"`
}

func (ops *MemoryOperations) IsEmpty() bool {
	return len(ops.WriteOps) == 0 && len(ops.EditOps) == 0 &&
		len(ops.EditOverviews) == 0 && len(ops.DeleteURIs) == 0
}

// UpdateResult tracks the outcome of applying memory operations.
type UpdateResult struct {
	WrittenURIs []string           `json:"written_uris"`
	EditedURIs  []string           `json:"edited_uris"`
	DeletedURIs []string           `json:"deleted_uris"`
	Errors      []OperationError   `json:"errors,omitempty"`
}

type OperationError struct {
	URI     string `json:"uri"`
	Message string `json:"message"`
}

func (r *UpdateResult) HasChanges() bool {
	return len(r.WrittenURIs) > 0 || len(r.EditedURIs) > 0 || len(r.DeletedURIs) > 0
}

// MemoryUpdater applies MemoryOperations to VikingFS using schema-driven merge operations.
type MemoryUpdater struct {
	registry *MemoryTypeRegistry
	vfs      *vikingfs.VikingFS
}

// NewMemoryUpdater creates a new updater.
func NewMemoryUpdater(registry *MemoryTypeRegistry, vfs *vikingfs.VikingFS) *MemoryUpdater {
	return &MemoryUpdater{registry: registry, vfs: vfs}
}

// ApplyOperations executes all operations in the given MemoryOperations.
func (u *MemoryUpdater) ApplyOperations(
	ops *MemoryOperations,
	reqCtx *ctx.RequestContext,
) *UpdateResult {
	result := &UpdateResult{}

	for _, op := range ops.WriteOps {
		if err := u.applyWrite(op, reqCtx); err != nil {
			log.Printf("[MemoryUpdater] write %s failed: %v", op.URI, err)
			result.Errors = append(result.Errors, OperationError{URI: op.URI, Message: err.Error()})
		} else {
			result.WrittenURIs = append(result.WrittenURIs, op.URI)
		}
	}

	for _, op := range ops.EditOps {
		if err := u.applyEdit(op, reqCtx); err != nil {
			log.Printf("[MemoryUpdater] edit %s failed: %v", op.URI, err)
			result.Errors = append(result.Errors, OperationError{URI: op.URI, Message: err.Error()})
		} else {
			result.EditedURIs = append(result.EditedURIs, op.URI)
		}
	}

	for _, op := range ops.EditOverviews {
		if err := u.applyEditOverview(op, reqCtx); err != nil {
			log.Printf("[MemoryUpdater] edit-overview %s failed: %v", op.URI, err)
			result.Errors = append(result.Errors, OperationError{URI: op.URI, Message: err.Error()})
		} else {
			result.EditedURIs = append(result.EditedURIs, op.URI)
		}
	}

	for _, uri := range ops.DeleteURIs {
		if err := u.vfs.Rm(uri, false, reqCtx); err != nil {
			log.Printf("[MemoryUpdater] delete %s failed: %v", uri, err)
			result.Errors = append(result.Errors, OperationError{URI: uri, Message: err.Error()})
		} else {
			result.DeletedURIs = append(result.DeletedURIs, uri)
		}
	}

	return result
}

func (u *MemoryUpdater) applyWrite(op MemoryOperation, reqCtx *ctx.RequestContext) error {
	content := op.Content
	metadata := make(map[string]any)

	if u.registry != nil && op.MemoryType != "" {
		fieldMap := u.registry.FieldSchemaMap(op.MemoryType)
		for fieldName := range fieldMap {
			if v, ok := op.Fields[fieldName]; ok {
				metadata[fieldName] = v
			}
		}
	} else {
		for k, v := range op.Fields {
			if k != "content" {
				metadata[k] = v
			}
		}
	}

	fullContent := serializeWithMetadata(content, metadata)
	return u.vfs.WriteString(op.URI, fullContent, reqCtx)
}

func (u *MemoryUpdater) applyEdit(op MemoryOperation, reqCtx *ctx.RequestContext) error {
	currentFull, err := u.vfs.ReadFile(op.URI, reqCtx)
	if err != nil {
		return u.applyWrite(op, reqCtx)
	}

	currentContent, currentMeta := deserializeFull(currentFull)

	fieldMap := u.registry.FieldSchemaMap(op.MemoryType)
	newContent := currentContent

	for fieldName, fieldSchema := range fieldMap {
		patchValue, hasPatch := op.Fields[fieldName]
		if !hasPatch {
			continue
		}

		mop := mergeop.NewMergeOp(fieldSchema.MergeOp, fieldSchema.FieldType)

		if fieldName == "content" {
			newContent, _ = mop.Apply(currentContent, patchValue).(string)
		} else {
			currentMeta[fieldName] = mop.Apply(currentMeta[fieldName], patchValue)
		}
	}

	if _, hasContent := op.Fields["content"]; hasContent && fieldMap["content"] == nil {
		patchOp := mergeop.NewPatchOp(mergeop.FieldString)
		newContent, _ = patchOp.Apply(currentContent, op.Fields["content"]).(string)
	}

	fullContent := serializeWithMetadata(newContent, currentMeta)
	return u.vfs.WriteString(op.URI, fullContent, reqCtx)
}

func (u *MemoryUpdater) applyEditOverview(op MemoryOperation, reqCtx *ctx.RequestContext) error {
	overviewValue, ok := op.Fields["overview"]
	if !ok {
		return nil
	}

	current, _ := u.vfs.ReadFile(op.URI, reqCtx)
	patchOp := mergeop.NewPatchOp(mergeop.FieldString)
	newOverview, _ := patchOp.Apply(current, overviewValue).(string)
	return u.vfs.WriteString(op.URI, newOverview, reqCtx)
}

// --- serialization helpers ---

const metadataMarker = "<!-- MEMORY_FIELDS"
const metadataEnd = "-->"

func serializeWithMetadata(content string, metadata map[string]any) string {
	if len(metadata) == 0 {
		return content
	}
	metaJSON, _ := json.Marshal(metadata)
	return fmt.Sprintf("%s %s %s\n%s", metadataMarker, string(metaJSON), metadataEnd, content)
}

func deserializeFull(full string) (content string, metadata map[string]any) {
	metadata = make(map[string]any)
	if !strings.HasPrefix(full, metadataMarker) {
		return full, metadata
	}

	endIdx := strings.Index(full, metadataEnd)
	if endIdx < 0 {
		return full, metadata
	}

	jsonStr := strings.TrimSpace(full[len(metadataMarker):endIdx])
	rest := full[endIdx+len(metadataEnd):]
	content = strings.TrimPrefix(rest, "\n")

	json.Unmarshal([]byte(jsonStr), &metadata)
	return content, metadata
}
