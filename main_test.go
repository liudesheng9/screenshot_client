package main

import (
	"regexp"
	"testing"
)

func TestFormatProgressDisplayLine(t *testing.T) {
	line := "PROGRESS W0:50 W1:45 W2:60 T:155"

	got, ok := formatProgressDisplayLine(line)
	if !ok {
		t.Fatalf("expected progress line to parse")
	}

	want := "W0:50 W1:45 W2:60 Total:155"
	if got != want {
		t.Fatalf("unexpected progress display: want %q, got %q", want, got)
	}

	allowed := regexp.MustCompile(`^[W0-9: ]+Total:[0-9]+$`)
	if !allowed.MatchString(got) {
		t.Fatalf("progress display should remain numeric-focused, got %q", got)
	}
}

func TestFormatDoneDisplayLine(t *testing.T) {
	line := "DONE copied=8 exist=10 failed=1 skipped=1 dest=./img_dump"

	got, ok := formatDoneDisplayLine(line)
	if !ok {
		t.Fatalf("expected DONE line to parse")
	}

	want := "DONE copied=8 exist=10 failed=1 skipped=1 dest=./img_dump"
	if got != want {
		t.Fatalf("unexpected DONE display: want %q, got %q", want, got)
	}
}
