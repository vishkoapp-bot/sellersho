package repository

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
)

const MaxProductSKUs = 500

func IsProductV2(data map[string]interface{}) bool { return jsonInteger(data["schemaVersion"], 2, 2) }

func jsonInteger(value interface{}, min, max float64) bool {
	n, ok := jsonNumber(value)
	return ok && !math.IsNaN(n) && !math.IsInf(n, 0) && n >= min && n <= max && n == math.Trunc(n)
}

func jsonNumber(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}

// ValidateVariantPhysicalValues is the ONE physical rule, shared by
// validateProductV2 (propose path) and the PUT .../physical handler.
// weight nil = inherit; dims nil = inherit. Dims are all-or-none:
// a stored triple is either complete or entirely absent.
func ValidateVariantPhysicalValues(weight, length, width, height *int) error {
	if weight != nil && (*weight < 1 || *weight > 1000000000) {
		return fmt.Errorf("invalid SKU weight")
	}
	for _, d := range []*int{length, width, height} {
		if d != nil && (*d < 1 || *d > 1000000) {
			return fmt.Errorf("invalid SKU dimensions")
		}
	}
	set := 0
	for _, d := range []*int{length, width, height} {
		if d != nil {
			set++
		}
	}
	if set != 0 && set != 3 {
		return fmt.Errorf("incomplete SKU dimensions")
	}
	return nil
}

// variantPhysicalInt extracts one physical field from a variant map,
// accepting both the camelCase (weightGrams) and snake_case
// (weight_grams) spellings. Returns (value, present, error): present is
// false when the key is absent or JSON null; error when the value is
// present but not a JSON integer in range.
func variantPhysicalInt(v map[string]interface{}, camel, snake string, max float64) (*int, bool, error) {
	raw, ok := v[camel]
	if !ok {
		raw, ok = v[snake]
	}
	if !ok || raw == nil {
		return nil, false, nil
	}
	if !jsonInteger(raw, 1, max) {
		return nil, true, fmt.Errorf("invalid SKU value")
	}
	n, _ := jsonNumber(raw)
	i := int(n)
	return &i, true, nil
}

// validateVariantSale validates one variant map's optional P1(c)
// sale fields (rules 1/4/5 of the spec: S > 0 and a multiple of 100,
// saleFrom < saleTo, saleTo in the future). Rules 2/3 (S > R and
// S < P_central) need the pricing resolver and are enforced by
// validateVariantSaleFloorsTx inside ValidateProductProposalTx
// (propose / correction / approval-publication re-validation /
// bulk-add — M1, 2026-11-13) and by the live sale endpoint, not
// here. Normalises onto canonical snake_case keys (sale_price
// float64, sale_from / sale_to RFC3339 strings, nil = absent).
func validateVariantSale(v map[string]interface{}) error {
	raw, ok := v["sale_price"]
	if !ok {
		raw, ok = v["salePrice"]
	}
	var price *float64
	if ok && raw != nil {
		if !jsonInteger(raw, 1, 1e12) {
			return fmt.Errorf("invalid SKU sale price")
		}
		n, _ := jsonNumber(raw)
		if int64(n)%100 != 0 {
			return fmt.Errorf("invalid SKU sale price")
		}
		price = &n
	}
	var from, to *time.Time
	for _, f := range []struct {
		camel, snake string
		dst          **time.Time
	}{
		{"saleFrom", "sale_from", &from},
		{"saleTo", "sale_to", &to},
	} {
		raw, ok := v[f.camel]
		if !ok {
			raw, ok = v[f.snake]
		}
		if !ok || raw == nil {
			continue
		}
		s, ok := raw.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return fmt.Errorf("invalid SKU sale window")
		}
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("invalid SKU sale window")
		}
		*f.dst = &parsed
	}
	if price == nil && (from != nil || to != nil) {
		return fmt.Errorf("invalid SKU sale window")
	}
	if from != nil && to != nil && !from.Before(*to) {
		return fmt.Errorf("invalid SKU sale window")
	}
	// An EXPIRED sale (saleTo in the past) is normalised away, not
	// rejected: validateProductV2 re-validates stored payloads at
	// approval time and bulk-add time, so a time-sensitive rejection
	// would block an unrelated approval / variant add for a sale that
	// has simply ended (it is inert everywhere). The live write path
	// (PUT .../variants/{vid}/sale) enforces the future-saleTo rule
	// strictly instead. Consequence at propose time: a past saleTo is
	// dropped, not rejected.
	if to != nil && !to.After(time.Now()) {
		v["sale_price"] = nil
		v["sale_from"] = nil
		v["sale_to"] = nil
		return nil
	}
	if price != nil {
		v["sale_price"] = *price
	} else {
		v["sale_price"] = nil
	}
	if from != nil {
		v["sale_from"] = from.Format(time.RFC3339)
	} else {
		v["sale_from"] = nil
	}
	if to != nil {
		v["sale_to"] = to.Format(time.RFC3339)
	} else {
		v["sale_to"] = nil
	}
	return nil
}

// validateVariantClasses validates one variant map's optional P2(b)
// class fields (must be in-enum when present) and normalises them
// onto canonical snake_case string keys (nil = inherit).
func validateVariantClasses(v map[string]interface{}) error {
	for _, f := range []struct {
		camel, snake string
		valid        func(string) bool
		err          string
	}{
		{"shippingClass", "shipping_class", domain.ValidShippingClass, "invalid SKU shipping class"},
		{"taxClass", "tax_class", domain.ValidTaxClass, "invalid SKU tax class"},
	} {
		raw, ok := v[f.camel]
		if !ok {
			raw, ok = v[f.snake]
		}
		if !ok || raw == nil {
			v[f.snake] = nil
			continue
		}
		s, ok := raw.(string)
		if !ok || !f.valid(s) {
			return fmt.Errorf("%s", f.err)
		}
		v[f.snake] = s
	}
	return nil
}

// validateProductClasses validates the optional product-level P2(b)
// class pair and normalises it onto canonical snake_case string keys.
func validateProductClasses(data map[string]interface{}) error {
	for _, f := range []struct {
		camel, snake string
		valid        func(string) bool
		err          string
	}{
		{"shippingClass", "shipping_class", domain.ValidShippingClass, "invalid shipping class"},
		{"taxClass", "tax_class", domain.ValidTaxClass, "invalid tax class"},
	} {
		raw, ok := data[f.camel]
		if !ok {
			raw, ok = data[f.snake]
		}
		if !ok || raw == nil {
			continue
		}
		s, ok := raw.(string)
		if !ok || !f.valid(s) {
			return fmt.Errorf("%s", f.err)
		}
		data[f.snake] = s
	}
	return nil
}

// + image fields (V-W-*, V-DIM-*, V-IMG-*), then normalises the values
// onto canonical snake_case float64 keys so the publication insert
// reads them consistently.
func validateVariantPhysical(v map[string]interface{}) error {
	weight, _, err := variantPhysicalInt(v, "weightGrams", "weight_grams", 1e9)
	if err != nil {
		return fmt.Errorf("invalid SKU weight")
	}
	length, _, err := variantPhysicalInt(v, "lengthMm", "length_mm", 1e6)
	if err != nil {
		return fmt.Errorf("invalid SKU dimensions")
	}
	width, _, err := variantPhysicalInt(v, "widthMm", "width_mm", 1e6)
	if err != nil {
		return fmt.Errorf("invalid SKU dimensions")
	}
	height, _, err := variantPhysicalInt(v, "heightMm", "height_mm", 1e6)
	if err != nil {
		return fmt.Errorf("invalid SKU dimensions")
	}
	if err := ValidateVariantPhysicalValues(weight, length, width, height); err != nil {
		return err
	}
	// Normalise: canonical snake_case keys, float64 like the existing
	// supplier_base_cost / stock normalisation; nil = inherit.
	var wf, lf, wf2, hf interface{}
	if weight != nil {
		wf = float64(*weight)
	}
	if length != nil {
		lf = float64(*length)
	}
	if width != nil {
		wf2 = float64(*width)
	}
	if height != nil {
		hf = float64(*height)
	}
	v["weight_grams"], v["length_mm"], v["width_mm"], v["height_mm"] = wf, lf, wf2, hf
	if raw, ok := v["images"]; ok && raw != nil {
		var arr []string
		switch t := raw.(type) {
		case []interface{}:
			for _, e := range t {
				str, ok := e.(string)
				if !ok {
					return fmt.Errorf("invalid SKU images")
				}
				arr = append(arr, str)
			}
		case []string:
			arr = t
		default:
			return fmt.Errorf("invalid SKU images")
		}
		if len(arr) > 4 {
			return fmt.Errorf("invalid SKU images")
		}
		for _, str := range arr {
			if strings.TrimSpace(str) == "" || !strings.HasPrefix(str, "/static/uploads/") {
				return fmt.Errorf("invalid SKU images")
			}
		}
	}
	return nil
}

func productIdentityText(s string) string {
	s = strings.NewReplacer("ي", "ی", "ك", "ک", "\u200c", " ").Replace(s)
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func uuidBackedID(id string) bool {
	if uuid.Validate(id) == nil {
		return true
	}
	i := strings.LastIndex(id, ":")
	return i >= 0 && uuid.Validate(id[i+1:]) == nil
}

func requestedOptionID(id string) bool {
	return strings.HasPrefix(id, "requestedv:") && uuid.Validate(strings.TrimPrefix(id, "requestedv:")) == nil
}

func signedRegistryID(raw, prefix string) (int64, error) {
	if !strings.HasPrefix(raw, prefix) {
		return 0, fmt.Errorf("invalid registry identity: %s", raw)
	}
	n, e := strconv.ParseInt(strings.TrimPrefix(raw, prefix), 10, 32)
	if e != nil || n == 0 || raw != prefix+strconv.FormatInt(n, 10) {
		return 0, fmt.Errorf("invalid registry identity: %s", raw)
	}
	return n, nil
}

func productSchemaFromData(data map[string]interface{}) (*domain.ProductSchema, error) {
	if !IsProductV2(data) {
		return nil, fmt.Errorf("unsupported product schema version")
	}
	raw, ok := data["productSchema"]
	if !ok || raw == nil {
		return nil, fmt.Errorf("product schema is required")
	}
	b, e := json.Marshal(raw)
	if e != nil {
		return nil, e
	}
	var schema domain.ProductSchema
	if e = json.Unmarshal(b, &schema); e != nil {
		return nil, fmt.Errorf("invalid product schema: %w", e)
	}
	return &schema, nil
}

// ProductCombinationKey is structural JSON, so arbitrary labels and separators
// cannot collide. IDs retain their exact registry identity.
func ProductCombinationKey(selection map[string]string) string {
	keys := make([]string, 0, len(selection))
	for k := range selection {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([][2]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, [2]string{k, selection[k]})
	}
	b, _ := json.Marshal(pairs)
	return string(b)
}

func isReservedDefaultVariantName(name string) bool {
	return strings.Map(func(r rune) rune {
		if r == '\u200c' || r == '\u200d' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, name) == "پیشفرض"
}

func validateProductV2(data map[string]interface{}) error {
	schema, e := productSchemaFromData(data)
	if e != nil {
		return e
	}
	kind, _ := data["productType"].(string)
	if (kind != "simple" && kind != "variable") || schema.Version != 2 || schema.Type != kind {
		return fmt.Errorf("product schema/type mismatch")
	}
	if len(schema.Attributes) > 100 {
		return fmt.Errorf("too many product attributes")
	}
	// V2 has a single attribute definition surface. Legacy mirrors would permit
	// a reviewed static color to contradict the SKU axes.
	if ca, ok := data["categoryAttributes"].(map[string]interface{}); ok {
		for k, v := range ca {
			if k == "brandId" {
				continue
			}
			switch x := v.(type) {
			case nil:
				continue
			case map[string]interface{}:
				if len(x) == 0 {
					continue
				}
			case []interface{}:
				if len(x) == 0 {
					continue
				}
			}
			return fmt.Errorf("V2 attributes must use productSchema")
		}
	}
	if req, ok := data["requestedAttributes"].(map[string]interface{}); ok && len(req) > 0 {
		return fmt.Errorf("V2 requests must use productSchema")
	}
	byID := map[string]*domain.ProductAttributeDefinition{}
	names := map[string]bool{}
	axes := []*domain.ProductAttributeDefinition{}
	for i := range schema.Attributes {
		a := &schema.Attributes[i]
		a.Name = strings.TrimSpace(a.Name)
		name := productIdentityText(a.Name)
		if a.ID == "" || a.ID != strings.TrimSpace(a.ID) || a.Name == "" || len([]rune(a.Name)) > 100 || names[name] || byID[a.ID] != nil {
			return fmt.Errorf("duplicate or invalid product attribute")
		}
		names[name] = true
		byID[a.ID] = a
		if a.Role != "fixed" && a.Role != "axis" {
			return fmt.Errorf("attribute role must be fixed or axis")
		}
		if len(a.Values) == 0 || len(a.Values) > 500 || (a.Role == "fixed" && len(a.Values) != 1) {
			return fmt.Errorf("invalid attribute value count")
		}
		if a.Role == "axis" {
			axes = append(axes, a)
		}
		var registryID int64
		switch a.Source {
		case "color":
			if a.ID != "color" {
				return fmt.Errorf("invalid color attribute identity")
			}
		case "dk":
			registryID, e = signedRegistryID(a.ID, "dk:")
			if e != nil {
				return e
			}
			if a.RegistryID == nil || *a.RegistryID != registryID {
				return fmt.Errorf("attribute registry identity mismatch")
			}
		case "local", "requested":
			if !uuidBackedID(a.ID) || a.RegistryID != nil {
				return fmt.Errorf("local attributes require stable UUID identities")
			}
		default:
			return fmt.Errorf("invalid attribute source")
		}
		valueIDs, valueLabels := map[string]bool{}, map[string]bool{}
		for j := range a.Values {
			v := &a.Values[j]
			v.Label = strings.TrimSpace(v.Label)
			label := productIdentityText(v.Label)
			if v.ID == "" || v.ID != strings.TrimSpace(v.ID) || v.Label == "" || len([]rune(v.Label)) > 200 || valueIDs[v.ID] || valueLabels[label] {
				return fmt.Errorf("duplicate or invalid attribute option")
			}
			valueIDs[v.ID] = true
			valueLabels[label] = true
			if !requestedOptionID(v.ID) {
				switch a.Source {
				case "color":
					n, err := signedRegistryID(v.ID, "color:")
					if err != nil || n < 1 {
						return fmt.Errorf("invalid color option")
					}
				case "dk":
					if _, err := signedRegistryID(v.ID, "dkv:"+strconv.FormatInt(registryID, 10)+":"); err != nil {
						return err
					}
				case "local", "requested":
					if !uuidBackedID(v.ID) {
						return fmt.Errorf("local options require stable UUID identities")
					}
				}
			}
		}
	}
	if kind == "simple" && len(axes) != 0 || kind == "variable" && len(axes) == 0 {
		return fmt.Errorf("product type does not match its axes")
	}
	raw, ok := data["variants"].([]interface{})
	// P1(b): subsets are legal — there is NO completeness requirement
	// against the cartesian product. The count check below measures
	// only the SUBMITTED (selected) rows; uniqueness is enforced by
	// seenCombinations + the partial unique index.
	if !ok || len(raw) == 0 || len(raw) > MaxProductSKUs || kind == "simple" && len(raw) != 1 {
		return fmt.Errorf("invalid SKU count (maximum %d)", MaxProductSKUs)
	}
	seenIDs, seenCombinations := map[string]bool{}, map[string]bool{}
	defaultCount := 0
	total, minCost, activeCount := 0., math.MaxFloat64, 0
	for _, entry := range raw {
		v, ok := entry.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid SKU")
		}
		name, _ := v["variant_name"].(string)
		if kind == "variable" && isReservedDefaultVariantName(name) {
			return fmt.Errorf("invalid SKU name %q for a variable product", name)
		}
		id, _ := v["id"].(string)
		if uuid.Validate(id) != nil || id != strings.ToLower(id) || seenIDs[id] {
			return fmt.Errorf("SKU requires a unique canonical UUID")
		}
		seenIDs[id] = true
		active, ok := v["is_active"].(bool)
		if !ok {
			return fmt.Errorf("SKU active state required")
		}
		// B2 (2026-11-13): real product-default assignment (OQ-1).
		// The proposal may name at most one variant as the product
		// default (is_default, camelCase isDefault also accepted);
		// publication honors it for variable products and falls back
		// to the first active submitted variant when none is named.
		// For `simple` there is exactly one SKU and it is always the
		// default. The flag is normalised onto canonical `is_default`
		// so publication and the post-publication add path read one
		// key. An explicitly flagged variant is honored even when
		// inactive (the supplier chose it; the PDP/storefront
		// fallback chain falls through to the first purchasable
		// variant when the default is not purchasable).
		isDefault := false
		if raw, present := v["is_default"]; present && raw != nil {
			flag, ok := raw.(bool)
			if !ok {
				return fmt.Errorf("invalid SKU default flag")
			}
			isDefault = flag
		} else if raw, present := v["isDefault"]; present && raw != nil {
			flag, ok := raw.(bool)
			if !ok {
				return fmt.Errorf("invalid SKU default flag")
			}
			isDefault = flag
		}
		v["is_default"] = isDefault
		if isDefault {
			defaultCount++
			if defaultCount > 1 {
				return fmt.Errorf("at most one default SKU per product")
			}
		}
		selectionRaw, ok := v["selection"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("SKU selection required")
		}
		if len(selectionRaw) != len(axes) {
			return fmt.Errorf("SKU must select exactly every axis")
		}
		selection := map[string]string{}
		snapshots := map[string]interface{}{}
		display := []string{}
		for _, a := range axes {
			selected, ok := selectionRaw[a.ID].(string)
			if !ok {
				return fmt.Errorf("SKU is missing an axis")
			}
			found := false
			for _, value := range a.Values {
				if value.ID == selected {
					found = true
					snapshots[a.Name] = value.Label
					display = append(display, a.Name+": "+value.Label)
					break
				}
			}
			if !found {
				return fmt.Errorf("SKU option is not declared in product schema")
			}
			selection[a.ID] = selected
		}
		key := ProductCombinationKey(selection)
		if seenCombinations[key] {
			return fmt.Errorf("duplicate SKU combination")
		}
		seenCombinations[key] = true
		if !active && (v["supplier_base_cost"] == nil || v["supplier_base_cost"] == "") {
			v["supplier_base_cost"] = float64(0)
		}
		minPrice := 0.
		if active {
			minPrice = 1
		}
		if !jsonInteger(v["supplier_base_cost"], minPrice, 1e12) || !jsonInteger(v["stock"], 0, 1e9) {
			return fmt.Errorf("invalid SKU price or stock")
		}
		v["supplier_base_cost"], _ = jsonNumber(v["supplier_base_cost"])
		v["stock"], _ = jsonNumber(v["stock"])
		if n, exists := v["min_cart_qty"]; exists && n != nil && !jsonInteger(n, 1, 1e9) {
			return fmt.Errorf("invalid minimum cart quantity")
		}
		if n, exists := v["min_cart_qty"]; exists && n != nil {
			v["min_cart_qty"], _ = jsonNumber(n)
		} else {
			v["min_cart_qty"] = float64(1)
		}
		if n, exists := v["max_cart_qty"]; exists && n != nil {
			if !jsonInteger(n, 1, 1e9) {
				return fmt.Errorf("invalid maximum cart quantity")
			}
			min := 1.
			if n, ok := v["min_cart_qty"].(float64); ok {
				min = n
			}
			number, _ := jsonNumber(n)
			if number < min {
				return fmt.Errorf("maximum cart quantity below minimum")
			}
			v["max_cart_qty"] = number
		} else {
			v["max_cart_qty"] = nil
		}
		if err := validateVariantPhysical(v); err != nil {
			return err
		}
		// P1(c): optional per-variant sale (rules 1/4/5 here; the
		// R/P_central floor/ceiling need the resolver and are
		// enforced by validateVariantSaleFloorsTx in
		// ValidateProductProposalTx + the live sale endpoint).
		// P2(b): optional per-variant classes.
		if err := validateVariantSale(v); err != nil {
			return err
		}
		if err := validateVariantClasses(v); err != nil {
			return err
		}
		if active {
			activeCount++
			total += v["stock"].(float64)
			minCost = math.Min(minCost, v["supplier_base_cost"].(float64))
		}
		v["combination_key"] = key
		v["attributes"] = snapshots
		v["inventory_mode"] = "tracked"
		v["variant_name"] = strings.Join(display, " / ")
		if kind == "simple" {
			v["variant_name"] = "پیش‌فرض"
		}
	}
	if activeCount == 0 {
		return fmt.Errorf("at least one active SKU required")
	}
	// P2(b): optional product-level shipping/tax class pair (the
	// fallback target for variants); absent = 'standard' at publish.
	if err := validateProductClasses(data); err != nil {
		return err
	}
	if total > math.MaxInt32 {
		return fmt.Errorf("aggregate stock exceeds supported product quantity")
	}
	data["stock"], data["supplierBaseCost"] = total, minCost
	// Compatibility specs are a projection, never a second source of fixed
	// attributes. Structural corrections update this mirror automatically.
	specs := map[string]interface{}{}
	for _, a := range schema.Attributes {
		if a.Role == "fixed" {
			specs[a.Name] = a.Values[0].Label
		}
	}
	data["specs"] = specs
	// Retain JSON-shaped maps for the review system, not Go struct values.
	b, _ := json.Marshal(schema)
	var canonical map[string]interface{}
	_ = json.Unmarshal(b, &canonical)
	data["productSchema"] = canonical
	return nil
}
