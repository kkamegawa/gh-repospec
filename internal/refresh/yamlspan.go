// Package refresh implements the format-preserving --into refresh: updating
// only the paths and top-level sections an existing configuration file
// already declares, while leaving everything else in the file byte-for-byte
// unchanged.
//
// YAML editing locates a target scalar or sequence entry through
// github.com/goccy/go-yaml's AST, then replaces only that value's own byte
// span in the original source — never re-encoding the document from the
// AST. config.Load keeps its own strict gopkg.in/yaml.v3 decoding; this
// package's parser is confined to locating spans for the rewrite and must
// never accept a document config.Load would reject.
//
// This package supports block-style YAML only: a scalar or sequence entry
// that lives inside a flow mapping or flow sequence ({...}/[...]), carries
// an anchor or alias, or is written as a block scalar (| or >) is left
// untouched and reported as unsupported, rather than risking a wrong edit.
package refresh

import (
	"bytes"
	"fmt"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

// parseYAML parses source with comments attached, the form every span
// computation in this file requires.
func parseYAML(source []byte) (*ast.File, error) {
	file, err := parser.ParseBytes(source, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse YAML for refresh: %w", err)
	}
	if len(file.Docs) != 1 || file.Docs[0].Body == nil {
		return nil, fmt.Errorf("refresh requires exactly one YAML document")
	}
	return file, nil
}

// topLevelMapping returns the document's root mapping, the only shape this
// package edits.
func topLevelMapping(file *ast.File) (*ast.MappingNode, error) {
	mapping, ok := asBlockMapping(file.Docs[0].Body)
	if !ok {
		return nil, fmt.Errorf("refresh requires a top-level YAML mapping")
	}
	return mapping, nil
}

// asBlockMapping accepts only *ast.MappingNode, the shape a block mapping
// with any number of keys parses to.
func asBlockMapping(node ast.Node) (*ast.MappingNode, bool) {
	mapping, ok := node.(*ast.MappingNode)
	if !ok || mapping.IsFlowStyle {
		return nil, false
	}
	return mapping, true
}

// asSequence accepts a block-style *ast.SequenceNode, and also accepts an
// empty flow sequence ("[]") since that is exactly how an empty declared
// collection is written. A non-empty flow sequence ("[a, b]") is refused:
// this package only inserts and updates block-style entries.
func asSequence(node ast.Node) (*ast.SequenceNode, bool) {
	seq, ok := node.(*ast.SequenceNode)
	if !ok {
		return nil, false
	}
	if seq.IsFlowStyle && len(seq.Values) > 0 {
		return nil, false
	}
	return seq, true
}

// fieldSpan is one mapping entry's key name and the exact byte span of its
// value within the source, computed without ever trusting a token's
// trailing bleed into whatever follows it.
type fieldSpan struct {
	key       string
	keyStart  int
	valueNode ast.Node
	// end is the start of the next sibling field, or the enclosing
	// construct's own end when this is the last field. It bounds where a
	// new entry may be appended after this field but is not used to size a
	// value replacement, which is computed independently by scanning.
	end int
}

// bodyStart returns the first offset past this field's own key, where a
// nested mapping or sequence declared as its value begins to be searched
// for. Anything before it belongs to an earlier part of the document.
func (f fieldSpan) bodyStart() int {
	return f.keyStart + len(f.key)
}

// mappingFieldSpans returns every field of mapping in document order.
// [sectionStart, sectionEnd) is the already-known extent of the enclosing
// construct (the whole document at the top level, the parent field's
// bodyStart and end one level down, or a sequence entry's own span). It
// bounds the fallback key search so a key is never located outside the
// mapping it belongs to, and sectionEnd is also the last field's end.
func mappingFieldSpans(source []byte, mapping *ast.MappingNode, sectionStart, sectionEnd int) []fieldSpan {
	entries := mapping.Values
	spans := make([]fieldSpan, 0, len(entries))
	cursor := sectionStart
	for i, entry := range entries {
		name := mappingKeyName(entry.Key)
		start := verifiedKeyStart(source, startOfNode(source, entry.Key), name, cursor, sectionEnd)
		end := sectionEnd
		if i+1 < len(entries) {
			nextName := mappingKeyName(entries[i+1].Key)
			end = verifiedKeyStart(source, startOfNode(source, entries[i+1].Key), nextName, start, sectionEnd)
		}
		spans = append(spans, fieldSpan{
			key:       name,
			keyStart:  start,
			valueNode: entry.Value,
			end:       end,
		})
		cursor = start
	}
	return spans
}

// verifiedKeyStart returns computed unless the source bytes at that offset
// do not actually begin with name, in which case it falls back to a literal
// search for "name:" at the start of a line within [searchFrom, searchTo).
//
// go-yaml's scanner is observed to skew a token's Position.Offset by a small
// amount for the token immediately following a block scalar (a
// multi-line "|" or ">" value elsewhere in the document, never edited by
// this package but still present in the byte stream this package must
// navigate around correctly). This verification catches that skew, and any
// other cause of it, before it can turn into a wrong-location edit.
func verifiedKeyStart(source []byte, computed int, name string, searchFrom, searchTo int) int {
	if matchesKeyAt(source, computed, name) {
		return computed
	}
	if found, ok := findKeyLine(source, searchFrom, searchTo, name); ok {
		return found
	}
	return computed
}

func matchesKeyAt(source []byte, offset int, name string) bool {
	return offset >= 0 && offset+len(name) <= len(source) && string(source[offset:offset+len(name)]) == name
}

// findKeyLine finds "<name>:" (or "<name>" ending a line, unquoted mapping
// key form) at the first character of a line within [from, to). It never
// matches inside a longer identifier.
func findKeyLine(source []byte, from, to int, name string) (int, bool) {
	to = min(to, len(source))
	needle := []byte(name)
	for i := from; i+len(needle) <= to; i++ {
		if i > from && source[i-1] != '\n' {
			continue
		}
		// Skip this line's own leading indentation and, for a sequence
		// entry, its "- " marker.
		j := i
		for j < to && (source[j] == ' ' || source[j] == '\t') {
			j++
		}
		if j < to && source[j] == '-' {
			j++
			for j < to && (source[j] == ' ' || source[j] == '\t') {
				j++
			}
		}
		if j+len(needle) > to || string(source[j:j+len(needle)]) != name {
			continue
		}
		after := j + len(needle)
		if after < to && source[after] != ':' {
			continue
		}
		return j, true
	}
	return 0, false
}

// mappingKeyName returns a plain scalar mapping key's text. Any other key
// shape (a merge key, a complex key) never matches a schema field name, so
// callers simply find no match for it.
func mappingKeyName(key ast.MapKeyNode) string {
	if s, ok := key.(*ast.StringNode); ok {
		return s.Value
	}
	return key.String()
}

// findField looks up one field by name among spans.
func findField(spans []fieldSpan, name string) (fieldSpan, bool) {
	for _, s := range spans {
		if s.key == name {
			return s, true
		}
	}
	return fieldSpan{}, false
}

// sequenceEntryStart returns the byte offset of a block sequence entry's "-"
// marker, given the entry's own value node (the mapping or scalar the "-"
// introduces). [searchFrom, searchTo) bounds a fallback scan the same way
// verifiedKeyStart's does, and guards against the same rare offset skew.
func sequenceEntryStart(source []byte, entry ast.Node, searchFrom, searchTo int) int {
	start := startOfNode(source, entry)
	lineStart := bytes.LastIndexByte(source[:start], '\n') + 1
	computed := start
	if dash := bytes.IndexByte(source[lineStart:start], '-'); dash >= 0 {
		computed = lineStart + dash
	}
	if computed < len(source) && source[computed] == '-' {
		return computed
	}
	if found, ok := findDashLine(source, searchFrom, searchTo); ok {
		return found
	}
	return computed
}

// findDashLine finds the first line within [from, to) whose first
// non-whitespace character is "-".
func findDashLine(source []byte, from, to int) (int, bool) {
	to = min(to, len(source))
	for i := from; i < to; i++ {
		if i > from && source[i-1] != '\n' {
			continue
		}
		j := i
		for j < to && (source[j] == ' ' || source[j] == '\t') {
			j++
		}
		if j < to && source[j] == '-' {
			return j, true
		}
	}
	return 0, false
}

// sequenceEntrySpans returns every entry of a block sequence, in document
// order, with the same [sectionStart, sectionEnd) bounding and
// end-via-next-sibling convention as mappingFieldSpans.
func sequenceEntrySpans(source []byte, seq *ast.SequenceNode, sectionStart, sectionEnd int) []entrySpan {
	entries := seq.Values
	spans := make([]entrySpan, 0, len(entries))
	cursor := sectionStart
	for i, entry := range entries {
		start := sequenceEntryStart(source, entry, cursor, sectionEnd)
		end := sectionEnd
		if i+1 < len(entries) {
			end = sequenceEntryStart(source, entries[i+1], start, sectionEnd)
		}
		spans = append(spans, entrySpan{node: entry, start: start, end: end})
		cursor = start
	}
	return spans
}

type entrySpan struct {
	node  ast.Node
	start int
	end   int
}

// startOfNode returns a node's first token's start offset as a 0-indexed
// byte position into source, trimmed forward past any leading space or tab
// the token's Position.Offset includes. This is reliable in every case
// observed: a token's Position always marks somewhere at or before its own
// first meaningful character, never later.
//
// token.Position.Offset is 1-indexed (the scanner starts counting at 1, not
// 0: see scanner.go's initial s.offset = 1), so it is converted here once,
// in the one place every span computation in this package goes through.
func startOfNode(source []byte, node ast.Node) int {
	offset := tokenOffset(node.GetToken())
	for offset < len(source) && (source[offset] == ' ' || source[offset] == '\t') {
		offset++
	}
	return offset
}

// tokenOffset converts a token's 1-indexed Position.Offset to a 0-indexed
// byte position, clamped to zero so a malformed or synthetic token (Offset
// 0) never underflows.
func tokenOffset(tok *token.Token) int {
	if tok.Position.Offset <= 0 {
		return 0
	}
	return tok.Position.Offset - 1
}

// fieldValueSpan computes the exact [start, end) byte span of a mapping
// field's scalar value's own source representation — quote characters
// included for a quoted scalar — without ever reading past it into a
// trailing comment, the newline, or the next line's indentation. This is the
// span a value replacement may safely overwrite.
//
// Only plain, single-quoted, and double-quoted scalars in block context are
// supported. A block scalar (| or >), an anchor or alias, or a scalar living
// inside a flow collection returns an error instead of a guessed span.
func fieldValueSpan(source []byte, field fieldSpan) (start, end int, err error) {
	node := field.valueNode
	switch node.(type) {
	case *ast.StringNode, *ast.IntegerNode, *ast.BoolNode, *ast.FloatNode, *ast.NullNode:
	default:
		return 0, 0, fmt.Errorf("unsupported value node type %T for byte-range replacement", node)
	}

	start = fieldValueStart(source, field)
	switch node.GetToken().Type {
	case token.SingleQuoteType:
		end, err = scanSingleQuoted(source, start)
	case token.DoubleQuoteType:
		end, err = scanDoubleQuoted(source, start)
	case token.LiteralType, token.FoldedType:
		return 0, 0, fmt.Errorf("block scalar values are not supported for byte-range replacement")
	default:
		end = scanPlainScalarEnd(source, start)
	}
	return start, end, err
}

// fieldValueStart returns the offset of field's value, derived from the
// already-verified key start by skipping "<key>", ":", and the spaces or
// tabs after it, rather than trusted from the value token's own
// Position.Offset.
//
// go-yaml's scanner is observed to skew a value token's offset backwards
// onto the key's own ": " when a full-line comment sits above an enclosing
// mapping key and the sibling line just before this field carries a
// trailing comment — the same class of skew verifiedKeyStart guards against
// for keys. That skew is a small backwards shift within the value's own
// line, so a derived start on any other line means keyStart itself was not
// this field's key (a quoted key falls back to a literal search that can
// land on a same-named key elsewhere in the section). The token offset is
// therefore used whenever the key text cannot be matched at keyStart (a
// quoted key, "key :"), the value does not begin on the key's own line (an
// empty value, or one on the following line), or the derived start is not
// on the same line as the token offset.
func fieldValueStart(source []byte, field fieldSpan) int {
	computed := startOfNode(source, field.valueNode)
	if !matchesKeyAt(source, field.keyStart, field.key) {
		return computed
	}
	i := field.keyStart + len(field.key)
	if i >= len(source) || source[i] != ':' {
		return computed
	}
	i++
	for i < len(source) && (source[i] == ' ' || source[i] == '\t') {
		i++
	}
	if i >= len(source) || source[i] == '\n' || source[i] == '\r' || source[i] == '#' {
		return computed
	}
	if bytes.IndexByte(source[min(i, computed):max(i, computed)], '\n') >= 0 {
		return computed
	}
	return i
}

// scanSingleQuoted returns the offset just past the closing quote of a
// single-quoted scalar starting at start, which must be the opening "'".
// Two consecutive single quotes are YAML's own escape for one literal
// quote inside the scalar and do not terminate it.
func scanSingleQuoted(source []byte, start int) (int, error) {
	if start >= len(source) || source[start] != '\'' {
		return 0, fmt.Errorf("expected a single-quoted scalar at offset %d", start)
	}
	for i := start + 1; i < len(source); i++ {
		if source[i] != '\'' {
			continue
		}
		if i+1 < len(source) && source[i+1] == '\'' {
			i++ // escaped quote; skip both characters
			continue
		}
		return i + 1, nil
	}
	return 0, fmt.Errorf("single-quoted scalar starting at offset %d is never closed", start)
}

// scanDoubleQuoted returns the offset just past the closing quote of a
// double-quoted scalar starting at start, which must be the opening `"`.
// A backslash escapes the following character, including an escaped quote.
func scanDoubleQuoted(source []byte, start int) (int, error) {
	if start >= len(source) || source[start] != '"' {
		return 0, fmt.Errorf("expected a double-quoted scalar at offset %d", start)
	}
	for i := start + 1; i < len(source); i++ {
		switch source[i] {
		case '\\':
			i++ // skip the escaped character; a trailing lone backslash is malformed input the parser already rejected
		case '"':
			return i + 1, nil
		}
	}
	return 0, fmt.Errorf("double-quoted scalar starting at offset %d is never closed", start)
}

// scanPlainScalarEnd returns the offset just past a plain (unquoted) scalar
// starting at start, stopping at the first line break or the start of a
// trailing " #" comment, then trimming trailing spaces and tabs. Unlike its
// quoted-scalar siblings, a plain scalar has no malformed-input case to
// report: it always ends somewhere, so this never fails.
func scanPlainScalarEnd(source []byte, start int) int {
	i := start
	for i < len(source) {
		switch source[i] {
		case '\n', '\r':
			return trimTrailingBlank(source, start, i)
		case '#':
			if i > start && (source[i-1] == ' ' || source[i-1] == '\t') {
				return trimTrailingBlank(source, start, i)
			}
		}
		i++
	}
	return trimTrailingBlank(source, start, i)
}

func trimTrailingBlank(source []byte, start, end int) int {
	for end > start && (source[end-1] == ' ' || source[end-1] == '\t') {
		end--
	}
	return end
}
