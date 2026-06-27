package main

import "testing"

func TestNavigationHistory(t *testing.T) {
	var history NavigationHistory
	history.Reset()
	history.Visit("main.A")
	history.Visit("main.B")
	history.Visit("main.C")

	if got, ok := history.Back(); !ok || got != "main.B" {
		t.Fatalf("Back() = %q, %v", got, ok)
	}
	if got, ok := history.Back(); !ok || got != "main.A" {
		t.Fatalf("Back() = %q, %v", got, ok)
	}
	if _, ok := history.Back(); ok {
		t.Fatal("Back succeeded at beginning")
	}
	if got, ok := history.Forward(); !ok || got != "main.B" {
		t.Fatalf("Forward() = %q, %v", got, ok)
	}
}

func TestNavigationHistoryVisitTruncatesForward(t *testing.T) {
	var history NavigationHistory
	history.Reset()
	history.Visit("A")
	history.Visit("B")
	history.Visit("C")
	_, _ = history.Back()
	history.Visit("D")

	if history.CanForward() {
		t.Fatal("forward history was not truncated")
	}
	if got, _ := history.Back(); got != "B" {
		t.Fatalf("Back() = %q, want B", got)
	}
}

func TestNavigationHistorySkipsDuplicateVisit(t *testing.T) {
	var history NavigationHistory
	history.Reset()
	history.Visit("A")
	history.Visit("A")
	if history.CanBack() {
		t.Fatal("duplicate visit created a history entry")
	}
}
