// FIN-008 (G-15): the evidenced opening-cash table. An opening
// balance is only money when it carries evidence; the table's CHECK
// must reject unevidenced rows at the database level, and an
// evidenced row must be accepted.

package repository

import (
	"os"
	"strings"
	"testing"

	"github.com/drup/platform/internal/testdb"
)

func TestTreasuryOpeningCashRequiresEvidence(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM treasury_opening_cash WHERE id LIKE 'fin008-%'`)
	})

	// The evidence CHECK exists on the table.
	var checkDef string
	err = db.QueryRow(`SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conrelid='treasury_opening_cash'::regclass AND contype='c'
		AND pg_get_constraintdef(oid) LIKE '%evidence_reference%'`).Scan(&checkDef)
	if err != nil {
		t.Fatalf("opening-cash evidence CHECK missing: %v", err)
	}
	if !strings.Contains(checkDef, "evidence_reference") {
		t.Fatalf("unexpected evidence CHECK definition: %s", checkDef)
	}

	// Unevidenced inserts are rejected.
	if _, err := db.Exec(`INSERT INTO treasury_opening_cash(id,amount_toman,currency,evidence_kind,evidence_reference)
		VALUES('fin008-no-evidence',7000000,'TOMAN','bank_statement','')`); err == nil {
		t.Fatal("opening cash with an empty evidence reference was accepted")
	}
	if _, err := db.Exec(`INSERT INTO treasury_opening_cash(id,amount_toman,currency,evidence_kind,evidence_reference)
		VALUES('fin008-null-evidence',7000000,'TOMAN','bank_statement',NULL)`); err == nil {
		t.Fatal("opening cash with a NULL evidence reference was accepted")
	}
	if _, err := db.Exec(`INSERT INTO treasury_opening_cash(id,amount_toman,currency,evidence_kind,evidence_reference)
		VALUES('fin008-null-kind',7000000,'TOMAN',NULL,'stmt-x')`); err == nil {
		t.Fatal("opening cash with a NULL evidence kind was accepted")
	}

	// An evidenced insert is accepted.
	if _, err := db.Exec(`INSERT INTO treasury_opening_cash(id,amount_toman,currency,evidence_kind,evidence_reference)
		VALUES('fin008-evidence-ok',7000000,'TOMAN','bank_statement','stmt-test-1')`); err != nil {
		t.Fatalf("evidenced opening cash rejected: %v", err)
	}
	var amount int64
	if err := db.QueryRow(`SELECT amount_toman FROM treasury_opening_cash WHERE id='fin008-evidence-ok'`).Scan(&amount); err != nil {
		t.Fatalf("read back evidenced opening cash: %v", err)
	}
	if amount != 7000000 {
		t.Fatalf("opening cash amount=%d want 7000000", amount)
	}
}
