package repository

import (
	"context"
	"database/sql"
)

// FIN-014 unresolved-exclusion gate (W7, audit §5.2 #5).
//
// An automatic payout/refund decision MUST consult this gate before
// acting on a record (order, store, or store-month). It returns the
// unresolved FIN-014 rows covering that record; a non-empty result
// means the decision is REFUSED and the case stays on the unresolved
// queue. A cleared record returns an empty slice.
//
// SCOPE — specification + helper, NOT live wiring: the gate is not
// connected to any production payout handler. Wiring it into a money
// path touches that path and needs supervisor + finance-reviewer
// approval (audit §7 W7); this file deliberately does not do it. Any
// caller that acts on a record without consulting this gate, or that
// treats a blocked record as clear, is a defect — the gate is the
// contract, not an advisory.
//
// The gate blocks on UNRESOLVED rows only. Certain defects are
// excluded by the release gate (W8: fail-closed default of zero
// certain defects), and ambiguous rows are held for a reviewer —
// neither belongs in this gate, which exists so that an unknown can
// never be auto-resolved by guessing.
//
// The helper reads the current dry-run report for the prefix (limit
// 100 rows, the dry-run scope). A live wiring must page through the
// full report instead of trusting a single page.
func FIN014UnresolvedGate(ctx context.Context, db *sql.DB, prefix, recordID string) ([]FIN014ReportRow, error) {
	report, err := GetFIN014Report(ctx, db, FIN014ReportOptions{Limit: 100, IDPrefix: prefix})
	if err != nil {
		return nil, err
	}
	var blocking []FIN014ReportRow
	for _, row := range report.Rows {
		if row.Confidence != FIN014ConfidenceUnresolved {
			continue
		}
		if row.RecordID == recordID || row.OrderID == recordID || row.StoreID == recordID {
			blocking = append(blocking, row)
		}
	}
	return blocking, nil
}
