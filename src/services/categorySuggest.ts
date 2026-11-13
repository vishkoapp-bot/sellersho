// Category suggestion for the supplier "propose new product" form (step 1).
// Pure, synchronous, no network, no I/O. Implements
// launch-readiness/category-suggest/SPEC.md exactly: Persian normalization,
// compound merge (the تیشرت rule), the four-entry synonym map, leaf-only
// scoring, and the auto (>= 8, margin >= 2) / chips (>= 6) thresholds.
//
// The caller passes the CategoryDto[] the SupplierCategoryPicker already
// loaded (catalogApi.getCategories()) — this module never fetches.
import type { CategoryDto } from './catalogApi';

export interface SuggestedCategory {
  slug: string;
  title: string;
  path: { id: string; title: string; slug: string }[];
  pathDisplay: string; // titles joined with ' / '
  score: number;
  leafMatch: number;
}

export interface SuggestResult {
  verdict: 'auto' | 'chips' | 'none';
  top: SuggestedCategory[]; // 1 item when 'auto', up to 3 when 'chips', [] when 'none'
}

// ---------------------------------------------------------------------------
// 1. Normalization (spec §1, applied in this exact order)
// ---------------------------------------------------------------------------

export function normalizePersian(text: string): string {
  return (
    text
      // 1. Arabic -> Persian letters
      .replace(/ي/g, 'ی')
      .replace(/ك/g, 'ک')
      .replace(/ة/g, 'ه')
      // 2. Strip diacritics/tashkeel (U+064B–U+0652, U+0670) and tatweel (U+0640)
      .replace(/[ً-ْٰـ]/g, '')
      // 3. Delete ZWNJ (U+200C) and ZWJ (U+200D) entirely — do NOT space them
      .replace(/[‌‍]/g, '')
      // 4. Persian / Arabic digits -> Latin
      .replace(/[۰-۹]/g, (ch) => String('۰۱۲۳۴۵۶۷۸۹'.indexOf(ch)))
      .replace(/[٠-٩]/g, (ch) => String('٠١٢٣٤٥٦٧٨٩'.indexOf(ch)))
      // 5. Latin letters -> lowercase
      .toLowerCase()
      // 6. Punctuation / anything that is not a letter or digit -> space
      .replace(/[^\p{L}\p{N}]+/gu, ' ')
      // 7. Collapse repeated whitespace; trim
      .replace(/\s+/g, ' ')
      .trim()
  );
}

// ---------------------------------------------------------------------------
// 2. Tokenization (spec §2)
// ---------------------------------------------------------------------------

const STOPWORD = 'و';

/** Light plural strip, per token: های (len > 5) then ها (len > 4). */
function stripPlural(token: string): string {
  if (token.length > 5 && token.endsWith('های')) return token.slice(0, -3);
  if (token.length > 4 && token.endsWith('ها')) return token.slice(0, -2);
  return token;
}

/** Steps 1–3 of tokenize: normalize, split, drop the و stopword, plural-strip. */
function rawTokens(text: string): string[] {
  const normalized = normalizePersian(text);
  if (normalized === '') return [];
  return normalized
    .split(' ')
    .filter((t) => t !== '' && t !== STOPWORD)
    .map(stripPlural)
    .filter((t) => t !== '');
}

function dedupe(tokens: string[]): string[] {
  return [...new Set(tokens)];
}

/**
 * Compound merge (the تیشرت rule, spec §2.4). Scan left to right: if
 * tokens[i] + tokens[i+1] occurs as a token in some *leaf* title
 * (vocabulary built once from all leaf titles tokenized the same way),
 * merge them and skip ahead. Greedy, one pass.
 *
 * `compoundVocabulary` is optional: without it no merging happens (the
 * caller normally passes the vocabulary built from the same category
 * array via buildCompoundVocabulary).
 */
export function tokenize(text: string, compoundVocabulary?: ReadonlySet<string>): string[] {
  const tokens = rawTokens(text);
  if (!compoundVocabulary || compoundVocabulary.size === 0) return dedupe(tokens);
  const merged: string[] = [];
  for (let i = 0; i < tokens.length; i++) {
    if (i + 1 < tokens.length && compoundVocabulary.has(tokens[i] + tokens[i + 1])) {
      merged.push(tokens[i] + tokens[i + 1]);
      i++;
    } else {
      merged.push(tokens[i]);
    }
  }
  return dedupe(merged);
}

/** The compound-merge vocabulary: every raw token of every leaf title. */
export function buildCompoundVocabulary(categories: CategoryDto[]): Set<string> {
  const vocabulary = new Set<string>();
  for (const leaf of leafCategories(categories)) {
    for (const token of rawTokens(leaf.title)) vocabulary.add(token);
  }
  return vocabulary;
}

// ---------------------------------------------------------------------------
// 3. Synonym map (spec §3 — ONLY these four entries, each with a
//    leaf-title witness in the live tree). Applied as EXPANSION of the
//    title tokens before matching; expansion never creates new title
//    tokens for the < 2 tokens rule or the phrase bonus.
// ---------------------------------------------------------------------------

const SYNONYMS: Readonly<Record<string, readonly string[]>> = {
  // No leaf is titled «قاب گوشی»; phone cases live under «کیف و کاور گوشی».
  قاب: ['کاور'],
  // Leaf «گوشی موبایل»; path «لوازم جانبی گوشی موبایل» mixes both words.
  گوشی: ['موبایل'],
  موبایل: ['گوشی'],
  // Root «مادر و کودک» vs branch «لباس نوزاد».
  نوزاد: ['کودک'],
  کودک: ['نوزاد'],
  // Zero leaves contain «کتانی»; colloquial for sports shoe. Weakest
  // entry — it yields chips, never auto-select (see §7 example #8).
  کتانی: ['کفش', 'ورزشی'],
};

function expandTokens(tokens: string[]): Set<string> {
  const expanded = new Set(tokens);
  for (const token of tokens) {
    for (const alias of SYNONYMS[token] ?? []) expanded.add(alias);
  }
  return expanded;
}

// ---------------------------------------------------------------------------
// Leaf index (built once per categories array identity)
// ---------------------------------------------------------------------------

interface LeafEntry {
  dto: CategoryDto;
  leafTokens: string[];
  pathTokens: Set<string>;
  path: { id: string; title: string; slug: string }[];
  pathDisplay: string;
  depth: number;
}

interface LeafIndex {
  vocabulary: Set<string>;
  leaves: LeafEntry[];
}

const indexCache = new WeakMap<CategoryDto[], LeafIndex>();

function leafCategories(categories: CategoryDto[]): CategoryDto[] {
  const parentIds = new Set<string>();
  for (const c of categories) {
    if (typeof c.parentId === 'string' && c.parentId !== '') parentIds.add(c.parentId);
  }
  return categories.filter((c) => !parentIds.has(c.id));
}

function buildPath(leaf: CategoryDto, byId: Map<string, CategoryDto>): { id: string; title: string; slug: string }[] {
  const path: { id: string; title: string; slug: string }[] = [];
  const seen = new Set<string>();
  let current: CategoryDto | undefined = leaf;
  while (current && !seen.has(current.id)) {
    seen.add(current.id);
    path.unshift({ id: current.id, title: current.title, slug: current.slug });
    current = typeof current.parentId === 'string' && current.parentId !== '' ? byId.get(current.parentId) : undefined;
  }
  return path;
}

function getLeafIndex(categories: CategoryDto[]): LeafIndex {
  const cached = indexCache.get(categories);
  if (cached) return cached;
  const vocabulary = buildCompoundVocabulary(categories);
  const byId = new Map(categories.map((c) => [c.id, c]));
  const leaves: LeafEntry[] = leafCategories(categories).map((dto) => {
    const path = buildPath(dto, byId);
    const ancestors = path.slice(0, -1);
    const pathTokens = new Set<string>();
    for (const ancestor of ancestors) {
      for (const token of tokenize(ancestor.title, vocabulary)) pathTokens.add(token);
    }
    return {
      dto,
      leafTokens: tokenize(dto.title, vocabulary),
      pathTokens,
      path,
      pathDisplay: path.map((p) => p.title).join(' / '),
      depth: path.length,
    };
  });
  const index = { vocabulary, leaves };
  indexCache.set(categories, index);
  return index;
}

// ---------------------------------------------------------------------------
// 4. Scoring (spec §4 — leaves only, one pass)
// ---------------------------------------------------------------------------

/**
 * Greedy bipartite match of title tokens against the leaf's tokens:
 * direct matches first, then alias matches; each title token and each
 * leaf token is used at most once.
 */
function leafMatchCount(titleTokens: string[], leafTokens: string[]): number {
  const remaining = [...leafTokens];
  let count = 0;
  const unmatched: string[] = [];
  for (const token of titleTokens) {
    const idx = remaining.indexOf(token);
    if (idx >= 0) {
      remaining.splice(idx, 1);
      count++;
    } else {
      unmatched.push(token);
    }
  }
  for (const token of unmatched) {
    const aliases = SYNONYMS[token] ?? [];
    if (aliases.length === 0) continue;
    const idx = remaining.findIndex((leafToken) => aliases.includes(leafToken));
    if (idx >= 0) {
      remaining.splice(idx, 1);
      count++;
    }
  }
  return count;
}

function isContiguousSubsequence(needle: string[], haystack: string[]): boolean {
  if (needle.length === 0 || needle.length > haystack.length) return false;
  outer: for (let i = 0; i + needle.length <= haystack.length; i++) {
    for (let j = 0; j < needle.length; j++) {
      if (haystack[i + j] !== needle[j]) continue outer;
    }
    return true;
  }
  return false;
}

function arraysEqual(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((token, i) => token === b[i]);
}

interface RankedLeaf extends SuggestedCategory {
  coverage: number;
  tokenCount: number;
  depth: number;
}

function scoreLeaf(titleTokens: string[], expanded: Set<string>, entry: LeafEntry): RankedLeaf | null {
  const leafMatch = leafMatchCount(titleTokens, entry.leafTokens);
  if (leafMatch === 0) return null; // leaves with no leaf match are not scored
  let pathMatch = 0;
  for (const token of expanded) {
    if (entry.pathTokens.has(token)) pathMatch++;
  }
  const leafCoverage = leafMatch / entry.leafTokens.length;
  const phraseBonus = arraysEqual(titleTokens, entry.leafTokens)
    ? 4
    : isContiguousSubsequence(entry.leafTokens, titleTokens)
      ? 2
      : 0;
  const headBonus = titleTokens[0] === entry.leafTokens[0] ? 1 : 0;
  const score = 3 * leafMatch + 2 * pathMatch + Math.round(4 * leafCoverage) + phraseBonus + headBonus;
  return {
    slug: entry.dto.slug,
    title: entry.dto.title,
    path: entry.path,
    pathDisplay: entry.pathDisplay,
    score,
    leafMatch,
    coverage: leafCoverage,
    tokenCount: entry.leafTokens.length,
    depth: entry.depth,
  };
}

// ---------------------------------------------------------------------------
// 5. Thresholds and verdict (spec §5)
// ---------------------------------------------------------------------------

export function suggestCategories(title: string, categories: CategoryDto[]): SuggestResult {
  const { vocabulary, leaves } = getLeafIndex(categories);
  const titleTokens = tokenize(title, vocabulary);
  // < 2 title tokens -> no suggestions at all.
  if (titleTokens.length < 2) return { verdict: 'none', top: [] };
  const expanded = expandTokens(titleTokens);
  const ranked: RankedLeaf[] = [];
  for (const entry of leaves) {
    const scored = scoreLeaf(titleTokens, expanded, entry);
    if (scored) ranked.push(scored);
  }
  if (ranked.length === 0) return { verdict: 'none', top: [] };
  // Deterministic ranking: score desc, coverage desc, fewer leaf tokens
  // first, deeper leaf first, slug ascending.
  ranked.sort(
    (a, b) =>
      b.score - a.score ||
      b.coverage - a.coverage ||
      a.tokenCount - b.tokenCount ||
      b.depth - a.depth ||
      (a.slug < b.slug ? -1 : a.slug > b.slug ? 1 : 0),
  );
  const strip = (leaf: RankedLeaf): SuggestedCategory => ({
    slug: leaf.slug,
    title: leaf.title,
    path: leaf.path,
    pathDisplay: leaf.pathDisplay,
    score: leaf.score,
    leafMatch: leaf.leafMatch,
  });
  const top = ranked[0];
  const runnerUpScore = ranked.length > 1 ? ranked[1].score : 0;
  // AUTO iff top.score >= 8 AND margin >= 2 over runner-up AND >= 1
  // leaf-title token match.
  if (top.score >= 8 && top.score - runnerUpScore >= 2 && top.leafMatch >= 1) {
    return { verdict: 'auto', top: [strip(top)] };
  }
  // CHIPS iff not auto but top.score >= 6: top 3 ranked leaves.
  if (top.score >= 6) {
    return { verdict: 'chips', top: ranked.slice(0, 3).map(strip) };
  }
  return { verdict: 'none', top: [] };
}

// ---------------------------------------------------------------------------
// Form-side decision (spec §6): never override a manual pick.
// ---------------------------------------------------------------------------

export interface SelectionState {
  /** Slug of the currently selected category, or null when empty. */
  slug: string | null;
  /** True when the current selection was itself set by an auto-select. */
  autoApplied: boolean;
}

export interface DismissedAuto {
  title: string;
  slug: string;
}

export interface ApplySuggestionDecision {
  /** The leaf to auto-select now, or null to leave the selection alone. */
  apply: SuggestedCategory | null;
  /**
   * True when the current auto-applied pick no longer has a clear winner
   * behind it (verdict dropped to chips/none) and must be cleared so the
   * supplier is not left with a stale auto pick they never chose.
   */
  clearStaleAuto: boolean;
}

/**
 * Pure form-side rule for a fresh suggestion result. Auto-select fires
 * only when the category field is empty OR the current selection was
 * itself set by a previous auto-select for an earlier title. A hand pick
 * (picker gesture, chip tap) or a restored draft category counts as
 * manual and is never overridden; a dismissed auto suggestion is never
 * re-applied for the same title.
 */
export function decideAutoApply(
  result: SuggestResult,
  current: SelectionState,
  dismissed: DismissedAuto | null,
  title: string,
): ApplySuggestionDecision {
  if (result.verdict === 'auto') {
    const top = result.top[0];
    if (dismissed && dismissed.title === title && dismissed.slug === top.slug) {
      return { apply: null, clearStaleAuto: false };
    }
    if (current.slug === null) return { apply: top, clearStaleAuto: false };
    if (current.autoApplied) {
      return { apply: current.slug === top.slug ? null : top, clearStaleAuto: false };
    }
    return { apply: null, clearStaleAuto: false }; // manual pick wins
  }
  // Chips / none: the previous auto winner is gone — drop a stale
  // auto-applied pick (a manual pick is untouched).
  return { apply: null, clearStaleAuto: current.slug !== null && current.autoApplied };
}
