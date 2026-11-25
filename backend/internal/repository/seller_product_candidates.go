package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

type SellerProductCandidate struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	ImageURL string `json:"imageUrl"`
	IsActive bool   `json:"isActive"`
}

// ListSellerProductCandidates reads a page of eligible products outside the
// showcase of a store already checked by OwnedStore. One extra row indicates
// whether there is a next page.
func (r *StoreRepository) ListSellerProductCandidates(ctx context.Context, storeID, search, after string, ids []string, limit int) ([]SellerProductCandidate, bool, error) {
	if limit < 1 || limit > 100 || len(ids) > 100 {
		return nil, false, ErrStoreInput
	}
	args := []any{storeID, limit + 1}
	arg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	query := `SELECT p.id, p.title, COALESCE(p.image_url, ''), p.is_active
		FROM products p WHERE p.id <> '' AND p.supplier_base_cost > 0 AND ` + notSuspendedSupplierClause + `
		AND NOT EXISTS (SELECT 1 FROM store_products sp WHERE sp.store_id = $1 AND sp.product_id = p.id)`
	if after != "" {
		query += ` AND p.id > ` + arg(after)
	}
	if search != "" {
		escape := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
		query += ` AND p.title ILIKE ` + arg("%"+escape.Replace(search)+"%") + ` ESCAPE '\'`
	}
	if len(ids) > 0 {
		query += ` AND p.id = ANY(` + arg(pq.Array(ids)) + `::varchar[])`
	}
	rows, err := r.db.QueryContext(ctx, query+` ORDER BY p.id LIMIT $2`, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	products := make([]SellerProductCandidate, 0, limit+1)
	for rows.Next() {
		var product SellerProductCandidate
		if err := rows.Scan(&product.ID, &product.Title, &product.ImageURL, &product.IsActive); err != nil {
			return nil, false, err
		}
		products = append(products, product)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(products) > limit {
		return products[:limit], true, nil
	}
	return products, false, nil
}
