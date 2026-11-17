package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/drup/platform/internal/domain"
)

var (
	ErrCheckoutConflict  = errors.New("checkout_payload_conflict")
	ErrCheckoutAbandoned = errors.New("checkout_attempt_abandoned")
	ErrCheckoutLegacy    = errors.New("checkout_legacy_attempt_requires_recovery")
	checkoutHashPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type CheckoutAttempt struct {
	AttemptID   string        `json:"attemptId"`
	State       string        `json:"state"`
	OrderID     string        `json:"-"`
	Fingerprint string        `json:"-"`
	Order       *domain.Order `json:"order,omitempty"`
}

func validCheckoutIdentity(key, hash string) bool {
	return len(key) > 0 && len(key) <= 128 && key == strings.TrimSpace(key) && checkoutHashPattern.MatchString(hash)
}

// Registration has no stock, wallet, provider or order side effects. Keep it
// durable before slow quote/address validation; only the order transaction seals it.
func (r *OrderRepository) BeginCheckoutAttempt(ctx context.Context, customer, key, hash string) (*CheckoutAttempt, error) {
	if !validCheckoutIdentity(key, hash) {
		return nil, ErrCheckoutConflict
	}
	var legacy bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM orders o WHERE customer_user_id=$1 AND idempotency_key=$2 AND NOT EXISTS(SELECT 1 FROM checkout_attempts a WHERE a.customer_user_id=$1 AND a.attempt_id=$2))`, customer, key).Scan(&legacy); err != nil {
		return nil, err
	}
	if legacy {
		return nil, ErrCheckoutLegacy
	}
	if _, err := r.db.ExecContext(ctx, `INSERT INTO checkout_attempts(customer_user_id,attempt_id,payload_hash) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, customer, key, hash); err != nil {
		return nil, err
	}
	a, err := r.GetCheckoutAttempt(ctx, customer, key)
	if err != nil {
		return nil, err
	}
	if a.State == "abandoned" {
		return nil, ErrCheckoutAbandoned
	}
	if a.Fingerprint != hash {
		return nil, ErrCheckoutConflict
	}
	return a, nil
}

func (r *OrderRepository) GetCheckoutAttempt(ctx context.Context, customer, key string) (*CheckoutAttempt, error) {
	a := &CheckoutAttempt{AttemptID: key}
	var orderID sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT state,COALESCE(payload_hash,''),order_id FROM checkout_attempts WHERE customer_user_id=$1 AND attempt_id=$2`, customer, key).Scan(&a.State, &a.Fingerprint, &orderID)
	if errors.Is(err, sql.ErrNoRows) {
		// Historical keys can be recovered, but never assert a request fingerprint
		// that was not recorded when that order was created.
		err = r.db.QueryRowContext(ctx, `SELECT id FROM orders WHERE customer_user_id=$1 AND idempotency_key=$2`, customer, key).Scan(&orderID)
		if err == nil {
			a.State = "completed"
		}
	}
	if err != nil {
		return nil, err
	}
	if orderID.Valid {
		a.OrderID = orderID.String
		a.Order, err = r.GetOrderByID(ctx, a.OrderID)
		if err == nil && a.Order == nil {
			err = sql.ErrNoRows
		}
	}
	return a, err
}

func (r *OrderRepository) AbandonCheckoutAttempt(ctx context.Context, customer, key string) (*CheckoutAttempt, error) {
	if len(key) == 0 || len(key) > 128 || key != strings.TrimSpace(key) {
		return nil, ErrCheckoutConflict
	}
	if existing, err := r.GetCheckoutAttempt(ctx, customer, key); err == nil && existing.State == "completed" && existing.Fingerprint == "" {
		return existing, nil
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Insert an irreversible tombstone even before registration. There is no
	// invented fingerprint when the original request has not arrived yet.
	_, err = tx.ExecContext(ctx, `INSERT INTO checkout_attempts(customer_user_id,attempt_id,state) VALUES($1,$2,'abandoned') ON CONFLICT DO NOTHING`, customer, key)
	if err != nil {
		return nil, err
	}
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM checkout_attempts WHERE customer_user_id=$1 AND attempt_id=$2 FOR UPDATE`, customer, key).Scan(&state); err != nil {
		return nil, err
	}
	if state == "open" {
		_, err = tx.ExecContext(ctx, `UPDATE checkout_attempts SET state='abandoned',updated_at=now() WHERE customer_user_id=$1 AND attempt_id=$2`, customer, key)
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetCheckoutAttempt(ctx, customer, key)
}

func (r *OrderRepository) CreateCheckoutOrderWithShipments(ctx context.Context, order domain.Order, shipments []domain.SupplierShipment, hash string) (string, bool, error) {
	ctx, cancel := couponAdmissionContext(ctx)
	defer cancel()
	var id string
	var created bool
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		id, created, err = r.createCheckoutOrderWithShipmentsOnce(ctx, order, shipments, hash)
		if !couponTransactionRetryable(err) || ctx.Err() != nil || attempt == 2 {
			return id, created, err
		}
		if err = couponRetryPause(ctx); err != nil {
			return id, false, err
		}
	}
	return id, created, err
}
func (r *OrderRepository) createCheckoutOrderWithShipmentsOnce(ctx context.Context, order domain.Order, shipments []domain.SupplierShipment, hash string) (string, bool, error) {
	if order.CustomerUserID == nil || order.IdempotencyKey == nil || !validCheckoutIdentity(*order.IdempotencyKey, hash) {
		return "", false, ErrCheckoutConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	var state, stored string
	var existing sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT state,COALESCE(payload_hash,''),order_id FROM checkout_attempts WHERE customer_user_id=$1 AND attempt_id=$2 FOR UPDATE`, *order.CustomerUserID, *order.IdempotencyKey).Scan(&state, &stored, &existing)
	if err != nil {
		return "", false, err
	}
	if stored != hash {
		return "", false, ErrCheckoutConflict
	}
	if state == "abandoned" {
		return "", false, ErrCheckoutAbandoned
	}
	if existing.Valid {
		return existing.String, false, nil
	}
	if err = r.insertOrderWithShipmentsTx(ctx, tx, order, shipments, true); err != nil {
		return "", false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE checkout_attempts SET state='completed',order_id=$3,updated_at=now() WHERE customer_user_id=$1 AND attempt_id=$2`, *order.CustomerUserID, *order.IdempotencyKey, order.ID)
	if err != nil {
		return "", false, fmt.Errorf("seal checkout: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return "", false, err
	}
	notifyOrderPlaced(ctx, r.db, order.ID)
	return order.ID, true, nil
}
