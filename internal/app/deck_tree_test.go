package app

import (
	"reflect"
	"testing"
)

func TestDeckAncestorPaths(t *testing.T) {
	if got := DeckAncestorPaths(""); len(got) != 0 {
		t.Fatalf("expected nil/empty for empty deck, got %v", got)
	}
	if got := DeckAncestorPaths("Japanese"); len(got) != 0 {
		t.Fatalf("expected nil/empty for top-level deck, got %v", got)
	}
	got := DeckAncestorPaths("Japanese::N2::Vocab")
	want := []string{"Japanese", "Japanese::N2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DeckAncestorPaths mismatch: got %v, want %v", got, want)
	}
}

func TestVisibleDeckOptionsCollapseExpandAndSearch(t *testing.T) {
	decks := []string{
		"English::IELTS",
		"Japanese::N2::Grammar",
		"Japanese::N2::Vocab",
		"Japanese::N3",
		"Math",
	}

	// 1. Collapsed by default: only top-level decks shown.
	collapsed := VisibleDeckOptions(decks, nil, "")
	wantCollapsed := []DeckOption{
		{FullName: "English", Label: "English", Depth: 0, HasSubdeck: true},
		{FullName: "Japanese", Label: "Japanese", Depth: 0, HasSubdeck: true},
		{FullName: "Math", Label: "Math", Depth: 0, HasSubdeck: false},
	}
	if !reflect.DeepEqual(collapsed, wantCollapsed) {
		t.Fatalf("collapsed mismatch:\ngot:  %+v\nwant: %+v", collapsed, wantCollapsed)
	}

	// 2. Expanding Japanese reveals N2 (collapsed) and N3, hiding Grammar/Vocab.
	exp1 := map[string]bool{"Japanese": true}
	lvl1 := VisibleDeckOptions(decks, exp1, "")
	wantLvl1 := []DeckOption{
		{FullName: "English", Label: "English", Depth: 0, HasSubdeck: true},
		{FullName: "Japanese", Label: "Japanese", Depth: 0, HasSubdeck: true},
		{FullName: "Japanese::N2", Label: "N2", Depth: 1, HasSubdeck: true},
		{FullName: "Japanese::N3", Label: "N3", Depth: 1, HasSubdeck: false},
		{FullName: "Math", Label: "Math", Depth: 0, HasSubdeck: false},
	}
	if !reflect.DeepEqual(lvl1, wantLvl1) {
		t.Fatalf("level 1 expand mismatch:\ngot:  %+v\nwant: %+v", lvl1, wantLvl1)
	}

	// 3. Expanding Japanese::N2 reveals Grammar and Vocab.
	exp2 := map[string]bool{"Japanese": true, "Japanese::N2": true}
	lvl2 := VisibleDeckOptions(decks, exp2, "")
	wantLvl2 := []DeckOption{
		{FullName: "English", Label: "English", Depth: 0, HasSubdeck: true},
		{FullName: "Japanese", Label: "Japanese", Depth: 0, HasSubdeck: true},
		{FullName: "Japanese::N2", Label: "N2", Depth: 1, HasSubdeck: true},
		{FullName: "Japanese::N2::Grammar", Label: "Grammar", Depth: 2, HasSubdeck: false},
		{FullName: "Japanese::N2::Vocab", Label: "Vocab", Depth: 2, HasSubdeck: false},
		{FullName: "Japanese::N3", Label: "N3", Depth: 1, HasSubdeck: false},
		{FullName: "Math", Label: "Math", Depth: 0, HasSubdeck: false},
	}
	if !reflect.DeepEqual(lvl2, wantLvl2) {
		t.Fatalf("level 2 expand mismatch:\ngot:  %+v\nwant: %+v", lvl2, wantLvl2)
	}

	// 4. Searching a deep leaf auto-expands ancestor chain even when expanded is nil.
	searched := VisibleDeckOptions(decks, nil, "vocab")
	wantSearched := []DeckOption{
		{FullName: "Japanese", Label: "Japanese", Depth: 0, HasSubdeck: true},
		{FullName: "Japanese::N2", Label: "N2", Depth: 1, HasSubdeck: true},
		{FullName: "Japanese::N2::Vocab", Label: "Vocab", Depth: 2, HasSubdeck: false},
	}
	if !reflect.DeepEqual(searched, wantSearched) {
		t.Fatalf("search mismatch:\ngot:  %+v\nwant: %+v", searched, wantSearched)
	}
}

func TestBuildDailyProgressTracksDueAndNewSeparately(t *testing.T) {
	prog := buildDailyProgress("2026-11-27", 380, 350, 20, 15)
	if prog.InitialTotal != 400 || prog.Remaining != 365 || prog.Completed != 35 {
		t.Fatalf("unexpected total progress: %+v", prog)
	}
	if prog.InitialDue != 380 || prog.RemainingDue != 350 || prog.CompletedDue != 30 {
		t.Fatalf("unexpected due progress: %+v", prog)
	}
	if prog.InitialNew != 20 || prog.RemainingNew != 15 || prog.CompletedNew != 5 {
		t.Fatalf("unexpected new progress: %+v", prog)
	}
}
