// Package eval provides a RAGAS-inspired evaluation framework for measuring
// retrieval and RAG quality. It supports dataset loading, retrieval-only metrics
// (precision, recall, MRR, NDCG), end-to-end RAG metrics (faithfulness,
// answer relevancy), and structured reporting.
package eval

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// Sample represents a single evaluation sample with a query, expected context,
// ground truth answer, and the actual retrieval/generation results to evaluate.
type Sample struct {
	ID              string   `json:"id"`
	Query           string   `json:"query"`
	ExpectedDocs    []string `json:"expected_docs,omitempty"`
	GroundTruth     string   `json:"ground_truth,omitempty"`
	RetrievedDocs   []string `json:"retrieved_docs,omitempty"`
	RetrievedScores []float64 `json:"retrieved_scores,omitempty"`
	GeneratedAnswer string   `json:"generated_answer,omitempty"`
}

// Dataset is a collection of evaluation samples.
type Dataset struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Samples     []Sample `json:"samples"`
}

// LoadDataset reads a JSON dataset file.
func LoadDataset(path string) (*Dataset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read dataset: %w", err)
	}
	var ds Dataset
	if err := json.Unmarshal(data, &ds); err != nil {
		return nil, fmt.Errorf("parse dataset: %w", err)
	}
	return &ds, nil
}

// LoadDatasetFromJSON parses a dataset from raw JSON bytes.
func LoadDatasetFromJSON(data []byte) (*Dataset, error) {
	var ds Dataset
	if err := json.Unmarshal(data, &ds); err != nil {
		return nil, fmt.Errorf("parse dataset: %w", err)
	}
	return &ds, nil
}

// MetricResult holds one metric value.
type MetricResult struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
}

// SampleResult holds per-sample evaluation results.
type SampleResult struct {
	SampleID string         `json:"sample_id"`
	Query    string         `json:"query"`
	Metrics  []MetricResult `json:"metrics"`
}

// Report is the final evaluation report.
type Report struct {
	DatasetName    string         `json:"dataset_name"`
	Timestamp      string         `json:"timestamp"`
	TotalSamples   int            `json:"total_samples"`
	AggregateMetrics []MetricResult `json:"aggregate_metrics"`
	SampleResults  []SampleResult `json:"sample_results"`
}

// SaveReport writes the report as JSON.
func (r *Report) SaveReport(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// --- Retrieval Metrics ---

// Precision@K measures the fraction of retrieved docs (top K) that are relevant.
func PrecisionAtK(retrieved, expected []string, k int) float64 {
	if k <= 0 || len(retrieved) == 0 {
		return 0
	}
	topK := retrieved
	if len(topK) > k {
		topK = topK[:k]
	}

	relevant := toSet(expected)
	hits := 0
	for _, doc := range topK {
		if relevant[doc] {
			hits++
		}
	}
	return float64(hits) / float64(len(topK))
}

// RecallAtK measures the fraction of expected docs found in the top K retrieved.
func RecallAtK(retrieved, expected []string, k int) float64 {
	if k <= 0 || len(expected) == 0 {
		return 0
	}
	topK := retrieved
	if len(topK) > k {
		topK = topK[:k]
	}

	retrievedSet := toSet(topK)
	hits := 0
	for _, doc := range expected {
		if retrievedSet[doc] {
			hits++
		}
	}
	return float64(hits) / float64(len(expected))
}

// MRR (Mean Reciprocal Rank) returns 1/rank of the first relevant document.
func MRR(retrieved, expected []string) float64 {
	relevant := toSet(expected)
	for i, doc := range retrieved {
		if relevant[doc] {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

// NDCG@K (Normalized Discounted Cumulative Gain) measures ranking quality.
func NDCGAtK(retrieved, expected []string, k int) float64 {
	if k <= 0 || len(expected) == 0 {
		return 0
	}
	topK := retrieved
	if len(topK) > k {
		topK = topK[:k]
	}

	relevant := toSet(expected)

	dcg := 0.0
	for i, doc := range topK {
		if relevant[doc] {
			dcg += 1.0 / math.Log2(float64(i+2))
		}
	}

	idealK := k
	if len(expected) < idealK {
		idealK = len(expected)
	}
	idcg := 0.0
	for i := 0; i < idealK; i++ {
		idcg += 1.0 / math.Log2(float64(i+2))
	}

	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

// HitRate returns 1.0 if any expected document is in retrieved, else 0.0.
func HitRate(retrieved, expected []string) float64 {
	relevant := toSet(expected)
	for _, doc := range retrieved {
		if relevant[doc] {
			return 1.0
		}
	}
	return 0
}

// --- RAG Metrics (LLM-free heuristic versions) ---

// Faithfulness estimates how grounded the answer is in retrieved context,
// using simple token overlap. For LLM-based evaluation, use FaithfulnessLLM.
func Faithfulness(answer string, retrievedDocs []string) float64 {
	if answer == "" || len(retrievedDocs) == 0 {
		return 0
	}
	answerTokens := tokenize(answer)
	if len(answerTokens) == 0 {
		return 0
	}

	contextTokens := make(map[string]bool)
	for _, doc := range retrievedDocs {
		for _, t := range tokenize(doc) {
			contextTokens[t] = true
		}
	}

	grounded := 0
	for _, t := range answerTokens {
		if contextTokens[t] {
			grounded++
		}
	}
	return float64(grounded) / float64(len(answerTokens))
}

// AnswerRelevancy estimates answer relevancy by token overlap with the query.
func AnswerRelevancy(answer, query string) float64 {
	if answer == "" || query == "" {
		return 0
	}
	queryTokens := toSet(tokenize(query))
	answerTokens := tokenize(answer)
	if len(answerTokens) == 0 {
		return 0
	}

	overlap := 0
	for _, t := range answerTokens {
		if queryTokens[t] {
			overlap++
		}
	}
	return float64(overlap) / float64(len(answerTokens))
}

// AnswerCorrectness measures token-level F1 between answer and ground truth.
func AnswerCorrectness(answer, groundTruth string) float64 {
	if answer == "" || groundTruth == "" {
		return 0
	}
	ansSet := toSet(tokenize(answer))
	gtTokens := tokenize(groundTruth)
	gtSet := toSet(gtTokens)

	tp := 0
	for t := range ansSet {
		if gtSet[t] {
			tp++
		}
	}
	if tp == 0 {
		return 0
	}

	precision := float64(tp) / float64(len(ansSet))
	recall := float64(tp) / float64(len(gtSet))
	return 2 * precision * recall / (precision + recall)
}

// --- Evaluator ---

// Evaluator runs evaluation on a dataset.
type Evaluator struct {
	K int // top-K for retrieval metrics (default 5)
}

// NewEvaluator creates an evaluator with default K=5.
func NewEvaluator() *Evaluator {
	return &Evaluator{K: 5}
}

// EvaluateRetrieval runs retrieval-only metrics on the dataset.
func (ev *Evaluator) EvaluateRetrieval(ds *Dataset) *Report {
	report := &Report{
		DatasetName:  ds.Name,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		TotalSamples: len(ds.Samples),
	}

	var (
		sumP, sumR, sumMRR, sumNDCG, sumHit float64
	)

	for _, s := range ds.Samples {
		p := PrecisionAtK(s.RetrievedDocs, s.ExpectedDocs, ev.K)
		r := RecallAtK(s.RetrievedDocs, s.ExpectedDocs, ev.K)
		mrr := MRR(s.RetrievedDocs, s.ExpectedDocs)
		ndcg := NDCGAtK(s.RetrievedDocs, s.ExpectedDocs, ev.K)
		hit := HitRate(s.RetrievedDocs, s.ExpectedDocs)

		sumP += p
		sumR += r
		sumMRR += mrr
		sumNDCG += ndcg
		sumHit += hit

		report.SampleResults = append(report.SampleResults, SampleResult{
			SampleID: s.ID,
			Query:    s.Query,
			Metrics: []MetricResult{
				{Name: fmt.Sprintf("precision@%d", ev.K), Value: p},
				{Name: fmt.Sprintf("recall@%d", ev.K), Value: r},
				{Name: "mrr", Value: mrr},
				{Name: fmt.Sprintf("ndcg@%d", ev.K), Value: ndcg},
				{Name: "hit_rate", Value: hit},
			},
		})
	}

	n := float64(len(ds.Samples))
	if n > 0 {
		report.AggregateMetrics = []MetricResult{
			{Name: fmt.Sprintf("avg_precision@%d", ev.K), Value: sumP / n},
			{Name: fmt.Sprintf("avg_recall@%d", ev.K), Value: sumR / n},
			{Name: "avg_mrr", Value: sumMRR / n},
			{Name: fmt.Sprintf("avg_ndcg@%d", ev.K), Value: sumNDCG / n},
			{Name: "avg_hit_rate", Value: sumHit / n},
		}
	}

	return report
}

// EvaluateRAG runs both retrieval and RAG metrics.
func (ev *Evaluator) EvaluateRAG(ds *Dataset) *Report {
	report := ev.EvaluateRetrieval(ds)

	var sumFaith, sumRelevancy, sumCorrectness float64
	ragCount := 0

	for i, s := range ds.Samples {
		if s.GeneratedAnswer == "" {
			continue
		}

		faith := Faithfulness(s.GeneratedAnswer, s.RetrievedDocs)
		relevancy := AnswerRelevancy(s.GeneratedAnswer, s.Query)
		correctness := AnswerCorrectness(s.GeneratedAnswer, s.GroundTruth)

		sumFaith += faith
		sumRelevancy += relevancy
		sumCorrectness += correctness
		ragCount++

		if i < len(report.SampleResults) {
			report.SampleResults[i].Metrics = append(report.SampleResults[i].Metrics,
				MetricResult{Name: "faithfulness", Value: faith},
				MetricResult{Name: "answer_relevancy", Value: relevancy},
				MetricResult{Name: "answer_correctness", Value: correctness},
			)
		}
	}

	if ragCount > 0 {
		n := float64(ragCount)
		report.AggregateMetrics = append(report.AggregateMetrics,
			MetricResult{Name: "avg_faithfulness", Value: sumFaith / n},
			MetricResult{Name: "avg_answer_relevancy", Value: sumRelevancy / n},
			MetricResult{Name: "avg_answer_correctness", Value: sumCorrectness / n},
		)
	}

	return report
}

// --- helpers ---

func toSet[T comparable](items []T) map[T]bool {
	m := make(map[T]bool, len(items))
	for _, item := range items {
		m[item] = true
	}
	return m
}

func tokenize(text string) []string {
	text = strings.ToLower(text)
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127)
	})
	// Remove common stop words for better metric quality
	filtered := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) > 1 && !stopWords[w] {
			filtered = append(filtered, w)
		}
	}
	return filtered
}

var stopWords = map[string]bool{
	"the": true, "is": true, "at": true, "which": true, "on": true,
	"a": true, "an": true, "and": true, "or": true, "but": true,
	"in": true, "of": true, "to": true, "for": true, "with": true,
	"it": true, "this": true, "that": true, "are": true, "was": true,
	"be": true, "has": true, "have": true, "had": true, "not": true,
	"by": true, "from": true, "as": true, "do": true, "does": true,
}

// FormatReport returns a human-readable summary.
func FormatReport(r *Report) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== Evaluation Report: %s ===\n", r.DatasetName))
	sb.WriteString(fmt.Sprintf("Timestamp: %s\n", r.Timestamp))
	sb.WriteString(fmt.Sprintf("Total Samples: %d\n\n", r.TotalSamples))

	sb.WriteString("Aggregate Metrics:\n")
	sort.Slice(r.AggregateMetrics, func(i, j int) bool {
		return r.AggregateMetrics[i].Name < r.AggregateMetrics[j].Name
	})
	for _, m := range r.AggregateMetrics {
		sb.WriteString(fmt.Sprintf("  %-30s %.4f\n", m.Name, m.Value))
	}

	return sb.String()
}
