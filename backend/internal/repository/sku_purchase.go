package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ValidateProductSKUSelection keeps ambiguous historical products on their
// explicit legacy path. Typed products always purchase a real, selected SKU.
func ValidateProductSKUSelection(version int, productType, variantID string) error {
	if (version >= 2 || productType == "variable") && strings.TrimSpace(variantID) == "" {
		return fmt.Errorf("a product SKU must be selected")
	}
	return nil
}

// ValidateSKUQuantity is shared by checkout and payment-time revalidation.
// Only explicit legacy inventory retains the historical untracked-zero rule.
func ValidateSKUQuantity(mode string, active bool, stock, minimum int, maximum *int, quantity int) error {
	if !active {
		return fmt.Errorf("selected SKU is inactive")
	}
	if minimum < 1 {
		minimum = 1
	}
	if quantity < minimum {
		return fmt.Errorf("quantity is below the SKU minimum")
	}
	if maximum != nil && quantity > *maximum {
		return fmt.Errorf("quantity exceeds the SKU maximum")
	}
	if (mode == "tracked" || stock > 0) && quantity > stock {
		return ErrInsufficientInventory
	}
	return nil
}

// ResolveLegacyPurchaseVariant resolves the only unambiguous legacy SKU.
func ResolveLegacyPurchaseVariant(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, productID, variantID string) (string, error) {
	if variantID != "" {
		return variantID, nil
	}
	var resolved string
	err := db.QueryRowContext(ctx, `SELECT COALESCE((
		SELECT MIN(v.id::text) FILTER (WHERE v.is_default)
		FROM product_variants v WHERE v.product_id=p.id HAVING COUNT(*)=1
	),'') FROM products p WHERE p.id=$1 AND p.schema_version<2 AND p.product_type<>'variable'`, productID).Scan(&resolved)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return resolved, err
}
