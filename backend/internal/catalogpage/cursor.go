// Package catalogpage defines the bounded public catalog paging contract.
// It is not wired to public handlers until the authoritative price index and
// all maintained consumers are migrated together.
package catalogpage

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/drup/platform/internal/pricing"
)

const DefaultSize = 24
const MaxSize = 100
const MaxCursorBytes = 2048

var ErrInvalid = errors.New("invalid catalog page request")
var ErrRestart = errors.New("catalog changed or cursor belongs to another query; restart paging")

// ParseSize has no zero/unbounded mode. An obsolete offset page cannot silently
// return the first page repeatedly. Page 1 is accepted during client migration.
func ParseSize(values url.Values) (int, error) {
	for _, name := range []string{"limit", "pageSize", "page"} {
		if v, ok := values[name]; ok && (len(v) != 1 || v[0] == "") {
			return 0, ErrInvalid
		}
	}
	if v := values.Get("page"); v != "" && v != "1" {
		return 0, ErrInvalid
	}
	_, limit := values["limit"]
	_, pageSize := values["pageSize"]
	if limit && pageSize {
		return 0, ErrInvalid
	}
	raw := values.Get("limit")
	if pageSize {
		raw = values.Get("pageSize")
	}
	if raw == "" {
		return DefaultSize, nil
	}
	// Reject signs, exponents and surrounding whitespace consistently.
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0, ErrInvalid
		}
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > MaxSize {
		return 0, ErrInvalid
	}
	return n, nil
}

// Scope must come from canonical server-side store resolution, never a cursor.
type Scope struct {
	ID        string
	OwnerType string
	Revision  int64
}

// Filter contains public, SQL-applied filters. StorePosition is derived from the
// resolved store and sort, not an independently authorized client parameter.
type Filter struct {
	Category    string
	Niche       string
	Search      string
	Sort        string
	Brands      []string
	IsOriginal  *bool
	InStockOnly bool
	// VisibleStockOnly applies the stricter storefront stock filter to
	// selectable variants. It is derived only for a public storefront.
	VisibleStockOnly bool
	MinPrice         *int64
	MaxPrice         *int64
	Attributes       map[int][]int
	StorePosition    bool
	// IncludeInactive is the B1(a) carve-out: the public list never
	// sets it. handleProductsList sets it only for an authenticated
	// caller who owns the requested dropshipper store, so seller
	// management still sees inactive assortment products.
	IncludeInactive bool
}

// Key carries public ordering values only. In particular, supplier base costs
// are never an ordering key or a cursor field. ID breaks every sort tie.
// Rank (Batch 2, A1) is the search match rank — 0 title, 1 brand/category,
// 2 description-only — and is 0 whenever the page is not a search page.
type Key struct {
	ID        string    `json:"id"`
	Featured  bool      `json:"featured,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	Position  int64     `json:"position,omitempty"`
	Price     int64     `json:"price,omitempty"`
	Discount  int       `json:"discount,omitempty"`
	Rank      int       `json:"rank,omitempty"`
}

// CursorVersion is the cursor wire version. v2 (Batch 2, A1) added Key.Rank:
// v1 cursors carry no rank and would mis-page a search that continues
// across the deploy, so v1 tokens fail closed with ErrInvalid.
const CursorVersion = 2

type cursor struct {
	Version int    `json:"v"`
	Binding string `json:"binding"`
	Key     Key    `json:"key"`
}
type attribute struct {
	ID     int
	Values []int
}
type canonical struct {
	Scope                         Scope
	Category, Niche, Search, Sort string
	Brands                        []string
	IsOriginal                    *bool
	InStockOnly                   bool
	MinPrice, MaxPrice            *int64
	Attributes                    []attribute
	StorePosition                 bool
	IncludeInactive               bool
	VisibleStockOnly              bool `json:",omitempty"`
}

func validText(value string, max int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= max && !strings.ContainsFunc(value, unicode.IsControl)
}

func bind(scope Scope, filter Filter) (string, Filter, error) {
	if scope.ID == "" || !validText(scope.ID, 100) || scope.Revision < 1 || (scope.OwnerType != "central" && scope.OwnerType != "dropshipper") {
		return "", filter, ErrInvalid
	}
	if filter.Sort == "" {
		filter.Sort = "featured"
	}
	switch filter.Sort {
	case "featured", "newest", "cheapest", "expensive", "discount":
	default:
		return "", filter, ErrInvalid
	}
	if filter.Category == "all" {
		filter.Category = ""
	}
	if filter.Niche == "all" {
		filter.Niche = ""
	}
	if !validText(filter.Category, 100) || !validText(filter.Niche, 100) || !validText(filter.Search, 256) || len(filter.Brands) > 10 || len(filter.Attributes) > 20 {
		return "", filter, ErrInvalid
	}
	for _, amount := range []*int64{filter.MinPrice, filter.MaxPrice} {
		if amount != nil && (*amount < 0 || *amount > pricing.MaxTransportMoney) {
			return "", filter, ErrInvalid
		}
	}
	if filter.MinPrice != nil && filter.MaxPrice != nil && *filter.MinPrice > *filter.MaxPrice {
		return "", filter, ErrInvalid
	}
	brands := make([]string, 0, len(filter.Brands))
	for _, brand := range filter.Brands {
		if brand == "" || !validText(brand, 100) {
			return "", filter, ErrInvalid
		}
		brands = append(brands, brand)
	}
	sort.Strings(brands)
	brands = unique(brands)
	attrs := make([]attribute, 0, len(filter.Attributes))
	for id, values := range filter.Attributes {
		if id < 1 || len(values) > 20 {
			return "", filter, ErrInvalid
		}
		copyValues := append([]int{}, values...)
		for _, value := range copyValues {
			if value < 1 {
				return "", filter, ErrInvalid
			}
		}
		if len(copyValues) == 0 {
			continue
		}
		sort.Ints(copyValues)
		attrs = append(attrs, attribute{ID: id, Values: unique(copyValues)})
	}
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].ID < attrs[j].ID })
	if filter.StorePosition && filter.Sort != "featured" {
		return "", filter, ErrInvalid
	}
	if filter.VisibleStockOnly && (!filter.InStockOnly || scope.OwnerType != "dropshipper") {
		return "", filter, ErrInvalid
	}
	data, err := json.Marshal(canonical{scope, filter.Category, filter.Niche, filter.Search, filter.Sort, brands, filter.IsOriginal, filter.InStockOnly, filter.MinPrice, filter.MaxPrice, attrs, filter.StorePosition, filter.IncludeInactive, filter.VisibleStockOnly})
	if err != nil {
		return "", filter, ErrInvalid
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), filter, nil
}

func unique[T comparable](values []T) []T {
	out := values[:0]
	for _, value := range values {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}

func validateKey(filter Filter, key Key) error {
	if key.ID == "" || !validText(key.ID, 50) || key.Price < 0 || key.Price > pricing.MaxTransportMoney || key.Discount < 0 || key.Discount > 100 {
		return ErrInvalid
	}
	// Batch 2, A1: Rank is 0..2, and a non-zero rank is only meaningful
	// on a search page — a key with Rank != 0 and an empty search term
	// is invalid.
	if key.Rank < 0 || key.Rank > 2 || (filter.Search == "" && key.Rank != 0) {
		return ErrInvalid
	}
	if filter.StorePosition {
		// store_products.position is a signed PostgreSQL integer, with no
		// nonnegative constraint. Existing negative positions remain pageable.
		if key.Position < -2147483648 || key.Position > 2147483647 {
			return ErrInvalid
		}
		if key.Featured || !key.CreatedAt.IsZero() || key.Price != 0 || key.Discount != 0 {
			return ErrInvalid
		}
		return nil
	}
	if key.Position != 0 {
		return ErrInvalid
	}
	switch filter.Sort {
	case "featured", "newest":
		if key.CreatedAt.IsZero() || key.CreatedAt.Year() < 1 || key.CreatedAt.Year() > 9999 || key.Price != 0 || key.Discount != 0 || (filter.Sort == "newest" && key.Featured) {
			return ErrInvalid
		}
	case "cheapest", "expensive":
		if key.Featured || !key.CreatedAt.IsZero() || key.Discount != 0 {
			return ErrInvalid
		}
	case "discount":
		if key.Featured || !key.CreatedAt.IsZero() || key.Price != 0 {
			return ErrInvalid
		}
	}
	return nil
}

// Validate checks the independently resolved first-page request, or an already
// decoded key, without creating a token. It does not authorize a scope/revision.
func Validate(scope Scope, filter Filter, key *Key) error {
	_, normalized, err := bind(scope, filter)
	if err != nil || key == nil {
		return err
	}
	return validateKey(normalized, *key)
}

func Encode(scope Scope, filter Filter, key Key) (string, error) {
	binding, normalized, err := bind(scope, filter)
	if err != nil {
		return "", err
	}
	if err = validateKey(normalized, key); err != nil {
		return "", err
	}
	if !key.CreatedAt.IsZero() {
		key.CreatedAt = key.CreatedAt.UTC()
	}
	data, err := json.Marshal(cursor{Version: CursorVersion, Binding: binding, Key: key})
	if err != nil {
		return "", ErrInvalid
	}
	token := base64.RawURLEncoding.EncodeToString(data)
	if len(token) > MaxCursorBytes {
		return "", ErrInvalid
	}
	return token, nil
}

// Decode binds the cursor to filters resolved independently for this request.
// This is query identity, not authorization or a secret MAC; callers MUST apply
// tenant/visibility predicates again and compare the authoritative revision.
func Decode(token string, scope Scope, filter Filter) (Key, error) {
	if token == "" || len(token) > MaxCursorBytes {
		return Key{}, ErrInvalid
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || !utf8.Valid(data) || rejectDuplicates(data) != nil {
		return Key{}, ErrInvalid
	}
	var parsed cursor
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&parsed) != nil || parsed.Version != CursorVersion || len(parsed.Binding) != 64 {
		return Key{}, ErrInvalid
	}
	if _, err = hex.DecodeString(parsed.Binding); err != nil {
		return Key{}, ErrInvalid
	}
	binding, normalized, err := bind(scope, filter)
	if err != nil {
		return Key{}, err
	}
	if parsed.Binding != binding {
		return Key{}, ErrRestart
	}
	if err = validateKey(normalized, parsed.Key); err != nil {
		return Key{}, err
	}
	if !parsed.Key.CreatedAt.IsZero() {
		parsed.Key.CreatedAt = parsed.Key.CreatedAt.UTC()
	}
	return parsed.Key, nil
}

func rejectDuplicates(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var visit func(int) error
	visit = func(depth int) error {
		if depth > 4 {
			return ErrInvalid
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		if delim != '{' {
			return ErrInvalid
		}
		seen := map[string]bool{}
		for d.More() {
			name, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := name.(string)
			if !ok {
				return ErrInvalid
			}
			// Match encoding/json's Unicode case folding as well as exact
			// spelling, so v/V and price/Price cannot overwrite one another.
			for previous := range seen {
				if strings.EqualFold(previous, key) {
					return ErrInvalid
				}
			}
			seen[key] = true
			if err := visit(depth + 1); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return ErrInvalid
		}
		return nil
	}
	if err := visit(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	return nil
}
