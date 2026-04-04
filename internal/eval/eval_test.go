package eval

import (
	"math"
	"testing"
)

func approx(a, b, eps float64) bool {
	return math.Abs(a-b) < eps
}

func TestPrecisionAtK(t *testing.T) {
	retrieved := []string{"doc1", "doc2", "doc3", "doc4", "doc5"}
	expected := []string{"doc1", "doc3", "doc6"}

	p := PrecisionAtK(retrieved, expected, 5)
	if !approx(p, 2.0/5.0, 0.001) {
		t.Errorf("Precision@5 = %f, want 0.4", p)
	}

	p3 := PrecisionAtK(retrieved, expected, 3)
	if !approx(p3, 2.0/3.0, 0.001) {
		t.Errorf("Precision@3 = %f, want 0.667", p3)
	}
}

func TestRecallAtK(t *testing.T) {
	retrieved := []string{"doc1", "doc2", "doc3", "doc4", "doc5"}
	expected := []string{"doc1", "doc3", "doc6"}

	r := RecallAtK(retrieved, expected, 5)
	if !approx(r, 2.0/3.0, 0.001) {
		t.Errorf("Recall@5 = %f, want 0.667", r)
	}
}

func TestMRR(t *testing.T) {
	retrieved := []string{"doc2", "doc1", "doc3"}
	expected := []string{"doc1", "doc3"}

	mrr := MRR(retrieved, expected)
	if !approx(mrr, 0.5, 0.001) {
		t.Errorf("MRR = %f, want 0.5", mrr)
	}

	mrr2 := MRR([]string{"doc1", "doc2"}, []string{"doc1"})
	if !approx(mrr2, 1.0, 0.001) {
		t.Errorf("MRR = %f, want 1.0", mrr2)
	}

	mrr0 := MRR([]string{"doc2"}, []string{"doc1"})
	if mrr0 != 0 {
		t.Errorf("MRR = %f, want 0", mrr0)
	}
}

func TestNDCGAtK(t *testing.T) {
	// Perfect ranking
	retrieved := []string{"doc1", "doc2", "doc3"}
	expected := []string{"doc1", "doc2", "doc3"}
	ndcg := NDCGAtK(retrieved, expected, 3)
	if !approx(ndcg, 1.0, 0.001) {
		t.Errorf("NDCG@3 perfect = %f, want 1.0", ndcg)
	}

	// No relevant docs
	ndcg0 := NDCGAtK([]string{"x", "y"}, []string{"a", "b"}, 3)
	if ndcg0 != 0 {
		t.Errorf("NDCG@3 zero = %f, want 0", ndcg0)
	}
}

func TestHitRate(t *testing.T) {
	if HitRate([]string{"a", "b"}, []string{"b"}) != 1.0 {
		t.Error("HitRate should be 1.0")
	}
	if HitRate([]string{"a", "b"}, []string{"c"}) != 0.0 {
		t.Error("HitRate should be 0.0")
	}
}

func TestFaithfulness(t *testing.T) {
	f := Faithfulness("The cat sat on the mat", []string{"A cat sat on a mat in the room"})
	if f <= 0 {
		t.Errorf("Faithfulness should be > 0, got %f", f)
	}

	f0 := Faithfulness("quantum physics", []string{"cooking recipes for pasta"})
	if f0 >= 1.0 {
		t.Errorf("Faithfulness should be < 1.0 for unrelated, got %f", f0)
	}
}

func TestAnswerCorrectness(t *testing.T) {
	c := AnswerCorrectness("The capital of France is Paris", "Paris is the capital of France")
	if c < 0.5 {
		t.Errorf("AnswerCorrectness should be high for paraphrase, got %f", c)
	}
}

func TestEvaluateRetrieval(t *testing.T) {
	ds := &Dataset{
		Name: "test",
		Samples: []Sample{
			{
				ID:            "s1",
				Query:         "what is Go?",
				ExpectedDocs:  []string{"doc1", "doc2"},
				RetrievedDocs: []string{"doc1", "doc3", "doc2"},
			},
			{
				ID:            "s2",
				Query:         "what is Rust?",
				ExpectedDocs:  []string{"doc4"},
				RetrievedDocs: []string{"doc5", "doc4"},
			},
		},
	}

	ev := NewEvaluator()
	ev.K = 3
	report := ev.EvaluateRetrieval(ds)

	if report.TotalSamples != 2 {
		t.Errorf("TotalSamples = %d, want 2", report.TotalSamples)
	}
	if len(report.AggregateMetrics) == 0 {
		t.Error("Expected aggregate metrics")
	}
	if len(report.SampleResults) != 2 {
		t.Errorf("Expected 2 sample results, got %d", len(report.SampleResults))
	}
}

func TestEvaluateRAG(t *testing.T) {
	ds := &Dataset{
		Name: "rag-test",
		Samples: []Sample{
			{
				ID:              "s1",
				Query:           "What is the capital of France?",
				ExpectedDocs:    []string{"france_doc"},
				RetrievedDocs:   []string{"france_doc", "europe_doc"},
				GroundTruth:     "The capital of France is Paris.",
				GeneratedAnswer: "Paris is the capital city of France.",
			},
		},
	}

	ev := NewEvaluator()
	report := ev.EvaluateRAG(ds)

	hasRAGMetric := false
	for _, m := range report.AggregateMetrics {
		if m.Name == "avg_faithfulness" {
			hasRAGMetric = true
			break
		}
	}
	if !hasRAGMetric {
		t.Error("Expected RAG aggregate metrics")
	}
}

func TestLoadDatasetFromJSON(t *testing.T) {
	jsonData := []byte(`{
		"name": "demo",
		"samples": [
			{"id": "1", "query": "hello", "expected_docs": ["d1"]}
		]
	}`)

	ds, err := LoadDatasetFromJSON(jsonData)
	if err != nil {
		t.Fatalf("LoadDatasetFromJSON: %v", err)
	}
	if ds.Name != "demo" {
		t.Errorf("Name = %s, want demo", ds.Name)
	}
	if len(ds.Samples) != 1 {
		t.Errorf("Samples = %d, want 1", len(ds.Samples))
	}
}

func TestFormatReport(t *testing.T) {
	r := &Report{
		DatasetName:  "test",
		Timestamp:    "2026-04-02T00:00:00Z",
		TotalSamples: 1,
		AggregateMetrics: []MetricResult{
			{Name: "avg_precision@5", Value: 0.8},
		},
	}
	out := FormatReport(r)
	if out == "" {
		t.Error("FormatReport returned empty")
	}
}
