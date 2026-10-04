package refresh

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
	"github.com/kkamegawa/gh-repospec/internal/config"
	yamlv3 "gopkg.in/yaml.v3"
)

// ApplyYAML rewrites original, a YAML document, so that every change in
// plan is applied while every other byte stays exactly as it was, including
// the file's own line-ending convention.
func ApplyYAML(original []byte, plan Plan) ([]byte, error) {
	ending := detectLineEnding(original)
	source := normalizeToLF(original)

	file, err := parseYAML(source)
	if err != nil {
		return nil, err
	}
	top, err := topLevelMapping(file)
	if err != nil {
		return nil, err
	}
	topSpans := mappingFieldSpans(source, top, len(source))

	var replacements []replacement

	for _, sc := range plan.Scalars {
		rep, err := scalarReplacement(source, topSpans, sc)
		if err != nil {
			return nil, fmt.Errorf("refresh %s: %w", sc.Path, err)
		}
		replacements = append(replacements, rep)
	}

	for _, cc := range plan.Collections {
		reps, err := collectionReplacements(source, topSpans, cc)
		if err != nil {
			return nil, fmt.Errorf("refresh %s: %w", cc.Path, err)
		}
		replacements = append(replacements, reps...)
	}

	if plan.Rulesets != nil {
		reps, err := rulesetReplacements(source, topSpans, plan.Rulesets)
		if err != nil {
			return nil, fmt.Errorf("refresh rulesets: %w", err)
		}
		replacements = append(replacements, reps...)
	}

	out, err := applyReplacements(source, replacements)
	if err != nil {
		return nil, err
	}
	return restoreLineEnding(out, ending), nil
}

// scalarReplacement resolves a dotted path such as "repository.description"
// or "actions.selectedActions.verifiedAllowed" against the top-level
// mapping and returns the byte-range replacement for its value.
func scalarReplacement(source []byte, topSpans []fieldSpan, change ScalarChange) (replacement, error) {
	segments := strings.Split(change.Path, ".")
	spans := topSpans
	var target fieldSpan
	for i, seg := range segments {
		field, ok := findField(spans, seg)
		if !ok {
			return replacement{}, fmt.Errorf("field %q is not declared in the file", seg)
		}
		if i == len(segments)-1 {
			target = field
			break
		}
		nested, ok := asBlockMapping(field.valueNode)
		if !ok {
			return replacement{}, fmt.Errorf("field %q is not a mapping", seg)
		}
		spans = mappingFieldSpans(source, nested, field.end)
	}

	start, end, err := fieldValueSpan(source, target)
	if err != nil {
		return replacement{}, err
	}
	text, err := renderScalar(target.valueNode, change.Value)
	if err != nil {
		return replacement{}, err
	}
	return replacement{Start: start, End: end, Text: []byte(text)}, nil
}

// renderScalar renders value as YAML source text, preserving the quote
// style of existing when it is a quoted string, so an edited field does not
// gratuitously change its own presentation.
func renderScalar(existing ast.Node, value ScalarValue) (string, error) {
	switch value.Kind {
	case ScalarBool:
		if value.Bool {
			return "true", nil
		}
		return "false", nil
	case ScalarString:
		if s, ok := existing.(*ast.StringNode); ok {
			switch s.GetToken().Type {
			case token.SingleQuoteType:
				return "'" + strings.ReplaceAll(value.Str, "'", "''") + "'", nil
			case token.DoubleQuoteType:
				return quoteDoubleYAML(value.Str), nil
			}
		}
		return renderPlainOrQuoted(value.Str), nil
	default:
		return "", fmt.Errorf("unsupported scalar kind %v", value.Kind)
	}
}

// renderPlainOrQuoted renders s unquoted when that is unambiguous YAML,
// and double-quoted otherwise. This is intentionally conservative: it
// quotes more than strictly necessary rather than risk emitting a plain
// scalar YAML would parse back as something other than a string.
func renderPlainOrQuoted(s string) string {
	if needsQuoting(s) {
		return quoteDoubleYAML(s)
	}
	return s
}

func needsQuoting(s string) bool {
	if s == "" {
		return true
	}
	if s != strings.TrimSpace(s) {
		return true
	}
	switch strings.ToLower(s) {
	case "true", "false", "yes", "no", "on", "off", "null", "~":
		return true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return true
	}
	for _, r := range s {
		switch r {
		case ':', '#', '\'', '"', '[', ']', '{', '}', ',', '&', '*', '!', '|', '>', '%', '@', '`', '\n', '\t':
			return true
		}
	}
	switch s[0] {
	case ' ', '-', '?', '@', '`', '"', '\'':
		return true
	}
	return false
}

func quoteDoubleYAML(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// emptyFlowSequenceSpan returns the exact [start, end) span of an empty
// flow sequence's "[]" text, including exactly one preceding space so
// replacing it with a newline-led block does not leave a trailing space on
// the declaring key's own line.
func emptyFlowSequenceSpan(source []byte, node ast.Node) (start, end int, err error) {
	start = startOfNode(source, node)
	if start >= len(source) || source[start] != '[' {
		return 0, 0, fmt.Errorf("expected an empty flow sequence (\"[]\")")
	}
	end = start + 1
	for end < len(source) && source[end] != ']' {
		end++
	}
	if end >= len(source) {
		return 0, 0, fmt.Errorf("flow sequence starting at offset %d is never closed", start)
	}
	end++
	if start > 0 && source[start-1] == ' ' {
		start--
	}
	return start, end, nil
}

// scalarText returns a plain-text mapping value's decoded content, used to
// match a sequence entry by one of its own fields (a label's name, a
// milestone's title).
func scalarText(node ast.Node) (string, bool) {
	if s, ok := node.(*ast.StringNode); ok {
		return s.Value, true
	}
	return "", false
}

// collectionReplacements turns one declared collection's planned updates
// and additions into byte-range replacements: an update narrowly rewrites
// only the sub-fields that changed within the matched entry, and an
// addition appends freshly rendered entries after the last existing one (or
// converts an empty declared "[]" into a populated block sequence).
func collectionReplacements(source []byte, topSpans []fieldSpan, change CollectionChange) ([]replacement, error) {
	segments := strings.Split(change.Path, ".")
	spans := topSpans
	var seqField fieldSpan
	for i, seg := range segments {
		field, ok := findField(spans, seg)
		if !ok {
			return nil, fmt.Errorf("field %q is not declared in the file", seg)
		}
		if i == len(segments)-1 {
			seqField = field
			break
		}
		nested, ok := asBlockMapping(field.valueNode)
		if !ok {
			return nil, fmt.Errorf("field %q is not a mapping", seg)
		}
		spans = mappingFieldSpans(source, nested, field.end)
	}

	seq, ok := asSequence(seqField.valueNode)
	if !ok {
		return nil, fmt.Errorf("%q is not a sequence", change.Path)
	}
	entrySpans := sequenceEntrySpans(source, seq, seqField.end)
	keyField := collectionKeyField(change.Path)

	var replacements []replacement
	for _, upd := range change.Updated {
		entry, ok := findEntrySpanByField(source, entrySpans, keyField, upd.Key)
		if !ok {
			return nil, fmt.Errorf("entry %q was not found for update", upd.Key)
		}
		mapping, ok := asBlockMapping(entry.node)
		if !ok {
			return nil, fmt.Errorf("entry %q is not a mapping", upd.Key)
		}
		fields := mappingFieldSpans(source, mapping, entry.end)
		for _, sc := range upd.Scalars {
			field, ok := findField(fields, sc.Path)
			if !ok {
				// The field is not present on this entry (e.g. an optional
				// description was never declared for this label); adding a
				// brand-new sub-field to an existing entry is out of scope
				// for this pass and is reported rather than guessed at.
				return nil, fmt.Errorf("entry %q has no field %q to update", upd.Key, sc.Path)
			}
			start, end, err := fieldValueSpan(source, field)
			if err != nil {
				return nil, fmt.Errorf("entry %q field %q: %w", upd.Key, sc.Path, err)
			}
			text, err := renderScalar(field.valueNode, sc.Value)
			if err != nil {
				return nil, err
			}
			replacements = append(replacements, replacement{Start: start, End: end, Text: []byte(text)})
		}
	}

	if len(change.Added) > 0 {
		indent := sequenceIndent(source, entrySpans, seqField)
		var b strings.Builder
		for _, add := range change.Added {
			b.WriteString(renderEntryBlock(indent, add.Fields))
		}
		if len(entrySpans) == 0 {
			// The collection is currently "[]"; replace that flow-empty
			// marker outright with a newline before the first block entry.
			start, end, err := emptyFlowSequenceSpan(source, seqField.valueNode)
			if err != nil {
				return nil, fmt.Errorf("%q: %w", change.Path, err)
			}
			replacements = append(replacements, replacement{Start: start, End: end, Text: []byte("\n" + strings.TrimSuffix(b.String(), "\n"))})
		} else {
			insertAt := trimTrailingBlank(source, seqField.keyStart, entrySpans[len(entrySpans)-1].end)
			replacements = append(replacements, replacement{Start: insertAt, End: insertAt, Text: []byte(b.String())})
		}
	}

	return replacements, nil
}

// collectionKeyField names the field that identifies one entry within a
// declared collection, for the handful of collection shapes this package
// refreshes.
func collectionKeyField(path string) string {
	switch path {
	case "milestones":
		return "title"
	default:
		return "name"
	}
}

func findEntrySpanByField(source []byte, spans []entrySpan, fieldName, key string) (entrySpan, bool) {
	for _, sp := range spans {
		mapping, ok := asBlockMapping(sp.node)
		if !ok {
			continue
		}
		fields := mappingFieldSpans(source, mapping, sp.end)
		field, ok := findField(fields, fieldName)
		if !ok {
			continue
		}
		text, ok := scalarText(field.valueNode)
		if ok && text == key {
			return sp, true
		}
	}
	return entrySpan{}, false
}

// sequenceIndent returns the indentation string used by this sequence's own
// entries, falling back to two spaces past the declaring key's own column
// for an empty collection, which has no entry to measure.
func sequenceIndent(source []byte, entries []entrySpan, seqField fieldSpan) string {
	if len(entries) > 0 {
		lineStart := entries[0].start
		for lineStart > 0 && source[lineStart-1] != '\n' {
			lineStart--
		}
		return string(source[lineStart:entries[0].start])
	}
	lineStart := seqField.keyStart
	for lineStart > 0 && source[lineStart-1] != '\n' {
		lineStart--
	}
	return string(source[lineStart:seqField.keyStart]) + "  "
}

// renderEntryBlock renders one sequence entry from an ordered field list,
// as a "- first: field\n  second: field\n" block at indent.
func renderEntryBlock(indent string, fields []FieldValue) string {
	var b strings.Builder
	for i, f := range fields {
		if i == 0 {
			b.WriteString(indent + "- ")
		} else {
			b.WriteString(indent + "  ")
		}
		value, _ := renderScalar(nil, f.Value)
		b.WriteString(f.Name + ": " + value + "\n")
	}
	return b.String()
}

// rulesetReplacements applies whole-entry ruleset updates and additions;
// see RulesetChange's doc comment for why rulesets are not refreshed field
// by field the way the simpler collections are.
func rulesetReplacements(source []byte, topSpans []fieldSpan, change *RulesetChange) ([]replacement, error) {
	field, ok := findField(topSpans, "rulesets")
	if !ok {
		if len(change.Updated) > 0 || len(change.Added) > 0 {
			return nil, fmt.Errorf("rulesets is not declared in the file")
		}
		return nil, nil
	}
	seq, ok := asSequence(field.valueNode)
	if !ok {
		return nil, fmt.Errorf("rulesets is not a sequence")
	}
	entrySpans := sequenceEntrySpans(source, seq, field.end)

	var replacements []replacement
	for _, updated := range change.Updated {
		entry, ok := findEntrySpanByField(source, entrySpans, "name", updated.Name)
		if !ok {
			return nil, fmt.Errorf("ruleset %q was not found for update", updated.Name)
		}
		text, err := renderRulesetBlock(sequenceIndent(source, entrySpans, field), updated)
		if err != nil {
			return nil, err
		}
		end := trimTrailingBlank(source, entry.start, entry.end)
		replacements = append(replacements, replacement{Start: entry.start, End: end, Text: []byte(text)})
	}

	if len(change.Added) > 0 {
		indent := sequenceIndent(source, entrySpans, field)
		var b strings.Builder
		for _, added := range change.Added {
			text, err := renderRulesetBlock(indent, added)
			if err != nil {
				return nil, err
			}
			b.WriteString(text)
		}
		if len(entrySpans) == 0 {
			start, end, err := emptyFlowSequenceSpan(source, field.valueNode)
			if err != nil {
				return nil, fmt.Errorf("rulesets: %w", err)
			}
			replacements = append(replacements, replacement{Start: start, End: end, Text: []byte("\n" + strings.TrimSuffix(b.String(), "\n"))})
		} else {
			insertAt := trimTrailingBlank(source, field.keyStart, entrySpans[len(entrySpans)-1].end)
			replacements = append(replacements, replacement{Start: insertAt, End: insertAt, Text: []byte(b.String())})
		}
	}
	return replacements, nil
}

// renderRulesetBlock renders a whole ruleset as a fresh block sequence
// entry via the same strict YAML encoding config.Load reads back, reindented
// to indent and prefixed with "- ".
func renderRulesetBlock(indent string, ruleset config.RulesetConfig) (string, error) {
	data, err := yamlv3.Marshal(ruleset)
	if err != nil {
		return "", fmt.Errorf("render ruleset: %w", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	var b strings.Builder
	for i, line := range lines {
		if i == 0 {
			b.WriteString(indent + "- " + line + "\n")
		} else {
			b.WriteString(indent + "  " + line + "\n")
		}
	}
	return b.String(), nil
}
