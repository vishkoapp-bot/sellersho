package repository

// Storage-fee engine (gap 7). Implements the owner's recorded Oct 30
// decision: storage fees exist as a system with tariffs defaulting to
// 2,000 Toman per kg per day + 8,000 Toman per m3 per day, 30 free
// days, and the feature ships with its flag DEFAULT OFF — activation
// is a separate owner decision (platform_settings.storage_fee_enabled,
// migration 202611050005).
//
// Known QC receipts own separate immutable age, physical SKU/warehouse,
// measurement and tariff snapshots. Completed Tehran days use the quantity held
// at that day's midnight; physical departure retains unpaid completed days.
// Assessment is bounded and idempotent by receipt/day. MoreWork means another
// pass is needed. Old unknown receipt histories remain unbillable and visible
// in warehouse_storage_history_audit; catalog edits cannot certify their past.
// Fees reduce supplier settlement availability on read and are never cash payouts.
//
// RunStorageFeeAssessment is a plain callable: the admin endpoint
// POST /admin/storage-fees/run invokes it today; a scheduler could
// invoke it daily later. No scheduler registration ships with this
// change, and while the flag is OFF the run refuses (ErrStorageFeeDisabled).

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"time"

	"github.com/drup/platform/internal/pricing"
)

// ErrStorageFeeDisabled is returned by RunStorageFeeAssessment while
// platform_settings.storage_fee_enabled is false (or the settings
// columns do not exist yet — the shipped default state).
var ErrStorageFeeDisabled = errors.New("storage fee assessment is disabled")

// StorageFeeSettings is the platform_settings storage-fee block
// (migration 202611050005). All amounts are integer Tomans.
type StorageFeeSettings struct {
	Enabled  bool  `json:"storageFeeEnabled"`
	FreeDays int   `json:"storageFeeFreeDays"`
	PerKgDay int64 `json:"storageFeePerKgDayToman"`
	PerM3Day int64 `json:"storageFeePerM3DayToman"`
}

// DefaultStorageFeeSettings mirrors the migration defaults (flag OFF).
func DefaultStorageFeeSettings() StorageFeeSettings {
	return StorageFeeSettings{Enabled: false, FreeDays: 30, PerKgDay: 2000, PerM3Day: 8000}
}

// StorageFeeRunSummary is one assessment run's report.
type StorageFeeRunSummary struct {
	ReceiptsExamined              int   `json:"receiptsExamined"`
	MoreWork                      bool  `json:"moreWork"`
	VerifiedReceiptHistoryOnly    bool  `json:"verifiedReceiptHistoryOnly"`
	UnresolvedSampleLimited       bool  `json:"unresolvedSampleLimited"`
	PositionsCharged              int   `json:"positionsCharged"`
	PositionsSkippedNoMeasurement int   `json:"positionsSkippedNoMeasurement"`
	ChargesCreated                int   `json:"chargesCreated"`
	TotalCharged                  int64 `json:"totalCharged"`
}

// GetStorageFeeSettings reads the storage-fee settings columns of the
// singleton platform_settings row. When the columns do not exist yet
// (migration 202611050005 not applied) it returns the shipped defaults
// with the flag OFF, so the engine stays inert — the same fail-closed
// posture as the balance SQL's to_regclass gates.
func (r *SupplierRepository) GetStorageFeeSettings(ctx context.Context) (StorageFeeSettings, error) {
	settings := DefaultStorageFeeSettings()
	var cols int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'platform_settings'
		  AND column_name IN ('storage_fee_enabled','storage_fee_free_days','storage_fee_per_kg_day','storage_fee_per_m3_day')`).Scan(&cols); err != nil {
		return settings, err
	}
	if cols < 4 {
		return settings, nil
	}
	err := r.db.QueryRowContext(ctx, `SELECT storage_fee_enabled, storage_fee_free_days, storage_fee_per_kg_day, storage_fee_per_m3_day
		FROM platform_settings WHERE id = 1`).Scan(&settings.Enabled, &settings.FreeDays, &settings.PerKgDay, &settings.PerM3Day)
	if err == sql.ErrNoRows {
		return DefaultStorageFeeSettings(), nil
	}
	if err != nil {
		return settings, err
	}
	if settings.FreeDays < 0 {
		settings.FreeDays = 0
	}
	return settings, nil
}

var storageFeeTehranLoc = loadTehranLocation()

func loadTehranLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		return time.UTC
	}
	return loc
}

// tehranDay returns the Asia/Tehran calendar date of t, expressed as a
// UTC-midnight time.Time (the shape DATE columns scan into).
func tehranDay(t time.Time) time.Time {
	lt := t.In(storageFeeTehranLoc)
	return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, time.UTC)
}

// storageFeeUnitDayToman is one unit's daily fee in whole Tomans:
// weight and volume components BOTH apply (the recorded two-tariff
// decision), rounded to the nearest Toman.
func storageFeeUnitDayToman(weightGrams, lengthMM, widthMM, heightMM int, settings StorageFeeSettings) (int64, error) {
	if weightGrams <= 0 || weightGrams > 1000000000 || lengthMM <= 0 || lengthMM > 1000000 || widthMM <= 0 || widthMM > 1000000 || heightMM <= 0 || heightMM > 1000000 || settings.PerKgDay < 0 || settings.PerKgDay > pricing.MaxTransportMoney || settings.PerM3Day < 0 || settings.PerM3Day > pricing.MaxTransportMoney {
		return 0, pricing.ErrInvalidMoney
	}
	// Common denominator1e9: grams/1000 and mm³/1e9. Sum both
	// exact components before the one nearest-Toman, half-up rounding.
	weight := new(big.Int).Mul(big.NewInt(int64(weightGrams)), big.NewInt(settings.PerKgDay))
	weight.Mul(weight, big.NewInt(1000000))
	volume := big.NewInt(int64(lengthMM))
	for _, factor := range []int64{int64(widthMM), int64(heightMM), settings.PerM3Day} {
		volume.Mul(volume, big.NewInt(factor))
	}
	numerator := new(big.Int).Add(weight, volume)
	denominator := big.NewInt(1000000000)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(denominator) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() || quotient.Cmp(big.NewInt(pricing.MaxTransportMoney)) > 0 {
		return 0, pricing.ErrInvalidMoney
	}
	return quotient.Int64(), nil
}

// RunStorageFeeAssessment uses frozen receipt facts and bounded quantity history.
// It retains completed unpaid days even after the current position has closed.
func (r *SupplierRepository) RunStorageFeeAssessment(ctx context.Context) (StorageFeeRunSummary, error) {
	return r.runStorageHistoryAssessment(ctx, time.Now())
}
