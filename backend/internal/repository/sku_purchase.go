package repository

import (
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
