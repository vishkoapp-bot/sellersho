package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Local definitions occupy negative IDs, separate from the imported registry.
// The approval transaction publishes both the product and its requested facets.
func approveRequestedAttributes(ctx context.Context, tx *sql.Tx, data map[string]interface{}, slug, productID string) error {
	requests, _ := data["requestedAttributes"].(map[string]interface{})
	if len(requests) == 0 {
		return nil
	}
	if len(requests) > 40 {
		return fmt.Errorf("too many requested attributes")
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(20261110, 2)`); err != nil {
		return err
	}
	var categoryID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM categories WHERE slug=$1 OR id=$1 LIMIT 1`, slug).Scan(&categoryID); err != nil {
		return fmt.Errorf("invalid product category: %w", err)
	}
	for name, raw := range requests {
		value, ok := raw.(string)
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if !ok || name == "" || value == "" || len([]rune(name)) > 100 || len([]rune(value)) > 200 {
			return fmt.Errorf("invalid requested attribute")
		}
		var attrID, valueID int
		err := tx.QueryRowContext(ctx, `SELECT dk_attr_id FROM dk_attributes WHERE title=$1 ORDER BY dk_attr_id LIMIT 1`, name).Scan(&attrID)
		if err == sql.ErrNoRows {
			err = tx.QueryRowContext(ctx, `INSERT INTO dk_attributes(dk_attr_id,title,type) SELECT LEAST(COALESCE(MIN(dk_attr_id),0),0)-1,$1,'checkbox' FROM dk_attributes RETURNING dk_attr_id`, name).Scan(&attrID)
		}
		if err != nil {
			return err
		}
		err = tx.QueryRowContext(ctx, `SELECT dk_value_id FROM dk_attribute_values WHERE dk_attr_id=$1 AND title_fa=$2 LIMIT 1`, attrID, value).Scan(&valueID)
		if err == sql.ErrNoRows {
			err = tx.QueryRowContext(ctx, `INSERT INTO dk_attribute_values(dk_attr_id,dk_value_id,title_fa) SELECT $1,LEAST(COALESCE(MIN(dk_value_id),0),0)-1,$2 FROM dk_attribute_values RETURNING dk_value_id`, attrID, value).Scan(&valueID)
		}
		if err != nil {
			return err
		}
		for _, q := range []string{
			`INSERT INTO dk_category_attributes(category_id,dk_attr_id) VALUES($1,$2) ON CONFLICT DO NOTHING`,
		} {
			if _, err = tx.ExecContext(ctx, q, categoryID, attrID); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO dk_category_attribute_values(category_id,dk_attr_id,dk_value_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, categoryID, attrID, valueID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO product_attribute_values(product_id,dk_attribute_id,dk_value_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, productID, attrID, valueID); err != nil {
			return err
		}
	}
	return nil
}
