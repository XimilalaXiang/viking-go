package memory

import (
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/storage"
)

// ArchiveConfig configures the memory archiver behavior.
type ArchiveConfig struct {
	HotnessThreshold float64
	MaxAgeDays       int
	HalfLifeDays     float64
	ScanLimit        int
}

// DefaultArchiveConfig returns sensible defaults for the archiver.
func DefaultArchiveConfig() ArchiveConfig {
	return ArchiveConfig{
		HotnessThreshold: 0.1,
		MaxAgeDays:       90,
		HalfLifeDays:     7.0,
		ScanLimit:        500,
	}
}

// Archiver moves cold memories to an archive directory to reduce retrieval costs.
type Archiver struct {
	store  *storage.Store
	config ArchiveConfig
}

// NewArchiver creates a new memory archiver.
func NewArchiver(store *storage.Store, cfg ArchiveConfig) *Archiver {
	return &Archiver{store: store, config: cfg}
}

// ArchiveResult summarizes one archival run.
type ArchiveResult struct {
	Scanned  int `json:"scanned"`
	Archived int `json:"archived"`
	Errors   int `json:"errors"`
}

// ScanAndArchive identifies cold memories and marks them as archived.
func (a *Archiver) ScanAndArchive(reqCtx *ctx.RequestContext) (*ArchiveResult, error) {
	now := time.Now().UTC()
	result := &ArchiveResult{}

	filter := storage.Eq{Field: "context_type", Value: "memory"}
	if reqCtx != nil && reqCtx.AccountID != "" && reqCtx.Role != ctx.RoleRoot {
		filter = storage.Eq{Field: "context_type", Value: "memory"}
	}

	memories, err := a.store.ListByFilter(filter, a.config.ScanLimit)
	if err != nil {
		return nil, fmt.Errorf("scan memories: %w", err)
	}

	result.Scanned = len(memories)

	for _, mem := range memories {
		score := HotnessScore(mem.ActiveCount, mem.UpdatedAt, now, a.config.HalfLifeDays)

		if score >= a.config.HotnessThreshold {
			continue
		}

		ageDays := now.Sub(mem.UpdatedAt).Hours() / 24
		if ageDays < float64(a.config.MaxAgeDays) && score > 0.05 {
			continue
		}

		if err := a.archiveMemory(mem); err != nil {
			log.Printf("Warning: archive %s: %v", mem.URI, err)
			result.Errors++
			continue
		}
		result.Archived++
	}

	log.Printf("[Archiver] scanned=%d archived=%d errors=%d", result.Scanned, result.Archived, result.Errors)
	return result, nil
}

func (a *Archiver) archiveMemory(mem *ctx.Context) error {
	archiveURI := toArchiveURI(mem.URI)
	mem.URI = archiveURI

	if mem.Meta == nil {
		mem.Meta = make(map[string]any)
	}
	mem.Meta["archived_at"] = time.Now().UTC().Format(time.RFC3339)
	mem.Meta["archived_from"] = mem.URI

	return a.store.Upsert(mem)
}

// RestoreMemory moves a memory from archive back to active.
func (a *Archiver) RestoreMemory(archiveURI string) error {
	activeURI := fromArchiveURI(archiveURI)
	if activeURI == archiveURI {
		return fmt.Errorf("URI is not an archived memory")
	}

	mem, err := a.store.GetByURI(activeURI)
	if err == nil && mem != nil {
		return fmt.Errorf("active memory already exists at %s", activeURI)
	}

	archived, err := a.store.GetByURI(archiveURI)
	if err != nil || archived == nil {
		return fmt.Errorf("archived memory not found: %s", archiveURI)
	}

	archived.URI = activeURI
	delete(archived.Meta, "archived_at")
	delete(archived.Meta, "archived_from")
	archived.ActiveCount++
	archived.UpdatedAt = time.Now().UTC()

	return a.store.Upsert(archived)
}

// HotnessScore computes a 0.0-1.0 hotness score for a memory.
// Formula: sigmoid(log1p(active_count)) × exp_decay(age, half_life)
func HotnessScore(activeCount int, updatedAt time.Time, now time.Time, halfLifeDays float64) float64 {
	if updatedAt.IsZero() {
		return 0.0
	}
	if halfLifeDays <= 0 {
		halfLifeDays = 7.0
	}

	freq := 1.0 / (1.0 + math.Exp(-math.Log1p(float64(activeCount))))

	ageDays := math.Max(now.Sub(updatedAt).Hours()/24.0, 0.0)
	decay := math.Exp(-ageDays * math.Ln2 / halfLifeDays)

	return freq * decay
}

func toArchiveURI(uri string) string {
	parts := strings.Split(uri, "/")
	for i, p := range parts {
		if p == "memories" && i+1 < len(parts) {
			parts = append(parts[:i+1], append([]string{"_archive"}, parts[i+1:]...)...)
			return strings.Join(parts, "/")
		}
	}
	return uri + "/_archive"
}

func fromArchiveURI(uri string) string {
	return strings.Replace(uri, "/memories/_archive/", "/memories/", 1)
}
