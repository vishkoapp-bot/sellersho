package routing

import "testing"

// Gap 13: the resolver is the single routing truth shared by order
// creation and the shipping quote. All four toggle combinations are
// pinned against both product flags.
func TestResolveItemRoutingAllCombos(t *testing.T) {
	cases := []struct {
		name    string
		flag    string
		methods map[string]string
		want    string
	}{
		// Both enabled: the product flag decides.
		{"both/direct-flag", "supplier_direct", map[string]string{"direct": "enabled", "warehouse": "enabled"}, "supplier_direct"},
		{"both/warehouse-flag", "warehouse", map[string]string{"direct": "enabled", "warehouse": "enabled"}, "warehouse"},
		// Direct only: direct wins over the product flag (the queue's
		// documented semantics; creation ignoring the toggles was the
		// divergence bug).
		{"direct-only/direct-flag", "supplier_direct", map[string]string{"direct": "enabled", "warehouse": "disabled"}, "supplier_direct"},
		{"direct-only/warehouse-flag", "warehouse", map[string]string{"direct": "enabled", "warehouse": "disabled"}, "supplier_direct"},
		// Warehouse only: warehouse, whatever the flag says.
		{"warehouse-only/direct-flag", "supplier_direct", map[string]string{"direct": "disabled", "warehouse": "enabled"}, "warehouse"},
		{"warehouse-only/warehouse-flag", "warehouse", map[string]string{"direct": "disabled", "warehouse": "enabled"}, "warehouse"},
		// Neither enabled: fail-safe warehouse.
		{"neither/direct-flag", "supplier_direct", map[string]string{"direct": "disabled", "warehouse": "disabled"}, "warehouse"},
		{"neither/warehouse-flag", "warehouse", map[string]string{"direct": "disabled", "warehouse": "disabled"}, "warehouse"},
		// No rows at all (nil/empty map): fail-safe warehouse even for
		// a direct-flagged product — the intended behaviour change for
		// row-less suppliers.
		{"no-rows-nil/direct-flag", "supplier_direct", nil, "warehouse"},
		{"no-rows-nil/warehouse-flag", "warehouse", nil, "warehouse"},
		{"no-rows-empty/direct-flag", "supplier_direct", map[string]string{}, "warehouse"},
		// Non-"enabled" statuses (requested/disabled) never enable.
		{"requested-is-not-enabled", "warehouse", map[string]string{"direct": "requested"}, "warehouse"},
		// Unknown/empty product flag under both-enabled resolves to
		// the warehouse fail-safe, never to direct.
		{"both/empty-flag", "", map[string]string{"direct": "enabled", "warehouse": "enabled"}, "warehouse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveItemRouting(tc.flag, tc.methods); got != tc.want {
				t.Fatalf("ResolveItemRouting(%q, %v) = %q, want %q", tc.flag, tc.methods, got, tc.want)
			}
		})
	}
}
