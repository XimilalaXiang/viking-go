package memory

import (
	"math"
	"testing"
	"time"
)

func TestHotnessScore(t *testing.T) {
	now := time.Now().UTC()

	// Fresh memory with high access count should have high score
	score := HotnessScore(100, now, now, 7.0)
	if score < 0.7 {
		t.Errorf("fresh high-access score = %f, want > 0.7", score)
	}

	// Old memory with low access should have low score
	old := now.Add(-60 * 24 * time.Hour) // 60 days ago
	score = HotnessScore(1, old, now, 7.0)
	if score > 0.1 {
		t.Errorf("old low-access score = %f, want < 0.1", score)
	}

	// Zero time should return 0
	score = HotnessScore(5, time.Time{}, now, 7.0)
	if score != 0.0 {
		t.Errorf("zero time score = %f, want 0", score)
	}

	// Score should be between 0 and 1
	for _, ac := range []int{0, 1, 10, 100, 1000} {
		for _, days := range []float64{0, 1, 7, 30, 90, 365} {
			updated := now.Add(-time.Duration(days*24) * time.Hour)
			s := HotnessScore(ac, updated, now, 7.0)
			if s < 0 || s > 1 {
				t.Errorf("score(%d, %f days) = %f, out of range", ac, days, s)
			}
		}
	}

	// Half-life test: score at exactly half_life days should be ~half of score at 0 days
	scoreNow := HotnessScore(10, now, now, 7.0)
	score7d := HotnessScore(10, now.Add(-7*24*time.Hour), now, 7.0)
	ratio := score7d / scoreNow
	if math.Abs(ratio-0.5) > 0.1 {
		t.Errorf("half-life ratio = %f, want ~0.5 (now=%f, 7d=%f)", ratio, scoreNow, score7d)
	}
}

func TestArchiveURI(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			"viking://user/default/memories/entities/mem_123.md",
			"viking://user/default/memories/_archive/entities/mem_123.md",
		},
		{
			"viking://agent/default/memories/tools/tool1.md",
			"viking://agent/default/memories/_archive/tools/tool1.md",
		},
	}

	for _, tt := range tests {
		got := toArchiveURI(tt.input)
		if got != tt.want {
			t.Errorf("toArchiveURI(%q) = %q, want %q", tt.input, got, tt.want)
		}

		restored := fromArchiveURI(got)
		if restored != tt.input {
			t.Errorf("fromArchiveURI(%q) = %q, want %q", got, restored, tt.input)
		}
	}
}

func TestMergeToolStats(t *testing.T) {
	existing := &ToolStats{
		CallCount: 10, SuccessCount: 8, ErrorCount: 2,
		TotalLatency: 5000, LastUsed: "2026-01-01",
	}
	new := &ToolStats{
		CallCount: 5, SuccessCount: 4, ErrorCount: 1,
		TotalLatency: 2500, LastUsed: "2026-04-03",
	}

	merged := MergeToolStats(existing, new)
	if merged.CallCount != 15 {
		t.Errorf("CallCount = %d, want 15", merged.CallCount)
	}
	if merged.SuccessCount != 12 {
		t.Errorf("SuccessCount = %d, want 12", merged.SuccessCount)
	}
	if merged.ErrorCount != 3 {
		t.Errorf("ErrorCount = %d, want 3", merged.ErrorCount)
	}
	if merged.TotalLatency != 7500 {
		t.Errorf("TotalLatency = %f, want 7500", merged.TotalLatency)
	}
	if merged.LastUsed != "2026-04-03" {
		t.Errorf("LastUsed = %q", merged.LastUsed)
	}
}

func TestMergeGuidelines(t *testing.T) {
	existing := []SkillGuideline{
		{Guideline: "Always use JSON format", Source: "session1"},
	}
	new := []SkillGuideline{
		{Guideline: "Always use JSON format", Source: "session2"}, // duplicate
		{Guideline: "Prefer batch operations", Source: "session2"},
	}

	merged := MergeGuidelines(existing, new)
	if len(merged) != 2 {
		t.Errorf("len(merged) = %d, want 2", len(merged))
	}
}
