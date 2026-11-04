package phone

import "strings"

func Normalize(value string) string {
	s := strings.TrimSpace(value)
	s = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", "+", "").Replace(s)
	if strings.HasPrefix(s, "98") {
		s = "0" + s[2:]
	}
	if strings.HasPrefix(s, "9") && len(s) == 10 {
		s = "0" + s
	}
	return s
}

func IsValid(value string) bool {
	s := Normalize(value)
	if len(s) != 11 || s[0] != '0' || s[1] != '9' {
		return false
	}
	for _, r := range s[2:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
