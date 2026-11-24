package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// The published §5.8 global queries are copied from the backend spec.
const (
	reconciliationV0aSQL = `WITH doubled AS (
  SELECT p.id
  FROM products p
  JOIN product_variants d ON d.product_id = p.id AND d.is_default
  WHERE d.stock = p.stock
    AND (SELECT COALESCE(SUM(v.stock), 0) FROM product_variants v
         WHERE v.product_id = p.id AND NOT v.is_default) = p.stock
)
SELECT p.id, p.title
FROM products p
JOIN doubled db ON db.id = p.id
WHERE EXISTS (SELECT 1 FROM order_items oi
              JOIN product_variants d ON d.id = oi.variant_id
              WHERE d.product_id = p.id AND d.is_default)
   OR (EXISTS (SELECT 1 FROM order_items oi
               JOIN product_variants v ON v.id = oi.variant_id
               WHERE v.product_id = p.id)
       AND NOT EXISTS (SELECT 1 FROM order_items oi
                       JOIN product_variants v ON v.id = oi.variant_id
                       WHERE v.product_id = p.id AND NOT v.is_default));`

	reconciliationV1SQL = `SELECT v.id, v.spnc, v.stock AS variant_stock,
       COUNT(u.id) FILTER (WHERE u.status = 'in_stock') AS unit_count
FROM product_variants v
LEFT JOIN product_units u ON u.variant_id = v.id
WHERE v.product_id IN (SELECT product_id FROM spncx_backfill_state
                       WHERE status = 'completed')
GROUP BY v.id, v.spnc, v.stock
HAVING v.stock <> COUNT(u.id) FILTER (WHERE u.status = 'in_stock');`

	reconciliationV1bSQL = `SELECT b.id, b.status AS lot_status, b.quantity,
       COUNT(u.id) AS unit_count
FROM inventory_balances b
LEFT JOIN product_units u ON u.balance_id = b.id
WHERE b.unit_tracked = TRUE AND b.status <> 'sellable'
  AND b.product_id IN (SELECT product_id FROM spncx_backfill_state WHERE status = 'completed')
GROUP BY b.id, b.status, b.quantity
HAVING b.quantity <> COUNT(u.id);`

	reconciliationV1cSQL = `SELECT u.id, u.code, u.status AS unit_status, b.status AS lot_status, b.location_key
FROM product_units u
JOIN inventory_balances b ON b.id = u.balance_id
WHERE u.product_id IN (SELECT product_id FROM spncx_backfill_state WHERE status = 'completed')
  AND u.status <> CASE
    WHEN b.status = 'sellable' THEN 'in_stock'
    WHEN b.status IN ('sales_reserved','return_requested','inbound_reserved','in_transit') THEN 'reserved'
    WHEN b.status = 'quarantine' AND b.location_key LIKE 'return:%' THEN 'returned'
    WHEN b.status = 'quarantine' THEN 'quarantined'
    WHEN b.status IN ('qc_accepted','unreconciled') THEN 'defined'
    WHEN b.status = 'damaged' THEN 'damaged'
    WHEN b.status = 'sold' THEN 'sold'
    ELSE '###NO-MAPPING###'
  END;`

	reconciliationV2SQL = `SELECT b.id, b.product_id, b.variant_id, b.quantity,
       COUNT(u.id) AS unit_count
FROM inventory_balances b
LEFT JOIN product_units u ON u.balance_id = b.id
WHERE b.unit_tracked = TRUE
  AND b.product_id IN (SELECT product_id FROM spncx_backfill_state WHERE status = 'completed')
GROUP BY b.id, b.product_id, b.variant_id, b.quantity
HAVING b.quantity <> COUNT(u.id);`

	reconciliationV3SQL = `WITH ledger AS (
  SELECT b.product_id, COALESCE(SUM(b.quantity), 0) AS ledger_sellable
  FROM inventory_balances b
  LEFT JOIN product_variants pv ON pv.id = b.variant_id
  WHERE b.status = 'sellable' AND (pv.id IS NULL OR pv.is_active)
  GROUP BY b.product_id
),
units AS (
  SELECT u.product_id, COUNT(*) AS units_in_stock
  FROM product_units u
  WHERE u.status = 'in_stock'
  GROUP BY u.product_id
)
SELECT p.id, p.stock AS product_stock,
       COALESCE(l.ledger_sellable, 0) AS ledger_sellable,
       COALESCE(u.units_in_stock, 0) AS units_in_stock
FROM products p
LEFT JOIN ledger l ON l.product_id = p.id
LEFT JOIN units u ON u.product_id = p.id
WHERE p.id IN (SELECT product_id FROM spncx_backfill_state WHERE status = 'completed')
  AND (p.stock <> COALESCE(l.ledger_sellable, 0)
   OR p.stock <> COALESCE(u.units_in_stock, 0));`

	reconciliationV4SQL = `SELECT (SELECT COUNT(*) FROM product_units u
        WHERE u.product_id IN (SELECT product_id FROM spncx_backfill_state WHERE status='completed')) AS total_units,              -- 4016
       (SELECT COUNT(*) FROM product_units u
        WHERE u.status='in_stock'
          AND u.product_id IN (SELECT product_id FROM spncx_backfill_state WHERE status='completed')) AS in_stock, -- 4016
       (SELECT COUNT(*) FROM product_units u
        WHERE u.status='removed'
          AND u.product_id IN (SELECT product_id FROM spncx_backfill_state WHERE status='completed')) AS removed,   -- 0
       (SELECT COUNT(DISTINCT code) FROM product_units u
        WHERE u.product_id IN (SELECT product_id FROM spncx_backfill_state WHERE status='completed')) AS distinct_codes,      -- = total_units
       (SELECT COUNT(*) FROM product_variant_unit_counters c
        JOIN product_variants v ON v.id = c.variant_id
        WHERE v.product_id IN (SELECT product_id FROM spncx_backfill_state WHERE status='completed')) AS counters;        -- 112`

	reconciliationV5SQL = `SELECT status, COUNT(*) FROM spncx_backfill_state GROUP BY status;`

	reconciliationV6SQL = `SELECT product_id, SUM(quantity) FROM inventory_balances
WHERE variant_id IS NULL AND status='sellable' AND unit_tracked=FALSE
  AND product_id IN (SELECT product_id FROM spncx_backfill_state WHERE status='completed')
GROUP BY product_id
HAVING SUM(quantity) <> 0;`

	reconciliationV6bListSQL = `SELECT b.product_id, b.quantity, b.location_key,
       s.excess_decision, s.completed_at + INTERVAL '30 days' AS review_by
FROM inventory_balances b
JOIN spncx_backfill_state s ON s.product_id = b.product_id
WHERE b.status='unreconciled' AND b.unit_tracked=FALSE AND s.status='completed';`

	reconciliationV6bGateSQL = `SELECT b.product_id, b.quantity, b.location_key,
       s.completed_at + INTERVAL '30 days' AS review_by
FROM inventory_balances b
JOIN spncx_backfill_state s ON s.product_id = b.product_id
WHERE b.status='unreconciled' AND b.unit_tracked=FALSE AND s.status='completed'
  AND s.excess_decision IS NULL
  AND s.completed_at + INTERVAL '30 days' < NOW();`

	reconciliationV7SQL = `SELECT s.product_id, p.stock AS product_stock,
       (s.snapshot->>'product_stock')::int AS snapshot_stock
FROM spncx_backfill_snapshot s
JOIN products p ON p.id = s.product_id
JOIN spncx_backfill_state st ON st.product_id = s.product_id AND st.status = 'completed'
WHERE p.stock <> (s.snapshot->>'product_stock')::int;`

	reconciliationV8SQL = `SELECT v.id, v.spnc, v.stock AS variant_stock,
       COUNT(u.id) FILTER (WHERE u.status = 'in_stock') AS units_in_stock
FROM spncx_backfill_state s
JOIN product_variants v ON v.product_id = s.product_id AND v.is_default
JOIN LATERAL jsonb_array_elements(s.plan->'alloc') a
  ON (a->>'variant_id')::uuid = v.id
LEFT JOIN product_units u ON u.variant_id = v.id
WHERE s.status = 'completed' AND (a->>'alloc')::int = 0
GROUP BY v.id, v.spnc, v.stock
HAVING v.stock <> 0 OR COUNT(u.id) FILTER (WHERE u.status = 'in_stock') <> 0;`

	reconciliationV9SQL = `SELECT v.id, v.product_id, v.stock AS variant_stock,
       (nv->>'stock')::int AS expected,
       (SELECT COUNT(*) FROM product_units u
        WHERE u.variant_id = v.id AND u.status = 'in_stock') AS units_in_stock
FROM spncx_backfill_state s,
     jsonb_array_elements(s.plan->'new_variants') AS nv
JOIN product_variants v ON v.id = (nv->>'variant_id')::uuid
WHERE s.status = 'completed'
  AND (v.stock <> (nv->>'stock')::int
       OR (SELECT COUNT(*) FROM product_units u
           WHERE u.variant_id = v.id AND u.status = 'in_stock') <> (nv->>'stock')::int);`

	reconciliationV11SQL = `SELECT v.id, v.spnc, v.inventory_mode
FROM product_variants v
WHERE v.product_id IN (SELECT product_id FROM spncx_backfill_state WHERE status='completed')
  AND v.inventory_mode <> 'tracked';`

	reconciliationV12SQL = `SELECT p.id, p.title, p.schema_version, p.product_type,
       COUNT(v.id) AS variant_count,
       COUNT(v.id) FILTER (WHERE v.inventory_mode = 'tracked' AND v.is_default) AS resolvable_variants
FROM products p
LEFT JOIN product_variants v ON v.product_id = p.id
WHERE p.id IN (SELECT product_id FROM spncx_backfill_state WHERE status = 'completed')
  AND p.id IN ('demo-mobile-gear-02', 'demo-men-essentials-18', 'demo-kitchen-pro-19',
               'demo-mobile-gear-19', 'demo-mobile-gear-20', 'demo-men-essentials-20',
               'prod-tapin-test-20261028')
GROUP BY p.id, p.title, p.schema_version, p.product_type
HAVING COUNT(v.id) <> 1
    OR COUNT(v.id) FILTER (WHERE v.inventory_mode = 'tracked' AND v.is_default) <> 1
    OR p.schema_version >= 2 OR p.product_type = 'variable';`
)

type ReconciliationReport struct {
	GeneratedAt    time.Time         `json:"generated_at"`
	Checks         []any             `json:"checks"`
	HeldLots       []json.RawMessage `json:"held_lots"`
	CoverageGaps   []json.RawMessage `json:"coverage_gaps"`
	FailedCaptures []json.RawMessage `json:"failed_captures"`
}

type reconciliationCheck struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

type reconciliationRowsCheck struct {
	reconciliationCheck
	Rows     []json.RawMessage `json:"rows"`
	RowCount int               `json:"row_count"`
	Details  string            `json:"details,omitempty"`
}

type reconciliationTotals struct {
	TotalUnits    int `json:"total_units"`
	InStock       int `json:"in_stock"`
	Removed       int `json:"removed"`
	DistinctCodes int `json:"distinct_codes"`
	Counters      int `json:"counters"`
}

type reconciliationTotalsCheck struct {
	reconciliationCheck
	Totals   reconciliationTotals `json:"totals"`
	Expected reconciliationTotals `json:"expected"`
}

type reconciliationCompletionCheck struct {
	reconciliationCheck
	Summary backfillStatusCounts `json:"summary"`
}

type reconciliationPhysicalCheck struct {
	reconciliationCheck
	SampledProducts []string          `json:"sampled_products"`
	Mismatches      []json.RawMessage `json:"mismatches"`
	Details         string            `json:"details"`
}

// RunReconciliationReport reads one snapshot so the checks and review queues
// agree even when inventory is being updated.
func RunReconciliationReport(ctx context.Context, db *sql.DB) (*ReconciliationReport, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin inventory reconciliation: %w", err)
	}
	defer tx.Rollback()

	report := &ReconciliationReport{Checks: make([]any, 0, 16)}
	if err := tx.QueryRowContext(ctx, `SELECT NOW()`).Scan(&report.GeneratedAt); err != nil {
		return nil, fmt.Errorf("read reconciliation timestamp: %w", err)
	}
	for _, check := range []struct {
		id, name, query string
	}{
		{"V0", "order_history", ""},
		{"V1", "variant_sellable_units", reconciliationV1SQL},
		{"V1b", "non_sellable_units_vs_lots", reconciliationV1bSQL},
		{"V1c", "unit_status_vs_lot_status", reconciliationV1cSQL},
		{"V2", "lot_invariant", reconciliationV2SQL},
		{"V3", "product_sellable_equality", reconciliationV3SQL},
		{"V4", "global_totals", ""},
		{"V5", "backfill_completion", ""},
		{"V6", "unallocated_sellable", reconciliationV6SQL},
		{"V6b", "unreconciled_holding_lots", reconciliationV6bListSQL},
		{"V7", "product_stock_unchanged", reconciliationV7SQL},
		{"V8", "doubled_defaults_zero", reconciliationV8SQL},
		{"V9", "new_variants", reconciliationV9SQL},
		{"V10", "physical_count_sample", ""},
		{"V11", "variants_flipped_tracked", reconciliationV11SQL},
		{"V12", "variant_less_resolution", reconciliationV12SQL},
	} {
		base := reconciliationCheck{ID: check.id, Name: check.name}
		var result any
		switch check.id {
		case "V0":
			result, err = reconciliationOrderHistory(ctx, tx, base)
		case "V4":
			result, err = reconciliationCheckTotals(ctx, tx, base)
		case "V5":
			result, err = reconciliationCheckCompletion(ctx, tx, base)
		case "V10":
			result = reconciliationPhysicalCheck{reconciliationCheck: base, SampledProducts: []string{},
				Mismatches: []json.RawMessage{}, Details: "no physical-count sample recorded in v1"}
		default:
			result, err = reconciliationCheckRows(ctx, tx, base, check.query)
		}
		if err != nil {
			return nil, fmt.Errorf("inventory reconciliation %s: %w", check.id, err)
		}
		report.Checks = append(report.Checks, result)
	}

	report.HeldLots, _, err = reconciliationJSONRows(ctx, tx, `SELECT b.product_id,b.location_key,b.quantity,
		s.completed_at + INTERVAL '30 days' AS review_by,s.excess_decision
		FROM inventory_balances b
		LEFT JOIN spncx_backfill_state s ON s.product_id=b.product_id AND b.location_key='spncx-excess:'||b.product_id
		WHERE b.status='unreconciled' AND b.quantity>0 ORDER BY b.product_id,b.location_key,b.id`, -1)
	if err != nil {
		return nil, fmt.Errorf("read reconciliation held lots: %w", err)
	}
	report.CoverageGaps, _, err = reconciliationJSONRows(ctx, tx, `SELECT
		COALESCE(metadata->>'variant_id',variant_id::text) AS variant_id,
		COALESCE((metadata->>'requested')::int,quantity) AS requested,
		COALESCE((metadata->>'allocated')::int,0) AS allocated,
		COALESCE((metadata->>'shortfall')::int,quantity) AS shortfall,
		COALESCE(metadata->>'operation',reference_id,'') AS operation
		FROM inventory_movements WHERE reference_type='unit_coverage_gap'
		ORDER BY created_at DESC,id DESC LIMIT 100`, -1)
	if err != nil {
		return nil, fmt.Errorf("read reconciliation coverage gaps: %w", err)
	}
	report.FailedCaptures, _, err = reconciliationJSONRows(ctx, tx, `SELECT order_id,failure,mode,at
		FROM spncx_failed_captures ORDER BY at DESC,order_id`, -1)
	if err != nil {
		return nil, fmt.Errorf("read reconciliation failed captures: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("finish inventory reconciliation: %w", err)
	}
	return report, nil
}

const reconciliationRowLimit = 100

// PostgreSQL preserves column names, numeric types and nulls in row_to_json.
// Window counting lets the database return only the rows the report retains.
func reconciliationJSONRows(ctx context.Context, tx *sql.Tx, query string, limit int) ([]json.RawMessage, int, error) {
	query = "SELECT row_to_json(evidence), COUNT(*) OVER () FROM (" + strings.TrimSuffix(query, ";") + "\n) AS evidence"
	if limit >= 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := []json.RawMessage{}
	var count int
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw, &count); err != nil {
			return nil, 0, err
		}
		result = append(result, json.RawMessage(raw))
	}
	return result, count, rows.Err()
}

func reconciliationCheckRows(ctx context.Context, tx *sql.Tx, base reconciliationCheck, query string) (reconciliationRowsCheck, error) {
	rows, count, err := reconciliationJSONRows(ctx, tx, query, reconciliationRowLimit)
	result := reconciliationRowsCheck{reconciliationCheck: base, Rows: rows, RowCount: count}
	if err != nil {
		return result, err
	}
	result.Passed = count == 0
	if base.ID == "V6b" {
		gate, err := tx.QueryContext(ctx, reconciliationV6bGateSQL)
		if err != nil {
			return result, fmt.Errorf("read overdue excess decisions: %w", err)
		}
		defer gate.Close()
		result.Passed = true
		for gate.Next() {
			result.Passed = false
		}
		return result, gate.Err()
	}
	return result, nil
}

func reconciliationCheckTotals(ctx context.Context, tx *sql.Tx, base reconciliationCheck) (reconciliationTotalsCheck, error) {
	result := reconciliationTotalsCheck{reconciliationCheck: base}
	t := &result.Totals
	if err := tx.QueryRowContext(ctx, reconciliationV4SQL).Scan(&t.TotalUnits, &t.InStock, &t.Removed, &t.DistinctCodes, &t.Counters); err != nil {
		return result, fmt.Errorf("read actual totals: %w", err)
	}
	e := &result.Expected
	err := tx.QueryRowContext(ctx, `WITH completed AS (
		SELECT plan FROM spncx_backfill_state WHERE status='completed'
	)
	SELECT COALESCE(SUM((plan->>'P')::int),0),
		(SELECT COUNT(*) FROM completed, jsonb_array_elements(plan->'alloc') a WHERE (a->>'alloc')::int>0)
	FROM completed`).Scan(&e.TotalUnits, &e.Counters)
	if err != nil {
		return result, fmt.Errorf("read expected totals from completed plans: %w", err)
	}
	e.InStock, e.DistinctCodes = e.TotalUnits, e.TotalUnits
	result.Passed = result.Totals == result.Expected
	return result, nil
}

func reconciliationCheckCompletion(ctx context.Context, tx *sql.Tx, base reconciliationCheck) (reconciliationCompletionCheck, error) {
	result := reconciliationCompletionCheck{reconciliationCheck: base}
	rows, err := tx.QueryContext(ctx, reconciliationV5SQL)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	var total int
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return result, err
		}
		total += count
		result.Summary.add(status, count)
	}
	s := result.Summary
	result.Passed = s.Failed == 0 && s.Pending == 0 && s.Completed > 0 && s.Completed == total-s.Excluded
	return result, rows.Err()
}

func reconciliationOrderHistory(ctx context.Context, tx *sql.Tx, base reconciliationCheck) (reconciliationRowsCheck, error) {
	query := `WITH doubled_flags AS (` + strings.TrimSuffix(reconciliationV0aSQL, ";") + `
	), trim_plans AS (
		SELECT product_id, plan, plan->>'trim_target' AS target
		FROM spncx_backfill_state WHERE plan->>'trim_target' IS NOT NULL
	), order_counts AS (
		SELECT v.product_id, v.id::text AS variant_id, COUNT(oi.id) AS orders
		FROM trim_plans p
		JOIN product_variants v ON v.product_id=p.product_id
		LEFT JOIN order_items oi ON oi.variant_id=v.id
		GROUP BY v.product_id,v.id
	), trim_flags AS (
		SELECT p.product_id
		FROM trim_plans p
		JOIN order_counts t ON t.product_id=p.product_id AND t.variant_id=p.target
		WHERE t.orders > COALESCE((
			SELECT MAX(k.orders)
			FROM jsonb_array_elements(p.plan->'alloc') a
			JOIN order_counts k ON k.product_id=p.product_id AND k.variant_id=a->>'variant_id'
			WHERE k.variant_id<>p.target AND (a->>'alloc')::int>0
		),0)
	)
	SELECT id AS product_id,'V0a' AS source FROM doubled_flags
	UNION
	SELECT product_id,'V0b' AS source FROM trim_flags
	ORDER BY product_id,source`
	rows, count, err := reconciliationJSONRows(ctx, tx, query, reconciliationRowLimit)
	result := reconciliationRowsCheck{reconciliationCheck: base, Rows: rows, RowCount: count}
	if err != nil {
		return result, err
	}
	result.Passed = count == 0

	// The B5 group is internal and isn't serialized. For products without a
	// plan, identify "other" using the live B5 stock classification as well
	// as the three explicitly named V0b products from the spec.
	missing, err := tx.QueryContext(ctx, `SELECT p.id
		FROM products p
		LEFT JOIN spncx_backfill_state s ON s.product_id=p.id
		CROSS JOIN LATERAL (
			SELECT SUM(v.stock) AS total,
				COALESCE(SUM(v.stock) FILTER (WHERE NOT v.is_default),0) AS named,
				COALESCE(BOOL_OR(v.is_default AND v.stock=p.stock),FALSE) AS mirrored_default
			FROM product_variants v WHERE v.product_id=p.id
		) v
		WHERE s.plan->>'product_id' IS NULL
			AND (p.id IN ('prod-vitamin-serum','prod-brush-set','demo-women-style-19')
				OR (v.total>p.stock AND NOT (v.mirrored_default AND v.named=p.stock AND p.stock>0)))
		ORDER BY p.id`)
	if err != nil {
		return result, fmt.Errorf("list other products without dry-run plans: %w", err)
	}
	defer missing.Close()
	var ids []string
	for missing.Next() {
		var id string
		if err := missing.Scan(&id); err != nil {
			return result, err
		}
		ids = append(ids, id)
	}
	if len(ids) != 0 {
		result.Details = fmt.Sprintf("V0b unevaluated (no dry-run plan): %v", ids)
	}
	return result, missing.Err()
}
