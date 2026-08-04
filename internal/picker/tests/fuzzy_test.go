package tests

import (
	"testing"

	"mpdtui/internal/picker"
)

func TestFuzzyScoreMatches(t *testing.T) {
	if _, ok := picker.FuzzyScore("", "anything"); !ok {
		t.Fatal("empty query should match everything")
	}
	if _, ok := picker.FuzzyScore("mkn", "Mark Knopfler"); !ok {
		t.Fatal("expected subsequence match")
	}
	if _, ok := picker.FuzzyScore("xyz", "Mark Knopfler"); ok {
		t.Fatal("expected no match")
	}
	if _, ok := picker.FuzzyScore("knm", "Mark Knopfler"); ok {
		t.Fatal("out-of-order subsequence should not match")
	}
}

func TestFuzzyScorePrefersEarlierCompactMatches(t *testing.T) {
	earlyScore, _ := picker.FuzzyScore("mark", "Mark Knopfler")
	lateScore, _ := picker.FuzzyScore("mark", "Album by Mark Knopfler")
	if earlyScore >= lateScore {
		t.Fatalf("expected earlier match to score better: early=%d late=%d", earlyScore, lateScore)
	}

	compactScore, _ := picker.FuzzyScore("mk", "Mark Knopfler")
	spreadScore, _ := picker.FuzzyScore("mk", "M x x x x x x x x x x K")
	if compactScore >= spreadScore {
		t.Fatalf("expected compact match to score better: compact=%d spread=%d", compactScore, spreadScore)
	}
}

func TestFilterSortIndex(t *testing.T) {
	labels := []string{"Hotel California", "Mark Knopfler - In the Sky", "Bang Bang", "Mark Knopfler - Secondary Waltz"}

	got := picker.FilterSortIndex("knopfler", labels)
	want := []int{1, 3}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("FilterSortIndex(%q) = %v, want %v", "knopfler", got, want)
	}

	if got := picker.FilterSortIndex("", labels); len(got) != len(labels) {
		t.Fatalf("empty query should return all %d labels, got %d", len(labels), len(got))
	}

	if got := picker.FilterSortIndex("zzz", labels); len(got) != 0 {
		t.Fatalf("expected no matches, got %v", got)
	}
}
