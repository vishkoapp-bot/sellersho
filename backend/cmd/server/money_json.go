package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/drup/platform/internal/pricing"
)

// decodeMoneyJSON validates exact numeric tokens before unmarshalling existing
// float transport structs. It is used only on pricing/settings write endpoints.
func decodeMoneyJSON(reader io.Reader, target any, catalogFieldsOnly ...bool) error {
	fieldsOnly := len(catalogFieldsOnly) > 0 && catalogFieldsOnly[0]
	raw, err := io.ReadAll(io.LimitReader(reader, 1<<20))
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var input any
	if input, err = readMoneyJSONValue(dec, 0); err != nil {
		return err
	}
	if err = validateMoneyJSONValue(input, fieldsOnly); err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

// encoding/json matches tagged struct fields case-insensitively. Normalize the
// same recognized financial names for validation, while the original bytes are
// still unmarshalled into the caller's target. EqualFold also matches Go's
// Unicode simple-fold aliases. Reject duplicates before maps can erase a token.
func canonicalMoneyJSONKey(key string) string {
	for _, known := range []string{"platformPct", "centralProfitPct", "increaseCapPct", "maxProfitMargin", "minProfitMargin", "defaultMargin", "earlyWithdrawalFeePct", "referralCommissionPercent", "pct", "margin", "marginPercent", "maxMarginPct", "exceptionCapPct", "value", "overrideValue", "priceValue", "markupValue", "mode", "priceMode", "markupType", "discounts", "productPriceOverrides", "nichePriceOverrides", "categoryPriceOverrides", "salePrice", "supplierBaseCost", "supplier_base_cost", "new_supplier_base_cost", "newReference", "originalPrice", "amount", "walletMinWithdrawal", "capToman"} {
		if strings.EqualFold(key, known) {
			return known
		}
	}
	return key
}

func readMoneyJSONValue(dec *json.Decoder, depth int) (any, error) {
	if depth > 128 {
		return nil, pricing.ErrInvalidMoney
	}
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delim {
	case '{':
		obj := map[string]any{}
		for dec.More() {
			token, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := token.(string)
			if !ok {
				return nil, pricing.ErrInvalidMoney
			}
			key = canonicalMoneyJSONKey(key)
			if _, exists := obj[key]; exists {
				return nil, pricing.ErrInvalidMoney
			}
			value, err := readMoneyJSONValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			obj[key] = value
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return obj, nil
	case '[':
		list := []any{}
		for dec.More() {
			value, err := readMoneyJSONValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			list = append(list, value)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		return list, nil
	default:
		return nil, pricing.ErrInvalidMoney
	}
}

func validateMoneyJSONValue(value any, fieldsOnly bool) error {
	obj, ok := value.(map[string]any)
	if !ok {
		if list, ok := value.([]any); ok {
			for _, item := range list {
				if err := validateMoneyJSONValue(item, fieldsOnly); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for key, item := range obj {
		if item == nil {
			continue
		}
		switch key {
		case "platformPct", "centralProfitPct", "increaseCapPct", "maxProfitMargin", "minProfitMargin", "defaultMargin", "earlyWithdrawalFeePct", "referralCommissionPercent", "pct", "margin", "marginPercent", "maxMarginPct", "exceptionCapPct":
			if n, ok := item.(json.Number); ok {
				if _, err := pricing.ParsePercentDecimal(n.String()); err != nil {
					return err
				}
			}
		case "value", "overrideValue", "priceValue", "markupValue":
			if fieldsOnly && key != "priceValue" && key != "markupValue" {
				break
			}
			if n, ok := item.(json.Number); ok {
				mode, _ := obj["mode"].(string)
				if key == "priceValue" {
					mode, _ = obj["priceMode"].(string)
				}
				if key == "markupValue" {
					mode, _ = obj["markupType"].(string)
				}
				_, percentErr := pricing.ParsePercentDecimal(n.String())
				_, moneyErr := pricing.MoneyFromDecimal(n.String())
				if (mode == "fixed" && moneyErr != nil) || (mode == "percent" && percentErr != nil) || (mode != "fixed" && mode != "percent" && percentErr != nil && moneyErr != nil) {
					return pricing.ErrInvalidMoney
				}
			}
		case "discounts", "productPriceOverrides", "nichePriceOverrides", "categoryPriceOverrides":
			if entries, ok := item.(map[string]any); ok {
				for _, entry := range entries {
					if n, ok := entry.(json.Number); ok {
						if _, err := pricing.ParsePercentDecimal(n.String()); err != nil {
							return err
						}
					}
				}
			}
		case "salePrice", "supplierBaseCost", "supplier_base_cost", "new_supplier_base_cost", "newReference", "originalPrice", "amount", "walletMinWithdrawal", "capToman":
			if n, ok := item.(json.Number); ok {
				if _, err := pricing.MoneyFromDecimal(n.String()); err != nil {
					return err
				}
			}
		}
		if err := validateMoneyJSONValue(item, fieldsOnly); err != nil {
			return err
		}
	}
	return nil
}
