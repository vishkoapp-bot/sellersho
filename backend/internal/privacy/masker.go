package privacy

import (
	"fmt"
	"regexp"
	"strings"
)

// CustomerInfo represents sensitive customer PII
type CustomerInfo struct {
	FullName   string `json:"fullName"`
	Phone      string `json:"phone"`
	Province   string `json:"province"`
	City       string `json:"city"`
	Address    string `json:"address"`
	PostalCode string `json:"postalCode"`
	IsMasked   bool   `json:"isMasked"`
}

var nonDigitRegex = regexp.MustCompile(`\D`)

// MaskName masks customer name (e.g. "علیرضا تهرانی" -> "ع. ت***")
func MaskName(fullName string) string {
	trimmed := strings.TrimSpace(fullName)
	if trimmed == "" {
		return "کاربر محترم"
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 1 {
		single := []rune(parts[0])
		if len(single) > 2 {
			return fmt.Sprintf("%c. %c***", single[0], single[1])
		}
		return string(single) + "***"
	}
	first := []rune(parts[0])
	last := []rune(parts[len(parts)-1])
	return fmt.Sprintf("%c. %c***", first[0], last[0])
}

// MaskPhone masks phone number (e.g. "09121234567" -> "0912***567")
func MaskPhone(phone string) string {
	if phone == "" {
		return "***"
	}
	cleaned := nonDigitRegex.ReplaceAllString(phone, "")
	if len(cleaned) >= 10 {
		return fmt.Sprintf("%s***%s", cleaned[:4], cleaned[len(cleaned)-3:])
	}
	return "۰۹***"
}

// MaskAddress masks physical address
func MaskAddress(address, city, province string) string {
	locParts := []string{}
	if province != "" {
		locParts = append(locParts, province)
	}
	if city != "" {
		locParts = append(locParts, city)
	}
	loc := strings.Join(locParts, "، ")
	if loc != "" {
		return fmt.Sprintf("%s (آدرس دقیق و پلاک به دلایل حفظ محرمانگی خریدار نزد پلتفرم محفوظ است)", loc)
	}
	return "نشانی دقیق گیرنده نزد انبار پلتفرم محفوظ است"
}

// MaskPostalCode masks postal code
func MaskPostalCode(postalCode string) string {
	cleaned := nonDigitRegex.ReplaceAllString(postalCode, "")
	if len(cleaned) >= 6 {
		return cleaned[:4] + "******"
	}
	return "******"
}

// MaskCustomerForRole produces a role-scoped CustomerInfo DTO
func MaskCustomerForRole(info CustomerInfo, role string) CustomerInfo {
	// Privileged roles see full unmasked data
	if role == "customer" || role == "warehouse" || role == "supplier" || role == "admin" || role == "logistics_specialist" {
		info.IsMasked = false
		return info
	}

	// Seller, public, and unauthorized roles receive masked PII
	return CustomerInfo{
		FullName:   MaskName(info.FullName),
		Phone:      MaskPhone(info.Phone),
		Province:   info.Province,
		City:       info.City,
		Address:    MaskAddress(info.Address, info.City, info.Province),
		PostalCode: MaskPostalCode(info.PostalCode),
		IsMasked:   true,
	}
}
