package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RecordedQuery captures a single query-response interaction during live usage.
type RecordedQuery struct {
	ID            string    `json:"id"`
	Query         string    `json:"query"`
	Timestamp     time.Time `json:"timestamp"`
	RetrievedURIs []string  `json:"retrieved_uris"`
	RetrievedTexts []string `json:"retrieved_texts,omitempty"`
	Scores        []float64 `json:"scores,omitempty"`
	LatencyMs     int64     `json:"latency_ms"`
	SessionID     string    `json:"session_id,omitempty"`
}

// Recording is a collection of recorded queries from a live session.
type Recording struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	Queries     []RecordedQuery `json:"queries"`
}

// Recorder captures retrieval queries and results during live usage.
// It can later be used to generate evaluation datasets or replay interactions.
type Recorder struct {
	mu        sync.Mutex
	recording Recording
	active    bool
	outDir    string
	seqNo     int
}

// NewRecorder creates a recorder that will save recordings to the given directory.
func NewRecorder(outDir string) *Recorder {
	os.MkdirAll(outDir, 0755)
	return &Recorder{outDir: outDir}
}

// Start begins a new recording session.
func (r *Recorder) Start(name, description string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seqNo++
	r.recording = Recording{
		ID:          fmt.Sprintf("rec_%d_%d", time.Now().UnixMilli(), r.seqNo),
		Name:        name,
		Description: description,
		CreatedAt:   time.Now().UTC(),
	}
	r.active = true
}

// Stop ends the recording and saves it to disk.
func (r *Recorder) Stop() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return "", fmt.Errorf("no active recording")
	}
	r.active = false

	path := filepath.Join(r.outDir, r.recording.ID+".json")
	data, err := json.MarshalIndent(r.recording, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal recording: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("write recording: %w", err)
	}
	return path, nil
}

// IsActive returns true if a recording is in progress.
func (r *Recorder) IsActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// Record captures a query and its results.
func (r *Recorder) Record(query RecordedQuery) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return
	}
	if query.Timestamp.IsZero() {
		query.Timestamp = time.Now().UTC()
	}
	if query.ID == "" {
		query.ID = fmt.Sprintf("q_%d_%d", time.Now().UnixMilli(), len(r.recording.Queries))
	}
	r.recording.Queries = append(r.recording.Queries, query)
}

// QueryCount returns the number of recorded queries.
func (r *Recorder) QueryCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.recording.Queries)
}

// LoadRecording reads a saved recording from disk.
func LoadRecording(path string) (*Recording, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read recording: %w", err)
	}
	var rec Recording
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parse recording: %w", err)
	}
	return &rec, nil
}

// ListRecordings lists all recordings in the given directory.
func ListRecordings(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	return paths, nil
}

// RecordingToDataset converts a recording into an evaluation dataset.
// relevanceJudger is called for each query to determine which retrieved docs
// are relevant (ground truth). If nil, all retrieved docs are assumed relevant.
func RecordingToDataset(rec *Recording, relevanceJudger func(query string, doc string) bool) *Dataset {
	ds := &Dataset{
		Name:        rec.Name + " (from recording)",
		Description: fmt.Sprintf("Auto-generated from recording %s on %s", rec.ID, rec.CreatedAt.Format(time.RFC3339)),
	}

	for _, q := range rec.Queries {
		sample := Sample{
			ID:            q.ID,
			Query:         q.Query,
			RetrievedDocs: q.RetrievedURIs,
			RetrievedScores: q.Scores,
		}

		if relevanceJudger != nil {
			var expected []string
			for _, uri := range q.RetrievedURIs {
				if relevanceJudger(q.Query, uri) {
					expected = append(expected, uri)
				}
			}
			sample.ExpectedDocs = expected
		} else {
			sample.ExpectedDocs = q.RetrievedURIs
		}

		ds.Samples = append(ds.Samples, sample)
	}

	return ds
}

// GenerateTestDataset creates a synthetic dataset from recorded patterns.
// It takes the top-performing queries (by number of results) and uses them
// as the evaluation baseline.
func GenerateTestDataset(recordings []*Recording, maxSamples int) *Dataset {
	ds := &Dataset{
		Name:        "auto-generated-eval",
		Description: fmt.Sprintf("Auto-generated from %d recordings", len(recordings)),
	}

	seen := make(map[string]bool)
	for _, rec := range recordings {
		for _, q := range rec.Queries {
			if seen[q.Query] || len(q.RetrievedURIs) == 0 {
				continue
			}
			seen[q.Query] = true

			ds.Samples = append(ds.Samples, Sample{
				ID:            q.ID,
				Query:         q.Query,
				ExpectedDocs:  q.RetrievedURIs,
				RetrievedDocs: q.RetrievedURIs,
				RetrievedScores: q.Scores,
			})

			if maxSamples > 0 && len(ds.Samples) >= maxSamples {
				break
			}
		}
		if maxSamples > 0 && len(ds.Samples) >= maxSamples {
			break
		}
	}

	return ds
}
