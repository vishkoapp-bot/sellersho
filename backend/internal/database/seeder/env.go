package seeder

import "strings"

// IsDevEnvironment evaluates whether the given environment string matches an explicitly
// allowed development or test environment.
//
// Security Model: Strictly Fail-Closed Allowlist.
// Any empty string, unknown environment, staging, live, or production variant returns false.
func IsDevEnvironment(env string) bool {
	normalized := strings.ToLower(strings.TrimSpace(env))
	switch normalized {
	case "development", "dev", "test", "local":
		return true
	default:
		return false
	}
}
