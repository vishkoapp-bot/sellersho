package repository

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func isV2Review(data map[string]interface{}) bool {
	if version, ok := data["schemaVersion"].(float64); ok && version == 2 {
		return true
	}
	if schema, ok := data["productSchema"].(map[string]interface{}); ok {
		return schema["version"] == float64(2)
	}
	return false
}
func reviewV2Leaves(data map[string]interface{}) map[string]interface{} {
	legacy := map[string]interface{}{}
	for k, v := range data {
		if k != "schemaVersion" && k != "productSchema" && k != "variants" && k != "productType" && k != "specs" {
			legacy[k] = v
		}
	}
	legacy["productType"] = "variable"
	out := ReviewLeaves(legacy)
	delete(out, "/productType")
	if schema, ok := data["productSchema"].(map[string]interface{}); ok {
		outline := map[string]interface{}{"version": schema["version"], "type": schema["type"]}
		attrs := []interface{}{}
		if list, ok := schema["attributes"].([]interface{}); ok {
			for _, raw := range list {
				a, ok := raw.(map[string]interface{})
				if !ok {
					continue
				}
				id, _ := a["id"].(string)
				if id == "" {
					continue
				}
				meta := map[string]interface{}{}
				for _, k := range []string{"id", "name", "role", "source", "registryId"} {
					if v, ok := a[k]; ok {
						meta[k] = v
					}
				}
				attrs = append(attrs, meta)
				out["/productSchema/attributes/"+pointerEscape(id)] = a
			}
		}
		sort.Slice(attrs, func(i, j int) bool {
			return fmt.Sprint(attrs[i].(map[string]interface{})["id"]) < fmt.Sprint(attrs[j].(map[string]interface{})["id"])
		})
		outline["attributes"] = attrs
		out["/productSchema"] = outline
	}
	structure := []interface{}{}
	if list, ok := data["variants"].([]interface{}); ok {
		for _, raw := range list {
			v, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			id, _ := v["id"].(string)
			if id == "" {
				continue
			}
			base := "/variants/" + pointerEscape(id)
			for _, k := range []string{"stock", "supplier_base_cost", "min_cart_qty", "max_cart_qty"} {
				if value, ok := v[k]; ok {
					out[base+"/"+k] = value
				}
			}
			selection, _ := v["selection"].(map[string]interface{})
			for key, value := range selection {
				out[base+"/selection/"+pointerEscape(key)] = value
			}
			active := v["is_active"]
			if active == nil {
				active = true
			}
			structure = append(structure, map[string]interface{}{"id": id, "selection": v["selection"], "is_active": active})
		}
	}
	sort.Slice(structure, func(i, j int) bool {
		return fmt.Sprint(structure[i].(map[string]interface{})["id"]) < fmt.Sprint(structure[j].(map[string]interface{})["id"])
	})
	out["/variants"] = structure
	return out
}
func setStableReviewPointer(data map[string]interface{}, key string, value interface{}) error {
	parts := strings.Split(strings.TrimPrefix(key, "/"), "/")
	var current interface{} = data
	for i, part := range parts {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		last := i == len(parts)-1
		switch node := current.(type) {
		case map[string]interface{}:
			if _, exists := node[part]; !exists {
				return fmt.Errorf("unknown field")
			}
			if last {
				node[part] = value
			} else {
				current = node[part]
			}
		case []interface{}:
			index := -1
			for j, entry := range node {
				if m, ok := entry.(map[string]interface{}); ok && m["id"] == part {
					index = j
					break
				}
			}
			if index < 0 {
				if n, e := strconv.Atoi(part); e == nil && n >= 0 && n < len(node) {
					index = n
				}
			}
			if index < 0 {
				return fmt.Errorf("unknown stable item")
			}
			if last {
				node[index] = value
			} else {
				current = node[index]
			}
		default:
			return fmt.Errorf("invalid correction pointer")
		}
	}
	return nil
}
func applyV2Corrections(data map[string]interface{}, edits map[string]interface{}, rejected map[string]bool) error {
	before := ReviewLeaves(data)
	for key, value := range edits {
		old, exists := before[key]
		if !exists || !rejected[key] {
			return fmt.Errorf("field is not editable: %s", key)
		}
		switch old.(type) {
		case float64:
			if value == nil && strings.HasSuffix(key, "/max_cart_qty") {
				break
			}
			if _, ok := value.(float64); !ok {
				return fmt.Errorf("number required")
			}
		case string:
			if _, ok := value.(string); !ok {
				return fmt.Errorf("text required")
			}
		case bool:
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("boolean required")
			}
		case map[string]interface{}:
			if _, ok := value.(map[string]interface{}); !ok {
				return fmt.Errorf("object required")
			}
		case []interface{}:
			if _, ok := value.([]interface{}); !ok {
				return fmt.Errorf("list required")
			}
		}
	}
	// Edit a detached copy so a failed structural validation cannot alter input.
	clone := cloneReviewDocument(data)
	keys := []string{}
	for k := range edits {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) < len(keys[j]) })
	for _, key := range keys {
		if key != "/productSchema" && strings.HasPrefix(key, "/productSchema/attributes/") {
			a, _ := edits[key].(map[string]interface{})
			id := strings.ReplaceAll(strings.ReplaceAll(strings.TrimPrefix(key, "/productSchema/attributes/"), "~1", "/"), "~0", "~")
			if a["id"] != id && !rejected["/productSchema"] {
				return fmt.Errorf("attribute identity needs structural review")
			}
		}
		if e := setStableReviewPointer(clone, key, edits[key]); e != nil {
			return e
		}
	}
	if schema, ok := clone["productSchema"].(map[string]interface{}); ok {
		clone["productType"] = schema["type"]
	}
	after := ReviewLeaves(clone)
	for key, old := range before {
		value, exists := after[key]
		if !exists {
			if strings.HasPrefix(key, "/variants/") && rejected["/variants"] {
				continue
			}
			if strings.HasPrefix(key, "/productSchema/attributes/") && rejected["/productSchema"] {
				continue
			}
			return fmt.Errorf("removing unreviewed field: %s", key)
		}
		if ReviewValueHash(value) == ReviewValueHash(old) || rejected[key] {
			continue
		}
		if key == "/variants" || key == "/productSchema" {
			continue
		}
		if strings.Contains(key, "/selection/") && rejected["/variants"] {
			continue
		}
		if strings.HasPrefix(key, "/productSchema/attributes/") && rejected["/productSchema"] {
			continue
		}
		return fmt.Errorf("approved field changed by structure: %s", key)
	}
	for key := range data {
		delete(data, key)
	}
	for key, value := range clone {
		data[key] = value
	}
	return nil
}

func cloneReviewDocument(data map[string]interface{}) map[string]interface{} {
	b, _ := json.Marshal(data)
	out := map[string]interface{}{}
	_ = json.Unmarshal(b, &out)
	return out
}
