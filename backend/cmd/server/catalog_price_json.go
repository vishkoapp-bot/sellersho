package main

import "encoding/json"

// MarshalJSON distinguishes an authoritative suspended/nonwinning sale from an
// old server that never resolved sales. Clients intentionally support the latter
// by applying a missing-flag sale themselves. Do not omit false for configured
// sales: doing so could reapply a sale the pricing engine suspended below R.
func (price VariantPriceResponse) MarshalJSON() ([]byte, error) {
	type wirePrice VariantPriceResponse
	if price.SalePrice == nil || price.OnSale {
		return json.Marshal(wirePrice(price))
	}
	return json.Marshal(struct {
		wirePrice
		OnSale bool `json:"onSale"`
	}{wirePrice: wirePrice(price), OnSale: false})
}
