package refresh

import "testing"

func TestApplyReplacementsPreservesUntouchedBytes(t *testing.T) {
	t.Parallel()
	src := []byte("a: 1\nb: 2\nc: 3\n")
	out, err := applyReplacements(src, []replacement{{Start: 3, End: 4, Text: []byte("99")}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), "a: 99\nb: 2\nc: 3\n"; got != want {
		t.Fatalf("out = %q, want %q", got, want)
	}
}

func TestApplyReplacementsHandlesMultipleOutOfOrderRanges(t *testing.T) {
	t.Parallel()
	src := []byte("a: 1\nb: 2\nc: 3\n")
	out, err := applyReplacements(src, []replacement{
		{Start: 8, End: 9, Text: []byte("22")},
		{Start: 3, End: 4, Text: []byte("11")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), "a: 11\nb: 22\nc: 3\n"; got != want {
		t.Fatalf("out = %q, want %q", got, want)
	}
}

func TestApplyReplacementsRejectsOverlap(t *testing.T) {
	t.Parallel()
	src := []byte("a: 1\n")
	_, err := applyReplacements(src, []replacement{
		{Start: 0, End: 4, Text: []byte("x")},
		{Start: 3, End: 5, Text: []byte("y")},
	})
	if err == nil {
		t.Fatal("applyReplacements() unexpectedly accepted overlapping ranges")
	}
}

func TestDetectLineEndingRequiresUniformCRLF(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
		want   lineEnding
	}{
		{"lf only", "a: 1\nb: 2\n", lineEndingLF},
		{"crlf only", "a: 1\r\nb: 2\r\n", lineEndingCRLF},
		{"mixed treated as lf", "a: 1\r\nb: 2\n", lineEndingLF},
		{"empty", "", lineEndingLF},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := detectLineEnding([]byte(tc.source)); got != tc.want {
				t.Fatalf("detectLineEnding(%q) = %v, want %v", tc.source, got, tc.want)
			}
		})
	}
}

func TestNormalizeAndRestoreLineEndingRoundTrip(t *testing.T) {
	t.Parallel()
	src := []byte("a: 1\r\nb: 2\r\n")
	ending := detectLineEnding(src)
	normalized := normalizeToLF(src)
	if string(normalized) != "a: 1\nb: 2\n" {
		t.Fatalf("normalizeToLF() = %q", normalized)
	}
	restored := restoreLineEnding(normalized, ending)
	if string(restored) != string(src) {
		t.Fatalf("restoreLineEnding() = %q, want %q", restored, src)
	}
}
