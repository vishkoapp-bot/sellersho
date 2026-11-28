package catalogpage

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPageSizeCannotRequestAnUnboundedCatalog(t *testing.T) {
	for _, tc := range []struct {
		query string
		size  int
		valid bool
	}{
		{"", 24, true}, {"limit=100", 100, true}, {"pageSize=1", 1, true},
		{"page=1&limit=24", 24, true}, {"limit=0", 0, false}, {"limit=-1", 0, false},
		{"limit=101", 0, false}, {"limit=abc", 0, false}, {"limit=99999999999999999999", 0, false},
		{"limit=24&limit=100", 0, false}, {"pageSize=24&limit=24", 0, false},
		{"page=2", 0, false}, {"page=0", 0, false}, {"limit=", 0, false},
	} {
		t.Run(tc.query, func(t *testing.T) {
			values, _ := url.ParseQuery(tc.query)
			size, err := ParseSize(values)
			if (err == nil) != tc.valid || size != tc.size {
				t.Fatalf("size=%d err=%v", size, err)
			}
		})
	}
}

func TestCursorBindsResolvedTenantRevisionAndCanonicalFilters(t *testing.T) {
	scope := Scope{ID: "store-A", OwnerType: "dropshipper", Revision: 3}
	filter := Filter{Sort: "cheapest", Brands: []string{"B", "A", "B"}, Attributes: map[int][]int{9: {4, 3, 4}}}
	key := Key{ID: "public-product", Price: 120000}
	token, err := Encode(scope, filter, key)
	if err != nil {
		t.Fatal(err)
	}
	canonical := Filter{Sort: "cheapest", Brands: []string{"A", "B"}, Attributes: map[int][]int{9: {3, 4}}}
	got, err := Decode(token, scope, canonical)
	if err != nil || got != key {
		t.Fatalf("key=%+v err=%v", got, err)
	}
	for _, other := range []Scope{{ID: "store-B", OwnerType: "dropshipper", Revision: 3}, {ID: "store-A", OwnerType: "central", Revision: 3}, {ID: "store-A", OwnerType: "dropshipper", Revision: 4}} {
		if _, err := Decode(token, other, canonical); !errors.Is(err, ErrRestart) {
			t.Fatalf("scope %+v accepted: %v", other, err)
		}
	}
	falseValue, min, max := false, int64(1), int64(500000)
	var changed []Filter
	for _, mutate := range []func(*Filter){
		func(f *Filter) { f.Sort = "expensive" }, func(f *Filter) { f.Search = "different" },
		func(f *Filter) { f.Category = "category-B" }, func(f *Filter) { f.Niche = "niche-B" },
		func(f *Filter) { f.Brands = []string{"A", "C"} },
		func(f *Filter) { f.Attributes = map[int][]int{9: {3, 5}} },
		func(f *Filter) { f.IsOriginal = &falseValue }, func(f *Filter) { f.InStockOnly = true },
		func(f *Filter) { f.MinPrice = &min }, func(f *Filter) { f.MaxPrice = &max },
	} {
		other := canonical
		mutate(&other)
		changed = append(changed, other)
	}
	for _, other := range changed {
		if _, err := Decode(token, scope, other); !errors.Is(err, ErrRestart) {
			t.Fatalf("changed filter accepted: %v", err)
		}
	}
	decoded, _ := base64.RawURLEncoding.DecodeString(token)
	if strings.Contains(string(decoded), "supplier") || strings.Contains(string(decoded), "baseCost") || strings.Contains(string(decoded), "store-A") {
		t.Fatal("cursor contains protected or unnecessary tenant fields")
	}
}

func TestCursorBindsStorefrontVariantAvailability(t *testing.T) {
	scope := Scope{ID: "store-A", OwnerType: "dropshipper", Revision: 1}
	filter := Filter{Sort: "featured", StorePosition: true, InStockOnly: true, VisibleStockOnly: true}
	token, err := Encode(scope, filter, Key{ID: "visible-product", Position: 2})
	if err != nil {
		t.Fatal(err)
	}
	filter.VisibleStockOnly = false
	if _, err := Decode(token, scope, filter); !errors.Is(err, ErrRestart) {
		t.Fatalf("reused storefront cursor with legacy stock filter: %v", err)
	}
}

func TestDefaultAndEmptyFiltersHaveOneCursorIdentity(t *testing.T) {
	scope := Scope{ID: "central", OwnerType: "central", Revision: 1}
	key := Key{ID: "p-1", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	token, err := Encode(scope, Filter{}, key)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Decode(token, scope, Filter{Sort: "featured", Category: "all", Niche: "all", Brands: []string{}, Attributes: map[int][]int{1: {}}})
	if err != nil {
		t.Fatalf("equivalent defaults changed query identity: %v", err)
	}
}

func TestCursorHandlesTiedPublicSortKeysAndPreservesTimestampPrecision(t *testing.T) {
	scope := Scope{ID: "central", OwnerType: "central", Revision: 1}
	created := time.Date(2026, 11, 14, 3, 2, 1, 123456000, time.UTC)
	for _, tc := range []struct {
		filter Filter
		key    Key
	}{
		{Filter{}, Key{ID: "p-1", Featured: true, CreatedAt: created}},
		{Filter{Sort: "newest"}, Key{ID: "p-2", CreatedAt: created}},
		{Filter{StorePosition: true}, Key{ID: "p-3", Position: 0}},
		{Filter{Sort: "cheapest"}, Key{ID: "p-4", Price: 1}},
		{Filter{Sort: "discount"}, Key{ID: "p-5", Discount: 100}},
	} {
		token, err := Encode(scope, tc.filter, tc.key)
		if err != nil {
			t.Fatal(err)
		}
		key, err := Decode(token, scope, tc.filter)
		if err != nil || key != tc.key {
			t.Fatalf("round trip %+v -> %+v: %v", tc.key, key, err)
		}
	}
}

func TestCursorRejectsMalformedAndOverlargePayloads(t *testing.T) {
	scope := Scope{ID: "central", OwnerType: "central", Revision: 1}
	filter := Filter{Sort: "cheapest"}
	valid, _ := Encode(scope, filter, Key{ID: "p-1", Price: 10})
	raw, _ := base64.RawURLEncoding.DecodeString(valid)
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "%%%", strings.Repeat("a", 2049), base64.RawURLEncoding.EncodeToString(append(raw, []byte(" {}")...)), base64.RawURLEncoding.EncodeToString([]byte(`{"v":999}`))} {
		if _, err := Decode(token, scope, filter); !errors.Is(err, ErrInvalid) {
			t.Fatalf("malformed cursor accepted (%d bytes): %v", len(token), err)
		}
	}
	payload["supplierBaseCost"] = 100
	raw, _ = json.Marshal(payload)
	if _, err := Decode(base64.RawURLEncoding.EncodeToString(raw), scope, filter); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown/protected field accepted: %v", err)
	}
	if _, err := Encode(scope, filter, Key{ID: "p-1", Price: -1}); !errors.Is(err, ErrInvalid) {
		t.Fatal("negative price accepted")
	}
	if _, err := Encode(scope, Filter{Sort: "supplier-cost"}, Key{ID: "p-1"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("unsupported sort accepted")
	}
	for _, raw := range []string{
		`{"v":1,"v":1,"binding":"` + strings.Repeat("0", 64) + `","key":{"id":"p-1"}}`,
		`{"v":1,"binding":"` + strings.Repeat("0", 64) + `","key":{"id":"p-1","id":"p-2"}}`,
		`{"v":1,"binding":"` + strings.Repeat("0", 64) + `","key":{"id":"p-1","price":1.5}}`,
	} {
		if _, err := Decode(base64.RawURLEncoding.EncodeToString([]byte(raw)), scope, filter); !errors.Is(err, ErrInvalid) {
			t.Fatalf("ambiguous cursor accepted: %v", err)
		}
	}
	invalidUTF8 := append([]byte(`{"v":1,"binding":"`+payload["binding"].(string)+`","key":{"id":"`), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`","price":10}}`)...)
	if _, err := Decode(base64.RawURLEncoding.EncodeToString(invalidUTF8), scope, filter); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid UTF-8 cursor accepted")
	}
}

func FuzzDecodeRejectsUntrustedCursorsWithoutPanicking(f *testing.F) {
	scope := Scope{ID: "central", OwnerType: "central", Revision: 1}
	filter := Filter{Sort: "cheapest"}
	valid, err := Encode(scope, filter, Key{ID: "p-1", Price: 100})
	if err != nil {
		f.Fatal(err)
	}
	for _, token := range []string{"", "%%%", valid, strings.Repeat("a", 2049)} {
		f.Add(token)
	}
	f.Fuzz(func(t *testing.T, token string) {
		key, err := Decode(token, scope, filter)
		if err == nil && (key.ID == "" || key.Price < 0) {
			t.Fatalf("invalid decoded key: %+v", key)
		}
	})
}

func TestCursorRejectsCaseFoldedJSONAliases(t *testing.T) {
	scope := Scope{ID: "central", OwnerType: "central", Revision: 1}
	filter := Filter{Sort: "cheapest"}
	valid, err := Encode(scope, filter, Key{ID: "p-1", Price: 10})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(valid)
	for _, mutated := range []string{
		strings.Replace(string(raw), `"v":2`, `"v":2,"V":2`, 1),
		strings.Replace(string(raw), `"price":10`, `"price":10,"Price":11`, 1),
	} {
		if _, err := Decode(base64.RawURLEncoding.EncodeToString([]byte(mutated)), scope, filter); !errors.Is(err, ErrInvalid) {
			t.Fatalf("case-alias duplicate accepted: %v", err)
		}
	}
}

func TestCursorSupportsSignedDatabaseStorePositions(t *testing.T) {	scope := Scope{ID: "store-A", OwnerType: "dropshipper", Revision: 1}
	filter := Filter{StorePosition: true}
	for _, position := range []int64{-2147483648, -1, 0, 2147483647} {
		key := Key{ID: "p-1", Position: position}
		token, err := Encode(scope, filter, key)
		if err != nil {
			t.Fatalf("valid SQL integer position%d rejected: %v", position, err)
		}
		got, err := Decode(token, scope, filter)
		if err != nil || got != key {
			t.Fatalf("position round trip: %+v %v", got, err)
		}
	}
	for _, position := range []int64{-2147483649, 2147483648} {
		if _, err := Encode(scope, filter, Key{ID: "p-1", Position: position}); !errors.Is(err, ErrInvalid) {
			t.Fatal("out-of-range position accepted")
		}
	}
}

// Batch 2, A1: a pre-change (v1) search cursor fails closed after the
// version bump, and Rank validates as 0..2, search-only.
func TestCursorV1TokenRejectedAfterVersionBump(t *testing.T) {
	scope := Scope{ID: "central", OwnerType: "central", Revision: 1}
	filter := Filter{Sort: "featured", Search: "ساعت"}
	key := Key{ID: "p-1", Featured: true, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Rank: 2}
	token, err := Encode(scope, filter, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(token)
	if !strings.Contains(string(raw), `"v":2`) {
		t.Fatalf("v2 cursor does not stamp version 2: %s", raw)
	}
	// Downgrade the stamped version to 1: the token must fail closed.
	v1 := base64.RawURLEncoding.EncodeToString([]byte(strings.Replace(string(raw), `"v":2`, `"v":1`, 1)))
	if _, err := Decode(v1, scope, filter); !errors.Is(err, ErrInvalid) {
		t.Fatalf("v1 cursor decoded: %v, want ErrInvalid", err)
	}
	// The v2 token itself round-trips, rank included.
	got, err := Decode(token, scope, filter)
	if err != nil || got != key {
		t.Fatalf("v2 round trip %+v: %v", got, err)
	}
}

func TestCursorRankValidation(t *testing.T) {
	scope := Scope{ID: "central", OwnerType: "central", Revision: 1}
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	search := Filter{Sort: "featured", Search: "ساعت"}
	plain := Filter{Sort: "featured"}
	for _, rank := range []int{0, 1, 2} {
		if _, err := Encode(scope, search, Key{ID: "p-1", CreatedAt: created, Rank: rank}); err != nil {
			t.Fatalf("search rank %d rejected: %v", rank, err)
		}
	}
	for _, rank := range []int{-1, 3} {
		if _, err := Encode(scope, search, Key{ID: "p-1", CreatedAt: created, Rank: rank}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("search rank %d accepted", rank)
		}
	}
	// Non-search keys must carry Rank 0.
	if _, err := Encode(scope, plain, Key{ID: "p-1", CreatedAt: created, Rank: 1}); !errors.Is(err, ErrInvalid) {
		t.Fatal("non-search key with Rank != 0 accepted")
	}
	if _, err := Encode(scope, plain, Key{ID: "p-1", CreatedAt: created}); err != nil {
		t.Fatalf("non-search key with Rank 0 rejected: %v", err)
	}
}

func TestNormalizeSearchForMatch(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"طلا", "طلا"},
		{"  سلام  كتاب ي ", "سلام کتاب ی"},
		{"هندز\u200cفری", "هندزفری"},
		{"هندزفر\u064a", "هندزفری"},
		{"هندز فری", "هندز فری"},
		{"ساعت  هوشمند", "ساعت هوشمند"},
	} {
		if got := NormalizeSearchForMatch(tc.in); got != tc.want {
			t.Errorf("NormalizeSearchForMatch(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	spaced, stripped := SearchMatchForms("هندز فری")
	if spaced != "هندز فری" || stripped != "هندزفری" {
		t.Fatalf("SearchMatchForms = (%q, %q), want (\"هندز فری\", \"هندزفری\")", spaced, stripped)
	}
	spaced, stripped = SearchMatchForms("هندز\u200cفری")
	if spaced != "هندزفری" || stripped != "هندزفری" {
		t.Fatalf("SearchMatchForms ZWNJ = (%q, %q)", spaced, stripped)
	}
}
