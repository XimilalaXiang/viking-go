package eval

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecorderStartStopRecord(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(dir)

	if rec.IsActive() {
		t.Error("recorder should not be active initially")
	}

	rec.Start("test-session", "unit test recording")
	if !rec.IsActive() {
		t.Error("recorder should be active after Start")
	}

	rec.Record(RecordedQuery{
		Query:         "what is Go?",
		RetrievedURIs: []string{"viking://resources/golang.md", "viking://resources/intro.md"},
		Scores:        []float64{0.95, 0.82},
		LatencyMs:     42,
	})
	rec.Record(RecordedQuery{
		Query:         "explain garbage collection",
		RetrievedURIs: []string{"viking://resources/gc.md"},
		Scores:        []float64{0.88},
		LatencyMs:     37,
	})

	if rec.QueryCount() != 2 {
		t.Errorf("query count = %d, want 2", rec.QueryCount())
	}

	path, err := rec.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if rec.IsActive() {
		t.Error("recorder should not be active after Stop")
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("recording file not found: %v", err)
	}

	loaded, err := LoadRecording(path)
	if err != nil {
		t.Fatalf("LoadRecording: %v", err)
	}
	if len(loaded.Queries) != 2 {
		t.Errorf("loaded queries = %d, want 2", len(loaded.Queries))
	}
	if loaded.Name != "test-session" {
		t.Errorf("name = %s, want test-session", loaded.Name)
	}
}

func TestRecorderNotActive(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(dir)

	rec.Record(RecordedQuery{Query: "should be ignored"})
	if rec.QueryCount() != 0 {
		t.Error("recording to inactive recorder should be no-op")
	}

	_, err := rec.Stop()
	if err == nil {
		t.Error("Stop on inactive recorder should return error")
	}
}

func TestListRecordings(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(dir)

	rec.Start("r1", "")
	rec.Record(RecordedQuery{Query: "q1"})
	rec.Stop()

	rec.Start("r2", "")
	rec.Record(RecordedQuery{Query: "q2"})
	rec.Stop()

	paths, err := ListRecordings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Errorf("listing = %d, want 2", len(paths))
	}
}

func TestRecordingToDataset(t *testing.T) {
	rec := &Recording{
		ID:        "test",
		Name:      "test",
		CreatedAt: time.Now(),
		Queries: []RecordedQuery{
			{
				ID:            "q1",
				Query:         "what is Go",
				RetrievedURIs: []string{"a.md", "b.md", "c.md"},
				Scores:        []float64{0.9, 0.7, 0.3},
			},
			{
				ID:            "q2",
				Query:         "rust programming",
				RetrievedURIs: []string{"rust.md"},
				Scores:        []float64{0.85},
			},
		},
	}

	// Without judger — all retrieved docs are expected
	ds := RecordingToDataset(rec, nil)
	if len(ds.Samples) != 2 {
		t.Fatalf("samples = %d, want 2", len(ds.Samples))
	}
	if len(ds.Samples[0].ExpectedDocs) != 3 {
		t.Errorf("expected docs = %d, want 3", len(ds.Samples[0].ExpectedDocs))
	}

	// With judger — only score > 0.5 is relevant
	ds2 := RecordingToDataset(rec, func(query, doc string) bool {
		for i, uri := range rec.Queries[0].RetrievedURIs {
			if uri == doc && len(rec.Queries[0].Scores) > i {
				return rec.Queries[0].Scores[i] > 0.5
			}
		}
		return true
	})
	if len(ds2.Samples) != 2 {
		t.Fatalf("samples = %d", len(ds2.Samples))
	}
}

func TestGenerateTestDataset(t *testing.T) {
	recordings := []*Recording{
		{
			Queries: []RecordedQuery{
				{ID: "q1", Query: "go lang", RetrievedURIs: []string{"go.md"}},
				{ID: "q2", Query: "rust", RetrievedURIs: []string{"rust.md"}},
				{ID: "q3", Query: "empty", RetrievedURIs: nil}, // no results, should be skipped
			},
		},
		{
			Queries: []RecordedQuery{
				{ID: "q4", Query: "go lang", RetrievedURIs: []string{"go2.md"}}, // duplicate query, skipped
				{ID: "q5", Query: "python", RetrievedURIs: []string{"py.md"}},
			},
		},
	}

	ds := GenerateTestDataset(recordings, 10)
	if len(ds.Samples) != 3 { // go lang, rust, python (empty skipped, dup skipped)
		t.Errorf("samples = %d, want 3", len(ds.Samples))
	}

	// Test with max limit
	ds2 := GenerateTestDataset(recordings, 2)
	if len(ds2.Samples) != 2 {
		t.Errorf("limited samples = %d, want 2", len(ds2.Samples))
	}
}

func TestRecorderAutoIDs(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(dir)
	rec.Start("auto-id-test", "")

	rec.Record(RecordedQuery{Query: "test query"})

	path, _ := rec.Stop()
	loaded, _ := LoadRecording(path)

	if loaded.Queries[0].ID == "" {
		t.Error("auto-generated ID should not be empty")
	}
	if loaded.Queries[0].Timestamp.IsZero() {
		t.Error("auto-generated timestamp should not be zero")
	}
}

func TestRecordingFilePath(t *testing.T) {
	dir := t.TempDir()
	rec := NewRecorder(dir)
	rec.Start("path-test", "")
	rec.Record(RecordedQuery{Query: "q"})
	path, _ := rec.Stop()

	if filepath.Dir(path) != dir {
		t.Errorf("recording should be in %s, got %s", dir, filepath.Dir(path))
	}
	if filepath.Ext(path) != ".json" {
		t.Errorf("extension should be .json, got %s", filepath.Ext(path))
	}
}
