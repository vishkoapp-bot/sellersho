package repository

import "github.com/drup/platform/internal/pricing"

func validateAddonMoney(a *SupplierAddon) error {
	if a == nil || a.CapToman < 0 || a.CapToman > pricing.MaxTransportMoney {
		return pricing.ErrInvalidMoney
	}
	if a.PriceMode == "percent" {
		_, err := pricing.ParsePercent(a.PriceValue)
		return err
	}
	if a.PriceMode != "fixed" {
		return pricing.ErrInvalidMoney
	}
	_, err := pricing.MoneyFromNumber(a.PriceValue)
	return err
}
