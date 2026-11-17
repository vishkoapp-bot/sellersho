package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/drup/platform/internal/pricing"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

var ErrParcelChargeInput = errors.New("invalid parcel charge input")
var ErrParcelChargeState = errors.New("parcel charge state requires reconciliation")

type ParcelChargeCorrectionInput struct {
	ExpectedAmount int64  `json:"expectedAmount"`
	FinalAmount    int64  `json:"finalAmount"`
	Source         string `json:"source"`
	Reference      string `json:"reference"`
	Reason         string `json:"reason"`
}
type ParcelChargeReceipt struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	ParcelID       string    `json:"parcelId"`
	ReturnID       string    `json:"returnId"`
	SupplierID     string    `json:"supplierId"`
	PaymentMethod  string    `json:"paymentMethod"`
	ExpectedAmount *int64    `json:"expectedAmount,omitempty"`
	FinalAmount    int64     `json:"finalAmount"`
	AmountWasNull  bool      `json:"amountWasNull"`
	Source         string    `json:"source"`
	Reference      string    `json:"reference"`
	Reason         string    `json:"reason"`
	Actor          string    `json:"actor"`
	Key            string    `json:"key"`
	EventID        *string   `json:"eventId,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

const chargeReceiptColumns = `id,kind,parcel_id,return_request_id,supplier_id,payment_method,expected_amount,final_amount,amount_was_null,source,reference,reason,actor_user_id,client_key,event_id,created_at`

func scanChargeReceipt(row interface{ Scan(...interface{}) error }) (*ParcelChargeReceipt, error) {
	v := new(ParcelChargeReceipt)
	err := row.Scan(&v.ID, &v.Kind, &v.ParcelID, &v.ReturnID, &v.SupplierID, &v.PaymentMethod, &v.ExpectedAmount, &v.FinalAmount, &v.AmountWasNull, &v.Source, &v.Reference, &v.Reason, &v.Actor, &v.Key, &v.EventID, &v.CreatedAt)
	return v, err
}
func validChargeCommand(actor, key string) bool {
	return strings.TrimSpace(actor) != "" && len(key) > 0 && len(key) <= 128
}
func validCorrection(v ParcelChargeCorrectionInput) bool {
	return v.ExpectedAmount >= 0 && v.ExpectedAmount <= pricing.MaxTransportMoney && v.FinalAmount >= 0 && v.FinalAmount <= pricing.MaxTransportMoney && v.ExpectedAmount != v.FinalAmount && (v.Source == "tariff" || v.Source == "api" || v.Source == "manual") && strings.TrimSpace(v.Reference) != "" && utf8.RuneCountInString(v.Reference) <= 150 && strings.TrimSpace(v.Reason) != ""
}

type lockedParcelCharge struct {
	supplier, parentStatus, method, status, source, reference string
	amount                                                    sql.NullInt64
}

func (r *SupplierRepository) lockParcelCharge(ctx context.Context, tx *sql.Tx, returnID, parcelID string) (*lockedParcelCharge, error) {
	var discovered string
	if err := tx.QueryRowContext(ctx, `SELECT supplier_id FROM supplier_return_requests WHERE id=$1`, returnID).Scan(&discovered); err != nil {
		return nil, err
	}
	var set TransactionLockSet
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := LockAccountsTx(bounded, tx, &set, FinancialAccount{Type: "supplier", ID: discovered}); err != nil {
		if bounded.Err() != nil {
			return nil, bounded.Err()
		}
		return nil, err
	}
	if err := LockTransactionRowsTx(bounded, tx, &set, TransactionRowLock{Table: "supplier_return_requests", ID: returnID}, TransactionRowLock{Table: "supplier_return_parcels", ID: parcelID}); err != nil {
		if bounded.Err() != nil {
			return nil, bounded.Err()
		}
		return nil, err
	}
	p := new(lockedParcelCharge)
	err := tx.QueryRowContext(ctx, `SELECT r.supplier_id,r.status,p.payment_method,p.status,p.final_charge,COALESCE(p.final_charge_source,''),COALESCE(p.final_charge_reference,'') FROM supplier_return_requests r JOIN supplier_return_parcels p ON p.return_request_id=r.id WHERE r.id=$1 AND p.id=$2`, returnID, parcelID).Scan(&p.supplier, &p.parentStatus, &p.method, &p.status, &p.amount, &p.source, &p.reference)
	if err != nil {
		return nil, err
	}
	if p.supplier != discovered {
		return nil, ErrFinanceAccountChanged
	}
	return p, nil
}
func chargeConflict(err error) error {
	var p *pq.Error
	if errors.As(err, &p) && p.Code == "23505" {
		return ErrFinanceCommandConflict
	}
	return err
}
func claimChargeKey(ctx context.Context, tx *sql.Tx, key, id string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO parcel_charge_keys(client_key,receipt_id) VALUES($1,$2) ON CONFLICT(client_key) DO NOTHING`, key, id)
	if err != nil {
		return err
	}
	var stored string
	if err = tx.QueryRowContext(ctx, `SELECT receipt_id FROM parcel_charge_keys WHERE client_key=$1`, key).Scan(&stored); err != nil {
		return err
	}
	if stored != id {
		return ErrFinanceCommandConflict
	}
	return nil
}
func chargeKeyReceipt(ctx context.Context, tx *sql.Tx, key string) (*ParcelChargeReceipt, error) {
	return scanChargeReceipt(tx.QueryRowContext(ctx, `SELECT `+chargeReceiptColumns+` FROM parcel_charge_receipts WHERE id=(SELECT receipt_id FROM parcel_charge_keys WHERE client_key=$1)`, key))
}
func matchesCharge(p *lockedParcelCharge, v *ParcelChargeReceipt, ret, parcel string) bool {
	return v.ParcelID == parcel && v.ReturnID == ret && v.SupplierID == p.supplier && v.PaymentMethod == p.method && v.FinalAmount == p.amount.Int64 && v.AmountWasNull == !p.amount.Valid && v.Source == p.source && v.Reference == p.reference
}
func appendChargeReceipt(ctx context.Context, tx *sql.Tx, v *ParcelChargeReceipt) error {
	err := tx.QueryRowContext(ctx, `INSERT INTO parcel_charge_receipts(id,kind,parcel_id,return_request_id,supplier_id,payment_method,expected_amount,final_amount,amount_was_null,source,reference,reason,actor_user_id,client_key,event_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING created_at`, v.ID, v.Kind, v.ParcelID, v.ReturnID, v.SupplierID, v.PaymentMethod, v.ExpectedAmount, v.FinalAmount, v.AmountWasNull, v.Source, v.Reference, v.Reason, v.Actor, v.Key, v.EventID).Scan(&v.CreatedAt)
	if err != nil {
		return chargeConflict(err)
	}
	return claimChargeKey(ctx, tx, v.Key, v.ID)
}

// The event is the existing canonical signed supplier projection; ledger legs
// retain the established supplier target shape to avoid counting them twice.
func postParcelCharge(ctx context.Context, tx *sql.Tx, v *ParcelChargeReceipt, delta int64) error {
	operation, direction := "debit", "debit"
	if delta < 0 {
		operation, direction, delta = "reversal", "credit", -delta
	}
	ledgerID, eventID := "RET-LED-"+uuid.NewString(), "RET-FIN-"+uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO ledger_entries(id,date,target_account,target_type,type,amount,reason,operator,related_order_or_dispute) VALUES($1,now(),$2,'supplier',$3,$4,$5,$6,$7)`, ledgerID, v.SupplierID, direction, delta, "carrier parcel charge "+v.ParcelID, v.Actor, v.ReturnID); err != nil {
		return err
	}
	identity := "return-parcel-charge:" + v.ParcelID
	if v.Kind == "adjustment" {
		identity = "return-parcel-adjustment:" + v.ID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO supplier_return_financial_events(id,return_request_id,parcel_id,supplier_id,operation,amount,payment_method,ledger_entry_id,idempotency_key,reason,actor_user_id,event_identity,charge_component) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, eventID, v.ReturnID, v.ParcelID, v.SupplierID, operation, delta, v.PaymentMethod, ledgerID, v.Key, v.Reason, v.Actor, identity, v.Kind); err != nil {
		return chargeConflict(err)
	}
	v.EventID = &eventID
	return nil
}
func (r *SupplierRepository) ConfirmReturnParcelCharge(ctx context.Context, returnID, parcelID, actor, key string) error {
	key = strings.TrimSpace(key)
	if !validChargeCommand(actor, key) {
		return ErrParcelChargeInput
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := r.lockParcelCharge(ctx, tx, returnID, parcelID)
	if err != nil {
		return err
	}
	keyed, keyErr := chargeKeyReceipt(ctx, tx, key)
	if keyErr != nil && keyErr != sql.ErrNoRows {
		return keyErr
	}
	if keyErr == nil && (keyed.Kind != "original" || keyed.ParcelID != parcelID || keyed.ReturnID != returnID) {
		return ErrFinanceCommandConflict
	}
	original, err := scanChargeReceipt(tx.QueryRowContext(ctx, `SELECT `+chargeReceiptColumns+` FROM parcel_charge_receipts WHERE parcel_id=$1 AND kind='original'`, parcelID))
	if err == nil {
		latest, e := scanChargeReceipt(tx.QueryRowContext(ctx, `SELECT `+chargeReceiptColumns+` FROM parcel_charge_receipts WHERE parcel_id=$1 ORDER BY sequence DESC LIMIT 1`, parcelID))
		if e != nil {
			return e
		}
		if original.SupplierID != p.supplier || original.PaymentMethod != p.method || !matchesCharge(p, latest, returnID, parcelID) {
			return ErrFinanceCommandConflict
		}
		if e = claimChargeKey(ctx, tx, key, original.ID); e != nil {
			return e
		}
		return tx.Commit()
	}
	if err != sql.ErrNoRows {
		return err
	}
	if keyErr == nil {
		return ErrFinanceCommandConflict
	}
	if p.status != "draft" || p.parentStatus == "cancelled" || p.parentStatus == "rejected" {
		return ErrParcelChargeState
	}
	amount := p.amount.Int64
	if amount < 0 || amount > pricing.MaxTransportMoney || (p.method == "sender_pays" && amount == 0) || (p.method != "sender_pays" && p.method != "receiver_pays") {
		return ErrParcelChargeState
	}
	v := &ParcelChargeReceipt{ID: "PCR-" + uuid.NewString(), Kind: "original", ParcelID: parcelID, ReturnID: returnID, SupplierID: p.supplier, PaymentMethod: p.method, FinalAmount: amount, AmountWasNull: !p.amount.Valid, Source: p.source, Reference: p.reference, Reason: "original carrier charge", Actor: actor, Key: key}
	if p.method == "sender_pays" {
		if err = postParcelCharge(ctx, tx, v, amount); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE supplier_return_parcels SET status='confirmed',confirmed_by=$1,updated_at=now() WHERE id=$2 AND return_request_id=$3 AND status='draft'`, actor, parcelID, returnID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrParcelChargeState
	}
	if err = appendChargeReceipt(ctx, tx, v); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if p.method == "sender_pays" {
		notifySupplierParcelCharge(ctx, r.db, returnID, parcelID, p.supplier, amount)
	}
	return nil
}
func (r *SupplierRepository) CorrectReturnParcelCharge(ctx context.Context, returnID, parcelID, actor, key string, input ParcelChargeCorrectionInput) (*ParcelChargeReceipt, error) {
	key = strings.TrimSpace(key)
	if !validChargeCommand(actor, key) || !validCorrection(input) {
		return nil, ErrParcelChargeInput
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := r.lockParcelCharge(ctx, tx, returnID, parcelID)
	if err != nil {
		return nil, err
	}
	prior, err := chargeKeyReceipt(ctx, tx, key)
	if err == nil {
		if prior.Kind != "adjustment" || prior.ParcelID != parcelID || prior.ReturnID != returnID || prior.SupplierID != p.supplier || prior.PaymentMethod != p.method || prior.ExpectedAmount == nil || *prior.ExpectedAmount != input.ExpectedAmount || prior.FinalAmount != input.FinalAmount || prior.Source != input.Source || prior.Reference != input.Reference || prior.Reason != input.Reason {
			return nil, ErrFinanceCommandConflict
		}
		return prior, tx.Commit()
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	if p.method != "sender_pays" || p.status == "draft" || p.status == "cancelled" || p.parentStatus == "cancelled" || p.parentStatus == "rejected" {
		return nil, ErrParcelChargeState
	}
	original, err := scanChargeReceipt(tx.QueryRowContext(ctx, `SELECT `+chargeReceiptColumns+` FROM parcel_charge_receipts WHERE parcel_id=$1 AND kind='original'`, parcelID))
	if err == sql.ErrNoRows {
		return nil, ErrParcelChargeState
	}
	if err != nil {
		return nil, err
	}
	latest, err := scanChargeReceipt(tx.QueryRowContext(ctx, `SELECT `+chargeReceiptColumns+` FROM parcel_charge_receipts WHERE parcel_id=$1 ORDER BY sequence DESC LIMIT 1`, parcelID))
	if err != nil {
		return nil, err
	}
	if original.EventID == nil || original.PaymentMethod != p.method || original.SupplierID != p.supplier || !matchesCharge(p, latest, returnID, parcelID) || input.ExpectedAmount != p.amount.Int64 {
		return nil, ErrFinanceCommandConflict
	}
	v := &ParcelChargeReceipt{ID: "PCA-" + uuid.NewString(), Kind: "adjustment", ParcelID: parcelID, ReturnID: returnID, SupplierID: p.supplier, PaymentMethod: p.method, ExpectedAmount: &input.ExpectedAmount, FinalAmount: input.FinalAmount, Source: input.Source, Reference: input.Reference, Reason: input.Reason, Actor: actor, Key: key}
	if err = postParcelCharge(ctx, tx, v, input.FinalAmount-input.ExpectedAmount); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE supplier_return_parcels SET final_charge=$1,final_charge_source=$2,final_charge_reference=$3,updated_at=now() WHERE id=$4 AND return_request_id=$5 AND final_charge=$6`, input.FinalAmount, input.Source, input.Reference, parcelID, returnID, input.ExpectedAmount)
	if err != nil {
		return nil, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, ErrFinanceCommandConflict
	}
	if err = appendChargeReceipt(ctx, tx, v); err != nil {
		return nil, err
	}
	return v, tx.Commit()
}
