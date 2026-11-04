package seeder

import (
	"context"
	"fmt"
)

// CleanDev safely purges all development and fixture data without dropping tables.
// Strict Fail-Safe: Will abort immediately if environment is not in the allowed dev list.
func (s *Seeder) CleanDev(ctx context.Context) error {
	if !IsDevEnvironment(s.env) {
		return fmt.Errorf("security violation: CleanDev is strictly restricted to development/test environments (rejected environment: %q)", s.env)
	}

	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	// Truncates development-specific tables while preserving core schema
	// Order handles Foreign Key relationships using CASCADE
	query := `
		TRUNCATE TABLE
			audit_logs,
			pending_approvals,
			ledger_entries,
			disputes,
			order_items,
			orders,
			warehouse_bins,
			shipping_manifests,
			inbound_shipments,
			payout_requests,
			store_niche_overrides,
			store_product_overrides,
			product_niches,
			products,
			stores,
			suppliers,
			users
		CASCADE;
	`
	_, err := s.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to clean development data: %w", err)
	}
	return nil
}

// ResetDev cleans all development data, re-bootstraps core system data, and re-seeds dev fixtures.
// Strict Fail-Safe: Will abort immediately if environment is not in the allowed dev list.
func (s *Seeder) ResetDev(ctx context.Context) error {
	if !IsDevEnvironment(s.env) {
		return fmt.Errorf("security violation: ResetDev is strictly restricted to development/test environments (rejected environment: %q)", s.env)
	}

	if err := s.CleanDev(ctx); err != nil {
		return fmt.Errorf("reset failed during clean stage: %w", err)
	}

	if err := s.SeedBootstrap(ctx); err != nil {
		return fmt.Errorf("reset failed during bootstrap stage: %w", err)
	}

	if err := s.SeedDev(ctx); err != nil {
		return fmt.Errorf("reset failed during dev seed stage: %w", err)
	}

	return nil
}
