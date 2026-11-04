package main

import (
	"net/http"
	"testing"
)

func TestHealthStatus(t *testing.T) {
	cases := []struct {
		name       string
		postgresUp bool
		redisUp    bool
		wantStatus string
		wantCode   int
	}{
		{"both up is ok 200", true, true, "ok", http.StatusOK},
		{"postgres down is degraded 503", false, true, "degraded", http.StatusServiceUnavailable},
		{"redis down is degraded 503", true, false, "degraded", http.StatusServiceUnavailable},
		{"both down is degraded 503", false, false, "degraded", http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, code := healthStatus(tc.postgresUp, tc.redisUp)
			if status != tc.wantStatus || code != tc.wantCode {
				t.Fatalf("healthStatus(%v, %v) = (%q, %d), want (%q, %d)",
					tc.postgresUp, tc.redisUp, status, code, tc.wantStatus, tc.wantCode)
			}
			if tc.wantStatus == "ok" && code != http.StatusOK {
				t.Fatal("healthy case must stay HTTP 200")
			}
		})
	}
}
