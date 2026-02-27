package main

import (
	"regexp"
	"strings"
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

func TestParseProgressV2(t *testing.T) {
	line := "PROGRESS_V2 T:3/10 IO-W0:2:img_00.png:reading:1.2 PROC-W1:1:-:idle:-"

	progress, ok := parseProgressV2(line)
	if !ok {
		t.Fatalf("expected PROGRESS_V2 line to parse")
	}
	if progress.Total != 3 || progress.Target != 10 {
		t.Fatalf("unexpected totals: %#v", progress)
	}
	if len(progress.WorkerStatuses) != 2 {
		t.Fatalf("expected 2 worker statuses, got %d", len(progress.WorkerStatuses))
	}
	if progress.WorkerStatuses[0].WorkerID != 0 || progress.WorkerStatuses[0].WorkerLabel != "IO-W0" || progress.WorkerStatuses[0].Filename != "img_00.png" {
		t.Fatalf("unexpected first worker status: %#v", progress.WorkerStatuses[0])
	}
}

func TestFormatWorkerTable(t *testing.T) {
	progress := ProgressV2{
		Total:  4,
		Target: 20,
		WorkerStatuses: []WorkerStatus{
			{WorkerID: 0, WorkerLabel: "IO-W0", Filename: "very_long_filename_that_should_be_cut_from_left.png", Stage: "reading", Elapsed: "1.3"},
			{WorkerID: 1, WorkerLabel: "PROC-W1", Filename: "-", Stage: "idle", Elapsed: "-"},
		},
	}

	table := formatWorkerTable(progress)
	for _, token := range []string{"Progress 4/20", "Worker", "IO-W0", "PROC-W1", "idle", "..."} {
		if !strings.Contains(table, token) {
			t.Fatalf("expected token %q in worker table: %s", token, table)
		}
	}
}

func TestParseProgressV2SupportsLegacyWorkerLabels(t *testing.T) {
	line := "PROGRESS_V2 T:3/10 W0:2:img_00.png:decode:1.2"

	progress, ok := parseProgressV2(line)
	if !ok {
		t.Fatalf("expected legacy PROGRESS_V2 line to parse")
	}
	if len(progress.WorkerStatuses) != 1 {
		t.Fatalf("expected 1 worker status, got %d", len(progress.WorkerStatuses))
	}
	if progress.WorkerStatuses[0].WorkerLabel != "W0" {
		t.Fatalf("unexpected legacy worker label: %#v", progress.WorkerStatuses[0])
	}
}
