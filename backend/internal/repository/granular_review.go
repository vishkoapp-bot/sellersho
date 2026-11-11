package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

var reviewKeys = strings.Fields("title brand noBrand description specs categorySlug categoryAttributes nicheIds barcode warranty imageUrl imageUrls stock supplierBaseCost variants productType fulfillmentMethod supplierWarehouseId weightGrams lengthMm widthMm heightMm packagingLengthMm packagingWidthMm packagingHeightMm preparationDays isOriginal requestedAttributes category supplier_base_cost supplier_warranty preparation_days images image image_url is_original niche_ids length_mm width_mm height_mm weight_grams warehouse suggested_platform_margin")

func pointerEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// Imported legacy payloads may use aliases; never silently prefer an unreviewed
// second representation. New submissions store only the canonical spelling.
func NormalizeReviewAliases(data map[string]interface{}) error {
	aliases := map[string]string{"category": "categorySlug", "supplier_base_cost": "supplierBaseCost", "supplier_warranty": "warranty", "preparation_days": "preparationDays", "images": "imageUrls", "image": "imageUrl", "image_url": "imageUrl", "is_original": "isOriginal", "niche_ids": "nicheIds", "length_mm": "lengthMm", "width_mm": "widthMm", "height_mm": "heightMm", "weight_grams": "weightGrams"}
	for alias, key := range aliases {
		if value, ok := data[alias]; ok {
			if canonical, exists := data[key]; exists && ReviewValueHash(canonical) != ReviewValueHash(value) {
				return fmt.Errorf("conflicting product fields: %s / %s", alias, key)
			}
			data[key] = value
			delete(data, alias)
		}
	}
	return nil
}

// Legacy aliases represent the same input. A corrected spelling synchronizes
// its mirrors; changed mirror hashes automatically return to pending review.
func syncReviewAliasCorrections(data, edits map[string]interface{}) error {
	aliases := map[string]string{"category": "categorySlug", "supplier_base_cost": "supplierBaseCost", "supplier_warranty": "warranty", "preparation_days": "preparationDays", "images": "imageUrls", "image": "imageUrl", "image_url": "imageUrl", "is_original": "isOriginal", "niche_ids": "nicheIds", "length_mm": "lengthMm", "width_mm": "widthMm", "height_mm": "heightMm", "weight_grams": "weightGrams"}
	chosen := map[string]interface{}{}
	for pointer, value := range edits {
		key := strings.TrimPrefix(pointer, "/")
		if strings.Contains(key, "/") {
			continue
		}
		canonical := key
		if mapped, ok := aliases[key]; ok {
			canonical = mapped
		}
		if old, ok := chosen[canonical]; ok && ReviewValueHash(old) != ReviewValueHash(value) {
			return fmt.Errorf("conflicting corrections")
		}
		chosen[canonical] = value
	}
	for alias, key := range aliases {
		if value, ok := chosen[key]; ok {
			data[key] = value
			if _, exists := data[alias]; exists {
				data[alias] = value
			}
		}
	}
	return nil
}
func ReviewLeaves(data map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	var walk func(string, interface{})
	walk = func(path string, v interface{}) {
		switch x := v.(type) {
		case map[string]interface{}:
			if len(x) > 0 {
				for k, c := range x {
					if data["productType"] == "variable" && strings.HasPrefix(path, "/variants/") && len(strings.Split(path, "/")) == 3 && k == "variant_name" {
						continue
					}
					walk(path+"/"+pointerEscape(k), c)
				}
				return
			}
		case []interface{}:
			if len(x) > 0 {
				for i, c := range x {
					walk(path+"/"+strconv.Itoa(i), c)
				}
				return
			}
		}
		out[path] = v
	}
	for _, k := range reviewKeys {
		if (data["productType"] == "variable" || data["productType"] == "simple") && (k == "stock" || k == "supplierBaseCost") {
			continue
		}
		if v, ok := data[k]; ok {
			walk("/"+k, v)
		}
	}
	return out
}
func ReviewValueHash(v interface{}) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func ValidateReviewDecision(decision, reason string) error {
	if decision != "approved" && decision != "rejected" && decision != "pending" {
		return fmt.Errorf("invalid decision")
	}
	if decision == "rejected" && strings.TrimSpace(reason) == "" {
		return fmt.Errorf("rejection reason required")
	}
	if len([]rune(reason)) > 2000 {
		return fmt.Errorf("reason too long")
	}
	return nil
}
func ApplyReviewCorrections(data map[string]interface{}, edits map[string]interface{}, rejected map[string]bool) error {
	leaves := ReviewLeaves(data)
	for key := range edits {
		if _, ok := leaves[key]; !ok || !rejected[key] {
			return fmt.Errorf("field is not editable: %s", key)
		}
		switch leaves[key].(type) {
		case string:
			if _, ok := edits[key].(string); !ok {
				return fmt.Errorf("text required: %s", key)
			}
		case float64:
			if _, ok := edits[key].(float64); !ok {
				return fmt.Errorf("number required: %s", key)
			}
		case bool:
			if _, ok := edits[key].(bool); !ok {
				return fmt.Errorf("boolean required: %s", key)
			}
		case map[string]interface{}:
			if _, ok := edits[key].(map[string]interface{}); !ok {
				return fmt.Errorf("object required: %s", key)
			}
		case []interface{}:
			if _, ok := edits[key].([]interface{}); !ok {
				return fmt.Errorf("list required: %s", key)
			}
		}
	}
	for key, value := range edits {
		parts := strings.Split(strings.TrimPrefix(key, "/"), "/")
		var node interface{} = data
		for i, p := range parts {
			p = strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")
			last := i == len(parts)-1
			switch n := node.(type) {
			case map[string]interface{}:
				if last {
					n[p] = value
				} else {
					node = n[p]
				}
			case []interface{}:
				j, e := strconv.Atoi(p)
				if e != nil || j < 0 || j >= len(n) {
					return fmt.Errorf("invalid array pointer")
				}
				if last {
					n[j] = value
				} else {
					node = n[j]
				}
			default:
				return fmt.Errorf("invalid pointer")
			}
		}
	}
	return nil
}

type FieldDecision struct {
	Key       string `json:"field_key"`
	Decision  string `json:"decision"`
	Reason    string `json:"reason"`
	ValueHash string `json:"value_hash"`
}

func reviewRows(ctx context.Context, tx *sql.Tx, id string) (map[string]FieldDecision, error) {
	rows, e := tx.QueryContext(ctx, `SELECT field_key,decision,COALESCE(reason,''),COALESCE(value_hash,'') FROM proposal_field_reviews WHERE proposal_id=$1`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]FieldDecision{}
	for rows.Next() {
		var f FieldDecision
		if e = rows.Scan(&f.Key, &f.Decision, &f.Reason, &f.ValueHash); e != nil {
			return nil, e
		}
		out[f.Key] = f
	}
	return out, rows.Err()
}
func requireReviewed(ctx context.Context, tx *sql.Tx, id string, data map[string]interface{}) error {
	if len(ReviewLeaves(data)) == 0 {
		return fmt.Errorf("product fields are missing")
	}
	reviews, e := reviewRows(ctx, tx, id)
	if e != nil {
		return e
	}
	for key, value := range ReviewLeaves(data) {
		f := reviews[key]
		if f.Decision != "approved" || f.ValueHash != ReviewValueHash(value) {
			return fmt.Errorf("field requires approval: %s", key)
		}
	}
	return nil
}
func (r *SupplierRepository) GetAdminFieldReview(ctx context.Context, id string) (map[string]interface{}, error) {
	var supplier string
	if e := r.db.QueryRowContext(ctx, `SELECT requested_by FROM pending_approvals WHERE id=$1 AND entity_type='proposed_product'`, id).Scan(&supplier); e != nil {
		return nil, e
	}
	p, f, h, e := r.GetCatalogReviewDetail(ctx, supplier, id)
	if e != nil {
		return nil, e
	}
	leaves := ReviewLeaves(p)
	keys := []string{}
	for k := range leaves {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := []map[string]interface{}{}
	for _, k := range keys {
		decision, reason := "pending", ""
		for _, v := range f {
			if v["field_key"] == k && v["value_hash"] == ReviewValueHash(leaves[k]) {
				decision, _ = v["decision"].(string)
				reason, _ = v["reason"].(string)
			}
		}
		fields = append(fields, map[string]interface{}{"field_key": k, "value": leaves[k], "value_hash": ReviewValueHash(leaves[k]), "decision": decision, "reason": reason})
	}
	return map[string]interface{}{"proposal": p, "fields": fields, "history": h}, nil
}
func (r *SupplierRepository) SaveFieldReview(ctx context.Context, id, actor string, decisions []FieldDecision) error {
	tx, e := r.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var raw []byte
	var status string
	if e = tx.QueryRowContext(ctx, `SELECT requested_data,status FROM pending_approvals WHERE id=$1 AND entity_type='proposed_product' FOR UPDATE`, id).Scan(&raw, &status); e != nil {
		return e
	}
	if status == "approved" {
		return fmt.Errorf("already published")
	}
	data := map[string]interface{}{}
	if e = json.Unmarshal(raw, &data); e != nil {
		return e
	}
	leaves := ReviewLeaves(data)
	if len(decisions) == 0 {
		return fmt.Errorf("no decisions")
	}
	seen := map[string]bool{}
	for _, f := range decisions {
		value, ok := leaves[f.Key]
		if !ok || seen[f.Key] {
			return fmt.Errorf("invalid field")
		}
		seen[f.Key] = true
		if e = ValidateReviewDecision(f.Decision, f.Reason); e != nil {
			return e
		}
		if f.ValueHash != ReviewValueHash(value) {
			return fmt.Errorf("field changed; reload review")
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO proposal_field_reviews(proposal_id,field_key,decision,reason,reviewer_id,value_hash,reviewed_at) VALUES($1,$2,$3,$4,$5,$6,NOW()) ON CONFLICT(proposal_id,field_key) DO UPDATE SET decision=EXCLUDED.decision,reason=EXCLUDED.reason,reviewer_id=EXCLUDED.reviewer_id,value_hash=EXCLUDED.value_hash,reviewed_at=NOW()`, id, f.Key, f.Decision, strings.TrimSpace(f.Reason), actor, f.ValueHash); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO proposal_review_history(proposal_id,action,field_key,reason,actor_id,actor_role) VALUES($1,$2,$3,$4,$5,'admin')`, id, f.Decision, f.Key, f.Reason, actor); e != nil {
			return e
		}
	}
	current, e := reviewRows(ctx, tx, id)
	if e != nil {
		return e
	}
	nextStatus := "pending"
	for key := range leaves {
		if current[key].Decision == "rejected" {
			nextStatus = "rejected"
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE pending_approvals SET status=$2 WHERE id=$1`, id, nextStatus); e != nil {
		return e
	}
	return tx.Commit()
}
func (r *SupplierRepository) ResubmitGranularReview(ctx context.Context, supplier, id string, edits map[string]interface{}) error {
	tx, e := r.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var raw []byte
	var status string
	if e = tx.QueryRowContext(ctx, `SELECT requested_data,status FROM pending_approvals WHERE id=$1 AND requested_by=$2 AND entity_type='proposed_product' FOR UPDATE`, id, supplier).Scan(&raw, &status); e != nil {
		return e
	}
	if status == "approved" {
		return fmt.Errorf("already approved")
	}
	reviews, e := reviewRows(ctx, tx, id)
	if e != nil {
		return e
	}
	rejected := map[string]bool{}
	for k, f := range reviews {
		if f.Decision == "rejected" {
			rejected[k] = true
		}
	}
	data := map[string]interface{}{}
	if e = json.Unmarshal(raw, &data); e != nil {
		return e
	}
	if len(edits) == 0 {
		return fmt.Errorf("no corrections")
	}
	if e = ApplyReviewCorrections(data, edits, rejected); e != nil {
		return e
	}
	if e = syncReviewAliasCorrections(data, edits); e != nil {
		return e
	}
	if e = validateCorrectionValues(data); e != nil {
		return e
	}
	if e = ValidateProductVariationProposal(data); e != nil {
		return e
	}
	merged, _ := json.Marshal(data)
	if _, e = tx.ExecContext(ctx, `UPDATE pending_approvals SET requested_data=$1::jsonb,title=$2,status='pending',rejection_reason=NULL WHERE id=$3`, string(merged), data["title"], id); e != nil {
		return e
	}
	for key, value := range ReviewLeaves(data) {
		_, categoryChanged := edits["/categorySlug"]
		_, typeChanged := edits["/productType"]
		if f, ok := reviews[key]; ok && (categoryChanged || typeChanged || f.ValueHash != ReviewValueHash(value) || edits[key] != nil) {
			if _, e = tx.ExecContext(ctx, `UPDATE proposal_field_reviews SET decision='pending',reason=NULL,value_hash=NULL WHERE proposal_id=$1 AND field_key=$2`, id, key); e != nil {
				return e
			}
		}
	}
	for key := range reviews {
		if _, ok := ReviewLeaves(data)[key]; !ok {
			if _, e = tx.ExecContext(ctx, `DELETE FROM proposal_field_reviews WHERE proposal_id=$1 AND field_key=$2`, id, key); e != nil {
				return e
			}
		}
	}
	for key := range edits {
		if _, e = tx.ExecContext(ctx, `INSERT INTO proposal_review_history(proposal_id,action,field_key,actor_id,actor_role) VALUES($1,'resubmitted',$2,$3,'supplier')`, id, key, supplier); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func validateCorrectionValues(data map[string]interface{}) error {
	for k, v := range ReviewLeaves(data) {
		isSKU := strings.HasPrefix(k, "/variants/") && len(strings.Split(k, "/")) == 4
		switch {
		case k == "/stock", isSKU && (strings.HasSuffix(k, "/stock") || strings.HasSuffix(k, "/supplier_base_cost")), k == "/supplierBaseCost", strings.Count(k, "/") == 1 && (strings.HasSuffix(k, "Grams") || strings.HasSuffix(k, "Mm") || k == "/preparationDays"):
			if v == nil && strings.HasPrefix(k, "/packaging") {
				continue
			}
			n, ok := v.(float64)
			if !ok || n < 0 || n > 1e12 || n != float64(int64(n)) {
				return fmt.Errorf("invalid number: %s", k)
			}
			if n == 0 && k != "/stock" && !(isSKU && strings.HasSuffix(k, "/stock")) {
				return fmt.Errorf("positive number required: %s", k)
			}
		case k == "/title", k == "/description":
			if s, ok := v.(string); !ok || strings.TrimSpace(s) == "" {
				return fmt.Errorf("required text: %s", k)
			}
		}
	}
	return nil
}
