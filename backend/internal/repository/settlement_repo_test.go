package repository

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/testdb"
	"github.com/google/uuid"
)

func TestEffectiveSettlementDay(t *testing.T) {
	cases := []struct {
		day   int
		year  int
		month time.Month
		want  int
	}{
		{10, 2026, time.November, 10},
		{31, 2026, time.April, 30},
		{31, 2026, time.February, 28},
		{29, 2024, time.February, 29},
		{31, 2026, time.January, 31},
		{1, 2026, time.November, 1},
	}
	for _, c := range cases {
		if got := effectiveSettlementDay(c.day, c.year, c.month); got != c.want {
			t.Errorf("effectiveSettlementDay(%d, %d, %s) = %d, want %d", c.day, c.year, c.month, got, c.want)
		}
	}
}

func TestEarlyWithdrawalConstants(t *testing.T) {
	if EarlyWithdrawalSLAHours != 24 {
		t.Errorf("EarlyWithdrawalSLAHours = %d, want 24", EarlyWithdrawalSLAHours)
	}
}

func TestEarlyWithdrawalMinimumMigration(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	const migration = "../../migrations/202611240001_seller_early_withdrawal_minimum"
	up, err := os.ReadFile(migration + ".up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile(migration + ".down.sql")
	if err != nil {
		t.Fatal(err)
	}
	const columnExists = `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema=current_schema() AND table_name='platform_settings'
		AND column_name='early_withdrawal_minimum_toman')`
	assertColumn := func(want bool) {
		t.Helper()
		var exists bool
		if err := db.QueryRow(columnExists).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != want {
			t.Fatalf("minimum column exists = %v, want %v", exists, want)
		}
	}
	assertColumn(true)
	var prior int64
	if err := db.QueryRow(`SELECT early_withdrawal_minimum_toman FROM platform_settings WHERE id=1`).Scan(&prior); err != nil {
		t.Fatal(err)
	}
	const updateFloor = `UPDATE platform_settings SET early_withdrawal_minimum_toman=$1 WHERE id=1`
	t.Cleanup(func() {
		var exists bool
		if err := db.QueryRow(columnExists).Scan(&exists); err != nil {
			t.Error(err)
			return
		}
		if !exists {
			if _, err := db.Exec(string(up)); err != nil {
				t.Errorf("restore minimum column: %v", err)
				return
			}
		}
		if _, err := db.Exec(updateFloor, prior); err != nil {
			t.Errorf("restore minimum: %v", err)
		}
	})

	var minimum int64
	if err := db.QueryRow(`UPDATE platform_settings SET early_withdrawal_minimum_toman=DEFAULT
		WHERE id=1 RETURNING early_withdrawal_minimum_toman`).Scan(&minimum); err != nil {
		t.Fatal(err)
	}
	if minimum != 1000000 {
		t.Fatalf("minimum default = %d, want 1000000", minimum)
	}
	for _, invalid := range []int64{0, 9007199254740992} {
		if _, err := db.Exec(updateFloor, invalid); err == nil {
			t.Errorf("minimum CHECK accepted %d", invalid)
		}
	}
	if _, err := db.Exec(string(down)); err != nil {
		t.Fatal(err)
	}
	assertColumn(false)
	if _, err := db.Exec(string(up)); err != nil {
		t.Fatal(err)
	}
	assertColumn(true)
	if err := db.QueryRow(`SELECT early_withdrawal_minimum_toman FROM platform_settings WHERE id=1`).Scan(&minimum); err != nil {
		t.Fatal(err)
	}
	if minimum != 1000000 {
		t.Fatalf("minimum after reapplying migration = %d, want 1000000", minimum)
	}
}

func TestGetSettlementSettingsEarlyWithdrawalMinimum(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	repo := NewFinanceRepository(db)
	assertMinimum := func(want int64) {
		t.Helper()
		settings, err := repo.GetSettlementSettings(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if settings.EarlyWithdrawalMinimumToman == nil {
			t.Fatal("minimum missing from settlement settings")
		}
		if got := *settings.EarlyWithdrawalMinimumToman; got != want {
			t.Fatalf("minimum = %d, want %d", got, want)
		}
	}
	assertMinimum(1000000)
	const updateFloor = `UPDATE platform_settings SET early_withdrawal_minimum_toman=$1 WHERE id=1`
	t.Cleanup(func() {
		if _, err := db.Exec(updateFloor, 1000000); err != nil {
			t.Errorf("restore minimum: %v", err)
		}
	})
	if _, err := db.Exec(updateFloor, 2000000); err != nil {
		t.Fatal(err)
	}
	assertMinimum(2000000)
}

func TestEarlyWithdrawalBelowFloorReturnsTypedErrorAfterReplayCheck(t *testing.T) {
	db, err := testdb.Open(t, os.Getenv("SELLERSHO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	owner := uuid.NewString()
	storeID := "early-floor-" + uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id,mobile,full_name,role) VALUES($1,$2,'Early withdrawal fixture','seller')`, owner, "f"+owner[:12]); err != nil {
		t.Fatal(err)
	}
	if err := NewStoreRepository(db).CreateStore(ctx, domain.Store{
		ID: storeID, Name: "Early withdrawal fixture", Slug: storeID, OwnerType: "dropshipper",
		OwnerUserID: &owner, ActivePlanID: "trial", ReferralCode: uuid.NewString()[:8],
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewFinanceRepository(db)
	withdrawal, err := repo.RequestEarlyWithdrawal(ctx, owner, storeID, 999999, "IR060170000000000000000000", "Fixture bank", "floor-test")
	if withdrawal != nil || !errors.Is(err, ErrEarlyWithdrawalTooSmall) {
		t.Fatalf("withdrawal=%+v error=%v, want ErrEarlyWithdrawalTooSmall", withdrawal, err)
	}
	var belowFloor *EarlyWithdrawalTooSmallError
	if !errors.As(err, &belowFloor) || belowFloor.Minimum != 1000000 {
		t.Fatalf("typed floor error = %+v, want minimum 1000000", belowFloor)
	}
}

func TestEarlyWithdrawalFillDeadline(t *testing.T) {
	created := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	pending := EarlyWithdrawal{Status: "pending", CreatedAt: created}
	pending.fillDeadline()
	if pending.DeadlineAt == nil || !pending.DeadlineAt.Equal(created.Add(24*time.Hour)) {
		t.Errorf("pending deadline = %v, want %v", pending.DeadlineAt, created.Add(24*time.Hour))
	}
	approved := EarlyWithdrawal{Status: "approved", CreatedAt: created}
	approved.fillDeadline()
	if approved.DeadlineAt != nil {
		t.Errorf("approved deadline = %v, want nil", approved.DeadlineAt)
	}
}
