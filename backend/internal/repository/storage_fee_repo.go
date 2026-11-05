package repository

// Storage-fee engine (gap 7). Implements the owner's recorded Oct 30
// decision: storage fees exist as a system with tariffs defaulting to
// 2,000 Toman per kg per day + 8,000 Toman per m3 per day, 30 free
// days, and the feature ships with its flag DEFAULT OFF — activation
// is a separate owner decision (platform_settings.storage_fee_enabled,
// migration 202611050005).
//
// The assessment walks warehouse_storage_positions (migration
// 202610280005) and writes one warehouse_storage_fee_charges row per
// position per chargeable calendar day (Asia/Tehran); the table's
// UNIQUE(position_id, fee_date) is the idempotency anchor, so a re-run
// never double-charges the same position-day. Charges are deducted
// from the supplier balance on read (see supplierBalanceSQLWithStorage
// in supplier_panel_repo.go), so Available drops by exactly the
// charged total.
//
// Per-position per-day fee: the product's per-unit daily rate
// (weight_kg x per-kg tariff + volume_m3 x per-m3 tariff, rounded to
// the nearest Toman) multiplied by the position quantity. A position
// whose product lacks ANY of weight/length/width/height is skipped
// and counted — dimensions are never guessed. Only fully elapsed days
// are charged (a day is billed once it has completed), and the first
// storage_fee_free_days calendar days from original_stocked_at are
// free (the original timestamp is used so an inter-warehouse transfer
// never resets the free window — migration 202610280005 §5.7).
//
// RunStorageFeeAssessment is a plain callable: the admin endpoint
// POST /admin/storage-fees/run invokes it today; a scheduler could
// invoke it daily later. No scheduler registration ships with this
// change, and while the flag is OFF the run refuses (ErrStorageFeeDisabled).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

// ErrStorageFeeDisabled is returned by RunStorageFeeAssessment while
// platform_settings.storage_fee_enabled is false (or the settings
// columns do not exist yet — the shipped default state).
var ErrStorageFeeDisabled = errors.New("storage fee assessment is disabled")

// StorageFeeSettings is the platform_settings storage-fee block
// (migration 202611050005). All amounts are integer Tomans.
type StorageFeeSettings struct {
	Enabled      bool  `json:"storageFeeEnabled"`
	FreeDays     int   `json:"storageFeeFreeDays"`
	PerKgDay     int64 `json:"storageFeePerKgDayToman"`
	PerM3Day     int64 `json:"storageFeePerM3DayToman"`
}

// DefaultStorageFeeSettings mirrors the migration defaults (flag OFF).
func DefaultStorageFeeSettings() StorageFeeSettings {
	return StorageFeeSettings{Enabled: false, FreeDays: 30, PerKgDay: 2000, PerM3Day: 8000}
}

// StorageFeeRunSummary is one assessment run's report.
type StorageFeeRunSummary struct {
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
func storageFeeUnitDayToman(weightGrams, lengthMM, widthMM, heightMM int, settings StorageFeeSettings) int64 {
	weightKg := float64(weightGrams) / 1000.0
	volumeM3 := float64(lengthMM) * float64(widthMM) * float64(heightMM) / 1e9
	fee := weightKg*float64(settings.PerKgDay) + volumeM3*float64(settings.PerM3Day)
	return int64(math.Floor(fee + 0.5))
}

type storagePositionRow struct {
	id                string
	supplierID        string
	productID         string
	warehouseID       string
	goodsClass        string
	qty               int
	originalStockedAt time.Time
	lastFeeDate       sql.NullTime
	lastAssessedDate  sql.NullTime
	weightGrams       sql.NullInt64
	lengthMM          sql.NullInt64
	widthMM           sql.NullInt64
	heightMM          sql.NullInt64
}

func (p storagePositionRow) measured() bool {
	return p.weightGrams.Valid && p.weightGrams.Int64 > 0 &&
		p.lengthMM.Valid && p.lengthMM.Int64 > 0 &&
		p.widthMM.Valid && p.widthMM.Int64 > 0 &&
		p.heightMM.Valid && p.heightMM.Int64 > 0
}

// RunStorageFeeAssessment charges every open storage position for its
// uncharged, fully elapsed days beyond the free window, in one
// transaction. It refuses (ErrStorageFeeDisabled) while the flag is
// OFF. Safe to re-run: UNIQUE(position_id, fee_date) plus the
// last_fee_date / last_assessed_date checkpoints make repeat runs
// no-ops for already-assessed days.
func (r *SupplierRepository) RunStorageFeeAssessment(ctx context.Context) (StorageFeeRunSummary, error) {
	var summary StorageFeeRunSummary
	settings, err := r.GetStorageFeeSettings(ctx)
	if err != nil {
		return summary, err
	}
	if !settings.Enabled {
		return summary, ErrStorageFeeDisabled
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return summary, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT p.id, p.supplier_id, p.product_id, p.warehouse_id, p.goods_class, p.qty,
		       p.original_stocked_at, p.last_fee_date, p.last_assessed_date,
		       pr.weight_grams, pr.length_mm, pr.width_mm, pr.height_mm
		FROM warehouse_storage_positions p
		JOIN products pr ON pr.id = p.product_id
		WHERE p.closed_at IS NULL AND p.qty > 0
		ORDER BY p.id`)
	if err != nil {
		return summary, err
	}
	positions := []storagePositionRow{}
	for rows.Next() {
		var p storagePositionRow
		if err := rows.Scan(&p.id, &p.supplierID, &p.productID, &p.warehouseID, &p.goodsClass, &p.qty,
			&p.originalStockedAt, &p.lastFeeDate, &p.lastAssessedDate,
			&p.weightGrams, &p.lengthMM, &p.widthMM, &p.heightMM); err != nil {
			rows.Close()
			return summary, err
		}
		positions = append(positions, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return summary, err
	}
	rows.Close()

	today := tehranDay(time.Now())
	lastChargeableDay := today.AddDate(0, 0, -1) // only fully elapsed days are billed

	for _, p := range positions {
		if !p.measured() {
			// Never guess dimensions. Checkpoints stay untouched, so
			// the position is assessed once measurements exist.
			summary.PositionsSkippedNoMeasurement++
			continue
		}
		unitDay := storageFeeUnitDayToman(int(p.weightGrams.Int64), int(p.lengthMM.Int64), int(p.widthMM.Int64), int(p.heightMM.Int64), settings)
		amountPerDay := unitDay * int64(p.qty)

		firstChargeable := tehranDay(p.originalStockedAt).AddDate(0, 0, settings.FreeDays)
		windowStart := firstChargeable
		for _, checkpoint := range []sql.NullTime{p.lastFeeDate, p.lastAssessedDate} {
			if checkpoint.Valid {
				if next := tehranDay(checkpoint.Time).AddDate(0, 0, 1); next.After(windowStart) {
					windowStart = next
				}
			}
		}
		if windowStart.After(lastChargeableDay) {
			// Nothing to bill yet; still record that the pass looked
			// at the position through the last elapsed day.
			if err := advanceStorageAssessed(ctx, tx, p, lastChargeableDay); err != nil {
				return summary, err
			}
			continue
		}

		chargedHere := false
		var lastCharged time.Time
		var insertedTotal int64
		for day := windowStart; !day.After(lastChargeableDay); day = day.AddDate(0, 0, 1) {
			if amountPerDay > 0 {
				res, err := tx.ExecContext(ctx, `
					INSERT INTO warehouse_storage_fee_charges
						(id, position_id, supplier_id, product_id, warehouse_id, fee_date, qty, fee_unit_day_toman, amount_toman, goods_class, estimated)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, FALSE)
					ON CONFLICT (position_id, fee_date) DO NOTHING`,
					"WSF-"+uuid.NewString(), p.id, p.supplierID, p.productID, p.warehouseID, day, p.qty, unitDay, amountPerDay, p.goodsClass)
				if err != nil {
					return summary, err
				}
				affected, err := res.RowsAffected()
				if err != nil {
					return summary, err
				}
				if affected > 0 {
					chargedHere = true
					lastCharged = day
					insertedTotal += amountPerDay
					summary.ChargesCreated++
				}
			}
		}
		if chargedHere {
			summary.PositionsCharged++
			summary.TotalCharged += insertedTotal
			if _, err := tx.ExecContext(ctx, `
				UPDATE warehouse_storage_positions
				SET last_fee_date = $2, last_assessed_date = $3,
				    total_fee_charged_toman = total_fee_charged_toman + $4, updated_at = NOW()
				WHERE id = $1`, p.id, lastCharged, lastChargeableDay, insertedTotal); err != nil {
				return summary, err
			}
		} else {
			if err := advanceStorageAssessed(ctx, tx, p, lastChargeableDay); err != nil {
				return summary, err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return summary, fmt.Errorf("commit storage fee assessment: %w", err)
	}
	return summary, nil
}

// advanceStorageAssessed moves a position's last_assessed_date forward
// (never backward) without touching last_fee_date: the pass has looked
// at those days, so they are never re-scanned, but no charge was made.
func advanceStorageAssessed(ctx context.Context, tx *sql.Tx, p storagePositionRow, through time.Time) error {
	if p.lastAssessedDate.Valid && !tehranDay(p.lastAssessedDate.Time).Before(through) {
		return nil
	}
	if tehranDay(p.originalStockedAt).After(through) {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE warehouse_storage_positions
		SET last_assessed_date = $2, updated_at = NOW()
		WHERE id = $1`, p.id, through)
	return err
}
