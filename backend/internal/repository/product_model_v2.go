package repository

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

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
	if !ok || len(raw) == 0 || len(raw) > MaxProductSKUs || kind == "simple" && len(raw) != 1 {
		return fmt.Errorf("invalid SKU count (maximum %d)", MaxProductSKUs)
	}
	seenIDs, seenCombinations := map[string]bool{}, map[string]bool{}
	total, minCost, activeCount := 0., math.MaxFloat64, 0
	for _, entry := range raw {
		v, ok := entry.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid SKU")
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
