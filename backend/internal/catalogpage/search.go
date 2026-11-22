package catalogpage

import "strings"

// NormalizeSearchForMatch normalises a search term for MATCHING (Batch 2,
// A1). It is the match-time twin of the logging normaliser
// (cmd/server normalizeSearchTerm): trim, collapse whitespace runs to one
// space, map Arabic Yeh/Kaf to Persian Yeh/Kaf — plus one extra step,
// deleting every U+200C ZERO WIDTH NON-JOINER — so «هندز‌فری» (ZWNJ),
// «هندز فری» (space) and «هندزفری» (plain) all match. Digits are NOT
// converted, matching the logging normaliser's documented rule.
//
// The repository derives both match forms from this one function, and the
// SQL predicates implement the identical pipeline (LOWER + Yeh/Kaf +
// ZWNJ-strip, plus a space-stripped comparison), so Go and SQL cannot
// drift apart in the Yeh/Kaf/ZWNJ rules. The logging normaliser itself
// is deliberately NOT changed: logging and matching stay separate.
func NormalizeSearchForMatch(term string) string {
	collapsed := strings.Join(strings.Fields(term), " ")
	var b strings.Builder
	for _, r := range collapsed {
		switch r {
		case 'ي':
			b.WriteRune('ی')
		case 'ك':
			b.WriteRune('ک')
		case '\u200c': // U+200C ZERO WIDTH NON-JOINER: deleted at match time.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SearchMatchForms returns the two forms a search term is compared in:
// the spaced normal form (preserves multi-word phrases) and the
// space-stripped form (so a spaced query matches a joined title and
// vice versa). Both forms are lower-cased by the caller where the SQL
// comparison needs it.
func SearchMatchForms(term string) (spaced, stripped string) {
	spaced = NormalizeSearchForMatch(term)
	stripped = strings.ReplaceAll(spaced, " ", "")
	return spaced, stripped
}
