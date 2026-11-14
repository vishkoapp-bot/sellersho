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
		strings.Replace(string(raw), `"v":1`, `"v":1,"V":1`, 1),
		strings.Replace(string(raw), `"price":10`, `"price":10,"Price":11`, 1),
	} {
		if _, err := Decode(base64.RawURLEncoding.EncodeToString([]byte(mutated)), scope, filter); !errors.Is(err, ErrInvalid) {
			t.Fatalf("case-alias duplicate accepted: %v", err)
		}
	}
}
