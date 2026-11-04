package accesspolicy

import "github.com/drup/platform/internal/phone"

const SuperAdminCapability = "super_admin"

// This backend-owned allowlist is intentionally not configurable from the
// request or from a permissive environment variable.
var superAdminMobiles = map[string]struct{}{
	"09933070870": {},
	"09127504005": {},
}

func IsSuperAdminMobile(mobile string) bool {
	_, ok := superAdminMobiles[phone.Normalize(mobile)]
	return ok
}

func SuperAdminMobiles() []string {
	return []string{"09933070870", "09127504005"}
}
