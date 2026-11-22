package repository

import (
	"context"
	"database/sql"
	"strings"
	"unicode"

	"github.com/drup/platform/internal/catalogpage"
)

// Batch 2, A1 — shared search matching for the three catalog predicates
// (priceFilterParts, GetProductCandidatePageTx, GetProducts). Go and SQL
// implement the same normalisation pipeline: the Go side derives the two
// match forms from catalogpage.SearchMatchForms, the SQL side applies
// LOWER + Arabic Yeh/Kaf mapping + U+200C deletion per column, in both
// the spaced form and the space-stripped form. Description search is
// kept: description-only matches stay in the result set, ranked last.
//
// Pre-filter design (Batch 2 pre-filter work, per prefilter-soundness.md):
// a row can only match if one of its RAW fields contains a rune from the
// pre-image set of the first LIKE-literal rune of the lower-cased
// space-stripped query (see firstPrefilterLiteral / prefilterPreimages).
// That is a NECESSARY condition, so it can only skip rows that cannot
// match. The pre-filter is ANDed with the field matches and the
// category EXISTS stays OUTSIDE it, grouped inside the search
// predicate: ((prefilter AND fieldMatches) OR categoryMatch), ANDed
// with the other filters by the callers. The pre-filter is a
// single-character containment test only — no two-character pre-filter
// (adjacency is unsound across raw spaces/ZWNJ). Empty-check first:
// spaced == "" → FALSE before any pre-filter, so whitespace/ZWNJ-only
// queries return 0 rows and never leak category matches.
//
// Fencing note (deviation from prefilter-soundness.md §7.3, measured):
// the MATERIALIZED-CTE fence was IMPLEMENTED and measured first — it
// is SLOWER than the flat shape (COUNT on the gate fixture: CTE
// 28.6/68.1 ms vs flat 15–21/29–31 ms for طلا/ساعت), because the CTE
// join re-scans and hash-joins all 10k products before filtering,
// while the flat shape's Bitmap Heap Scan applies the cheap POSITION
// pre-filter first and only then evaluates the REPLACE chains on the
// survivors (EXPLAIN on the gate fixture shows the POSITION qual
// preceding the REPLACE chains in the scan filter, Rows Removed
// 8039/10038 for طلا). Correctness does NOT depend on evaluation
// order — the pre-filter is a necessary condition ANDed with the
// field matches, so a planner reorder could only cost time, never
// change results — and the perf harness is the guard for that. The
// single-statement page (§3.2) still projects the rank in the inner
// query (see getPricedProductPageSearchTx) — but the rank is NOT
// computed once: rankExpr re-evaluates the title/brand/category
// chains, so WHERE + rank CASE = 2× per surviving row. The
// single-statement shape only removed the OUTER re-evaluation (the
// old two-query shape re-ran the whole predicate outside the CTE).

// searchZWNJ is U+200C ZERO WIDTH NON-JOINER — deleted at match time.
const searchZWNJ = "\u200c"

// searchNormSpacedExpr returns the SQL expression for the spaced normal
// form of a column — the exact Batch 2 chain (LOWER + Arabic Yeh
// U+064A → Persian Yeh U+06CC, Arabic Kaf U+0643 → Persian Kaf U+06A9,
// ZWNJ U+200C deleted). Identical result to the Go normaliser's
// match-time pipeline (see catalogpage.NormalizeSearchForMatch).
func searchNormSpacedExpr(col string) string {
	return "REPLACE(REPLACE(REPLACE(LOWER(" + col + "),'ي','ی'),'ك','ک'),'" + searchZWNJ + "','')"
}

// searchNormStrippedExpr returns the SQL expression for the
// space-stripped normal form of a column: the spaced form with every
// ASCII space deleted.
func searchNormStrippedExpr(col string) string {
	return "REPLACE(" + searchNormSpacedExpr(col) + ",' ','')"
}

// firstPrefilterLiteral returns the first LIKE-literal rune of the
// lower-cased space-stripped query, or ok=false when there is none or
// when fail-open applies. NEVER derive the key from the raw query or
// the spaced form (see prefilter-soundness.md §1.1): the raw first rune
// may be a space, a ZWNJ, an Arabic Yeh, or an uppercase letter, all
// of which normalisation changes or deletes; and a space is a literal
// in the spaced pattern but deleted in the stripped pattern, so a
// pre-filter on the spaced first literal would be UNSOUND for
// stripped-form matches.
func firstPrefilterLiteral(strippedLower string) (rune, bool) {
	rs := []rune(strippedLower) // rune-level: Persian is 2 bytes/rune.
	for i := 0; i < len(rs); i++ {
		switch rs[i] {
		case '%', '_':
			continue // wildcards constrain nothing by containment
		case '\\':
			if i+1 >= len(rs) {
				return 0, false // lone trailing escape: fail open
			}
			return rs[i+1], true // escaped rune is a literal
		default:
			if rs[i] == 0 {
				return 0, false // NUL: fail open
			}
			if rs[i] == ' ' || rs[i] == '\u200c' {
				return 0, false // cannot occur after normalisation; fail open
			}
			if unicode.Is(unicode.Mn, rs[i]) || unicode.Is(unicode.Me, rs[i]) {
				return 0, false // combining mark: fail open (§1.4)
			}
			return rs[i], true
		}
	}
	return 0, false // no literal at all: pre-filter is vacuous (TRUE)
}

// foldOrbit returns the unicode.SimpleFold orbit of r (every rune that
// folds to r under Go's single-rune case folding), in orbit order.
func foldOrbit(r rune) []rune {
	var out []rune
	seen := map[rune]bool{}
	for x := r; !seen[x]; {
		seen[x] = true
		out = append(out, x)
		x = unicode.SimpleFold(x)
	}
	return out
}

// prefilterPreimages returns the raw runes that can produce f (the first
// literal from firstPrefilterLiteral, already lower-cased by Go) under
// PG LOWER + the Yeh/Kaf/ZWNJ steps, or ok=false to fail open (vacuous
// pre-filter). The sets are deliberate SUPERSETS: PG LOWER is provider-
// and locale-dependent (libc simple mapping vs ICU full mapping), so a
// provider difference can only make the pre-filter looser, never
// unsound. Verified empirically (Go probe + PG-side check on this
// cluster, PG 16 libc C.UTF-8) — see the pre-filter report:
//   - Go ToLower('İ')='i' but SimpleFold('i') is only {i,I}: 'İ' is an
//     EXPLICIT extra for 'i' (the {f, upper(f)} shortcut would miss it
//     and be UNSOUND for a field containing only 'İ').
//   - 'k' orbit already contains K (U+212A); 'å' orbit already contains
//     Å (U+212B); 'ß' orbit is {ß,ẞ} (ToUpper('ß') is 'ß' itself, so the
//     {f, upper(f)} shortcut misses ẞ and is UNSOUND); 'σ' orbit is
//     {σ,Σ,ς} and 'ς' orbit is {ς,σ,Σ} (on this PG lower('ΟΣ')='οσ',
//     i.e. word-final Σ → σ, so ς is over-inclusion here but required
//     under ICU — superset, sound).
//   - 'μ' orbit is {μ,µ,Μ}: the table keeps µ distinct (LOWER('Μ')='μ',
//     never 'µ'), so including µ is over-inclusion only — a superset,
//     sound, noted here rather than special-cased.
//   - 's' orbit is {s,ſ,S}: ſ over-inclusion, superset, sound.
//
// Locale precondition: the pre-filter assumes the deployed C.UTF-8
// locale (PG LOWER = libc simple mapping). Under tr_TR, lower('I')
// is 'ı' (U+0131, dotless i), which sits outside the Go fold orbits
// assumed here — the Turkish ı case is a known hole under a Turkish
// locale; do not deploy under tr_TR without re-probing the
// pre-image sets.
func prefilterPreimages(f rune) ([]rune, bool) {
	switch f {
	case 'ی': // Persian Yeh
		return []rune{'ی', 'ي'}, true
	case 'ک': // Persian Kaf
		return []rune{'ک', 'ك'}, true
	case 'i':
		// Orbit {i,I} + explicit İ (U+0130). PG lower('İ') is 'i' on
		// this cluster (libc) and 'i'+U+0307 under ICU; either way a
		// field containing only İ must pass the pre-filter.
		return []rune{'i', 'I', 'İ'}, true
	}
	if unicode.Is(unicode.Mn, f) || unicode.Is(unicode.Me, f) {
		return nil, false
	}
	// Generic case-fold class via the SimpleFold orbit. For uncased
	// runes (Persian/Arabic letters, digits, punctuation, emoji, CJK)
	// the orbit is the singleton {f}, which is exact. For cased
	// letters it is the full fold class — never the {f, upper(f)}
	// shortcut.
	return foldOrbit(f), true
}

// searchPrefilterSQL builds the cheap necessary-condition pre-filter
// for one search term, using the POSITION form (a literal substring
// test, so '%', '_' and '\' need no LIKE escaping at all — the
// preferred form of prefilter-soundness.md §1.6). It allocates one
// bound arg per pre-image rune (reused across the three fields) through
// arg. It returns ok=false when the pre-filter is vacuous (fail open:
// no literal, combining-mark first literal, lone trailing backslash,
// or unknown fold class) — the caller then emits no pre-filter and
// the full predicate decides alone. Callers MUST check spaced == ""
// (empty) before calling: this function does not emit FALSE.
func searchPrefilterSQL(search string, arg func(any) string) (sql string, ok bool) {
	_, stripped := catalogpage.SearchMatchForms(search)
	f, ok := firstPrefilterLiteral(strings.ToLower(stripped))
	if !ok {
		return "", false
	}
	preimages, ok := prefilterPreimages(f)
	if !ok {
		return "", false
	}
	parts := make([]string, 0, len(preimages)*3)
	for _, c := range preimages {
		ph := arg(string(c))
		parts = append(parts,
			"POSITION("+ph+" IN p.title)>0",
			"POSITION("+ph+" IN p.description)>0",
			"POSITION("+ph+" IN p.brand)>0")
	}
	return "(" + strings.Join(parts, " OR ") + ")", true
}

// buildSearchCondition builds the WHERE fragment and the rank expression
// for one search term, appending the LIKE patterns through arg (the
// caller's positional-arg allocator) and the pre-filter POSITION args
// through the same allocator. Rank: 0 title match, 1 brand or
// category-title match, 2 description-only match. Brand is a plain
// column; category title is matched through an EXISTS over the real
// categories table, included only when hasCategories is true — the
// categories table is absent from the private-schema fixtures
// (catalog_page / perf01), and an EXISTS over a missing table fails at
// parse time, so the caller probes to_regclass('categories')
// (search_path-aware) first.
// Grouped shape (prefilter-soundness.md §3.1):
//
//	((prefilter AND (title OR description OR brand)) OR categoryMatch)
//
// The pre-filter is a necessary condition for the field matches only;
// the category match is ORed inside the same group, and the whole
// group is ANDed with the other filters by the callers — a category
// match can never bypass is_active or any other filter. When the
// pre-filter is vacuous (fail open: no literal, combining-mark first
// literal, lone trailing backslash), the shape reduces to the Batch 2
// as-is predicate.
// A term that normalises to nothing (only spaces/ZWNJ) matches nothing:
// the spaced == "" check runs BEFORE any pre-filter, so such queries
// return FALSE and never leak category matches.
func buildSearchCondition(search string, arg func(any) string, hasCategories bool) (whereClause, rankExpr string) {
	spaced, stripped := catalogpage.SearchMatchForms(search)
	if spaced == "" {
		return "FALSE", "0"
	}
	spacedArg := arg("%" + strings.ToLower(spaced) + "%")
	strippedArg := arg("%" + strings.ToLower(stripped) + "%")
	field := func(col string) string {
		return "(" + searchNormSpacedExpr(col) + " LIKE " + spacedArg + " OR " + searchNormStrippedExpr(col) + " LIKE " + strippedArg + ")"
	}
	title := field("p.title")
	description := field("p.description")
	brand := field("p.brand")
	category := "FALSE"
	if hasCategories {
		category = "EXISTS(SELECT 1 FROM categories c WHERE c.slug = p.category_slug AND " + field("c.title") + ")"
	}
	prefilter, hasPrefilter := searchPrefilterSQL(search, arg)
	fieldMatches := "(" + title + " OR " + description + " OR " + brand + ")"
	if hasPrefilter {
		fieldMatches = "(" + prefilter + " AND " + fieldMatches + ")"
	}
	whereClause = "(" + fieldMatches + " OR " + category + ")"
	rankExpr = "CASE WHEN " + title + " THEN 0 WHEN " + brand + " OR " + category + " THEN 1 ELSE 2 END"
	return whereClause, rankExpr
}

// scanRow scans one page row: dests hold the sort columns plus, as the
// last element, the rank destination. On a non-search page the rank
// column is not selected, so the trailing dest is dropped.
func scanRow(rows *sql.Rows, searching bool, dests ...any) error {
	if !searching {
		dests = dests[:len(dests)-1]
	}
	return rows.Scan(dests...)
}

// setSearchJITOff disables JIT for the search statements in tx.
// NOTE: JIT never engages at this scale — the search page plan cost
// on the gate fixture is ~6.7k, far below jit_above_cost=100000, so
// the gate pass does not depend on this setting. The SET LOCAL is
// kept as belt-and-braces for larger deployments where a plan could
// cross the JIT threshold (JIT compilation, ~46–75 ms per execution
// in the scratch benchmark, is pure overhead on a single 10k-row scan
// that never pays back). SET LOCAL lasts until tx end, so it cannot
// leak to other requests on the pooled connection; it is issued only
// when the filter actually searches, and only in the search
// transaction. Best-effort: a failure here must not fail a listing
// (JIT on is merely slower).
func setSearchJITOff(ctx context.Context, tx *sql.Tx) {
	_, _ = tx.ExecContext(ctx, `SET LOCAL jit = off`)
}

// categoriesTableExistsTx probes for the categories table the way
// suppliersTableExists probes for suppliers: to_regclass with an
// unqualified name, so it respects the caller's search_path (the
// private-schema fixtures have no categories copy).
func categoriesTableExistsTx(ctx context.Context, tx *sql.Tx) bool {
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('categories') IS NOT NULL`).Scan(&exists); err != nil {
		return false
	}
	return exists
}

// categoriesTableExists is the *sql.DB form of categoriesTableExistsTx.
func (r *CatalogRepository) categoriesTableExists(ctx context.Context) bool {
	var exists bool
	if err := r.db.QueryRowContext(ctx, `SELECT to_regclass('categories') IS NOT NULL`).Scan(&exists); err != nil {
		return false
	}
	return exists
}
