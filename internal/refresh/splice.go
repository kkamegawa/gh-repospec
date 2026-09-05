package refresh

import (
	"bytes"
	"fmt"
	"sort"
)

// replacement is one non-overlapping byte-range edit: the bytes in
// [Start, End) of the original source are replaced with Text.
type replacement struct {
	Start int
	End   int
	Text  []byte
}

// applyReplacements rewrites source by applying every replacement, which may
// be given in any order but must not overlap. Bytes outside every
// replacement's range are copied through unchanged.
func applyReplacements(source []byte, replacements []replacement) ([]byte, error) {
	sorted := append([]replacement(nil), replacements...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })

	var out bytes.Buffer
	out.Grow(len(source))
	cursor := 0
	for _, r := range sorted {
		if r.Start < cursor {
			return nil, fmt.Errorf("overlapping replacement at byte %d", r.Start)
		}
		if r.Start < 0 || r.End > len(source) || r.Start > r.End {
			return nil, fmt.Errorf("replacement range [%d,%d) is out of bounds for a %d-byte document", r.Start, r.End, len(source))
		}
		out.Write(source[cursor:r.Start])
		out.Write(r.Text)
		cursor = r.End
	}
	out.Write(source[cursor:])
	return out.Bytes(), nil
}

// lineEnding is a file's uniform line-ending convention, detected once so
// the YAML parser (which only ever sees "\n") can be handed normalized
// input and its output restored to the file's own convention afterward.
type lineEnding int

const (
	lineEndingLF lineEnding = iota
	lineEndingCRLF
)

// detectLineEnding reports CRLF only when every line break in source uses
// it; anything else (pure LF, or a mix) is treated as LF, matching the
// common case and never guessing on genuinely mixed input.
func detectLineEnding(source []byte) lineEnding {
	if len(source) == 0 || !bytes.Contains(source, []byte("\r\n")) {
		return lineEndingLF
	}
	if bytes.ContainsRune(bytes.ReplaceAll(source, []byte("\r\n"), nil), '\n') {
		return lineEndingLF
	}
	return lineEndingCRLF
}

func normalizeToLF(source []byte) []byte {
	return bytes.ReplaceAll(source, []byte("\r\n"), []byte("\n"))
}

func restoreLineEnding(source []byte, ending lineEnding) []byte {
	if ending == lineEndingLF {
		return source
	}
	return bytes.ReplaceAll(source, []byte("\n"), []byte("\r\n"))
}
