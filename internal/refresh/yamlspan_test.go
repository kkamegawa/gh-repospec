package refresh

import (
	"testing"
)

func TestScalarValueSpanPlainScalar(t *testing.T) {
	t.Parallel()
	src := []byte("repository:\n  name: demo\n  visibility: public\n")
	file, err := parseYAML(src)
	if err != nil {
		t.Fatal(err)
	}
	top, err := topLevelMapping(file)
	if err != nil {
		t.Fatal(err)
	}
	repo, ok := findField(mappingFieldSpans(src, top, len(src)), "repository")
	if !ok {
		t.Fatal("repository field not found")
	}
	nested, ok := asBlockMapping(repo.valueNode)
	if !ok {
		t.Fatal("repository value is not a block mapping")
	}
	name, ok := findField(mappingFieldSpans(src, nested, repo.end), "name")
	if !ok {
		t.Fatal("name field not found")
	}
	start, end, err := fieldValueSpan(src, name)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(src[start:end]), "demo"; got != want {
		t.Fatalf("span = %q, want %q", got, want)
	}
}

func TestScalarValueSpanQuotedScalarsPreserveStyle(t *testing.T) {
	t.Parallel()
	src := []byte("a: 'single ''quoted'' value'\nb: \"double \\\"quoted\\\" value\"\nc: plain\n")
	file, err := parseYAML(src)
	if err != nil {
		t.Fatal(err)
	}
	top, err := topLevelMapping(file)
	if err != nil {
		t.Fatal(err)
	}
	spans := mappingFieldSpans(src, top, len(src))

	a, _ := findField(spans, "a")
	start, end, err := fieldValueSpan(src, a)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(src[start:end]), "'single ''quoted'' value'"; got != want {
		t.Fatalf("single-quoted span = %q, want %q", got, want)
	}

	b, _ := findField(spans, "b")
	start, end, err = fieldValueSpan(src, b)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(src[start:end]), `"double \"quoted\" value"`; got != want {
		t.Fatalf("double-quoted span = %q, want %q", got, want)
	}
}

func TestScalarValueSpanStopsBeforeTrailingComment(t *testing.T) {
	t.Parallel()
	src := []byte("owner: octo # trailing comment\nname: demo\n")
	file, err := parseYAML(src)
	if err != nil {
		t.Fatal(err)
	}
	top, err := topLevelMapping(file)
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := findField(mappingFieldSpans(src, top, len(src)), "owner")
	start, end, err := fieldValueSpan(src, owner)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(src[start:end]), "octo"; got != want {
		t.Fatalf("span = %q, want %q (trailing comment must not be included)", got, want)
	}
}

func TestScalarValueSpanRejectsBlockScalar(t *testing.T) {
	t.Parallel()
	src := []byte("description: |\n  multi\n  line\n")
	file, err := parseYAML(src)
	if err != nil {
		t.Fatal(err)
	}
	top, err := topLevelMapping(file)
	if err != nil {
		t.Fatal(err)
	}
	field, _ := findField(mappingFieldSpans(src, top, len(src)), "description")
	if _, _, err := fieldValueSpan(src, field); err == nil {
		t.Fatal("fieldValueSpan() unexpectedly accepted a block scalar")
	}
}

func TestMappingFieldSpansPreservesNonAlphabeticalOrderAndComputesEndsFromSiblings(t *testing.T) {
	t.Parallel()
	src := []byte("zebra: 1\napple: 2\nmango: 3\n")
	file, err := parseYAML(src)
	if err != nil {
		t.Fatal(err)
	}
	top, err := topLevelMapping(file)
	if err != nil {
		t.Fatal(err)
	}
	spans := mappingFieldSpans(src, top, len(src))
	if len(spans) != 3 || spans[0].key != "zebra" || spans[1].key != "apple" || spans[2].key != "mango" {
		t.Fatalf("spans = %#v, want file order preserved regardless of alphabetical order", spans)
	}
	if got, want := string(src[spans[0].keyStart:spans[1].keyStart]), "zebra: 1\n"; got != want {
		t.Fatalf("zebra field span (via next sibling) = %q, want %q", got, want)
	}
	if spans[2].end != len(src) {
		t.Fatalf("last field end = %d, want len(src) = %d", spans[2].end, len(src))
	}
}

func TestSequenceEntrySpansComputeCorrectBoundariesAroundBlockScalar(t *testing.T) {
	t.Parallel()
	// The block scalar in entry 0 is exactly the case that made a
	// last-token-offset approach unreliable during development: its
	// trailing bleed must never leak into entry 1's span.
	src := []byte("labels:\n" +
		"  - name: bug\n" +
		"    color: d73a4a\n" +
		"  - name: feature # keep this\n" +
		"    color: \"00ff00\"\n" +
		"    description: |\n" +
		"      multi\n" +
		"      line\n" +
		"milestones:\n" +
		"  - title: v1\n")
	file, err := parseYAML(src)
	if err != nil {
		t.Fatal(err)
	}
	top, err := topLevelMapping(file)
	if err != nil {
		t.Fatal(err)
	}
	spans := mappingFieldSpans(src, top, len(src))
	labels, ok := findField(spans, "labels")
	if !ok {
		t.Fatal("labels field not found")
	}
	seqNode, ok := asSequence(labels.valueNode)
	if !ok {
		t.Fatalf("labels value is not a sequence: %T", labels.valueNode)
	}
	entries := sequenceEntrySpans(src, seqNode, labels.end)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	// entry 0's span starts at its own "-" (excluding the line's leading
	// indent, which belongs to no entry) and, by the next-sibling-start
	// convention, ends at entry 1's "-" — so it trails with entry 1's own
	// leading indent. That trailing whitespace is never spliced on its own:
	// updating an existing entry rewrites its fields in place, and a new
	// entry is only ever appended after the last one, so this span is used
	// solely to locate and iterate entries, never to blindly replace one.
	if got, want := string(src[entries[0].start:entries[0].end]), "- name: bug\n    color: d73a4a\n  "; got != want {
		t.Fatalf("entry 0 span = %q, want %q", got, want)
	}
	wantEntry1 := "- name: feature # keep this\n    color: \"00ff00\"\n    description: |\n      multi\n      line\n"
	if got := string(src[entries[1].start:entries[1].end]); got != wantEntry1 {
		t.Fatalf("entry 1 span = %q, want %q", got, wantEntry1)
	}
}
