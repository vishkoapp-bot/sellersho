package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrSupportContextNotFound = errors.New("support reference not found for this account")
	ErrSupportRequestConflict = errors.New("support request key already used with a different payload")
)

type SupportActor struct {
	UserID    string
	Role      string
	AccountID string
}

type SupportReferenceInput struct {
	Kind          string `json:"kind,omitempty"`
	ReferenceType string `json:"referenceType,omitempty"`
	ReferenceID   string `json:"referenceId,omitempty"`
	ReferenceCode string `json:"referenceCode,omitempty"`
	StoreID       string `json:"storeId,omitempty"`
}

type SupportReference struct {
	Category      string `json:"category"`
	ReferenceType string `json:"referenceType,omitempty"`
	ReferenceID   string `json:"referenceId,omitempty"`
	Label         string `json:"referenceLabel,omitempty"`
	accountID     string
	orderNumber   string
}

type supportQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (r *StoreRepository) ResolveSupportReference(ctx context.Context, actor SupportActor, input SupportReferenceInput) (SupportReference, error) {
	return resolveSupportReference(ctx, r.db, actor, input)
}

func resolveSupportReference(ctx context.Context, db supportQueryer, actor SupportActor, input SupportReferenceInput) (SupportReference, error) {
	if actor.UserID == "" || (actor.Role != "seller" && actor.Role != "supplier") ||
		(actor.Role == "supplier" && (actor.AccountID == "" || input.StoreID != "")) {
		return SupportReference{}, ErrStoreInput
	}
	if strings.ContainsRune(input.ReferenceID, '\x00') || strings.ContainsRune(input.StoreID, '\x00') ||
		len(input.ReferenceID) > 255 || len(input.StoreID) > 50 ||
		(input.ReferenceCode != "" && (input.Kind != "financial" || input.ReferenceType != "statement" || actor.Role != "seller")) {
		return SupportReference{}, ErrStoreInput
	}
	ref := SupportReference{Category: input.Kind, ReferenceType: input.ReferenceType, accountID: actor.AccountID}
	if input.Kind == "" || input.Kind == "general" {
		if input.ReferenceID != "" || input.ReferenceType != "" {
			return SupportReference{}, ErrStoreInput
		}
		ref.Category = "general"
		if actor.Role == "seller" {
			ref.accountID = actor.UserID
			if input.StoreID != "" {
				err := db.QueryRowContext(ctx, `SELECT id FROM stores WHERE id=$1 AND owner_user_id=$2`, input.StoreID, actor.UserID).Scan(&ref.accountID)
				return ref, supportReferenceError(err)
			}
		}
		return ref, nil
	}
	if input.ReferenceID == "" {
		return SupportReference{}, ErrStoreInput
	}
	if actor.Role == "supplier" {
		return resolveSupplierSupportReference(ctx, db, ref, input)
	}
	return resolveSellerSupportReference(ctx, db, ref, actor.UserID, input)
}

func resolveSellerSupportReference(ctx context.Context, db supportQueryer, ref SupportReference, userID string, input SupportReferenceInput) (SupportReference, error) {
	var query string
	const ownedStore = ` AND s.owner_user_id=$2 AND ($3='' OR s.id=$3)`
	switch {
	case input.Kind == "order" && input.ReferenceType == "":
		ref.ReferenceType = "order"
		query = `SELECT o.id,o.order_number,s.id,o.order_number FROM orders o JOIN stores s ON s.id=o.store_id WHERE o.id=$1` + ownedStore
	case input.Kind == "shipment" && input.ReferenceType == "order_fulfillment":
		query = `SELECT o.id,o.order_number,s.id,o.order_number FROM orders o JOIN stores s ON s.id=o.store_id WHERE o.id=$1 AND o.tracking_code<>''` + ownedStore
	case input.Kind == "return" && input.ReferenceType == "":
		id, err := uuid.Parse(input.ReferenceID)
		if err != nil {
			return SupportReference{}, ErrSupportContextNotFound
		}
		input.ReferenceID = id.String()
		ref.ReferenceType = "customer_return"
		query = `SELECT cr.id::text,COALESCE(NULLIF(cr.return_code,''),cr.id::text),s.id,o.order_number FROM customer_returns cr
			JOIN orders o ON o.id=cr.order_id JOIN stores s ON s.id=o.store_id WHERE cr.id=$1` + ownedStore
	case input.Kind == "financial" && input.ReferenceType == "payout":
		query = `SELECT p.id,p.id,s.id,'' FROM payout_requests p JOIN stores s ON s.id=p.store_id WHERE p.id=$1 AND p.recipient_type='seller'` + ownedStore
	case input.Kind == "financial" && input.ReferenceType == "statement":
		if _, known := statementReasonLabels[input.ReferenceCode]; !known || input.StoreID == "" {
			return SupportReference{}, ErrStoreInput
		}
		// The posted statement includes several source tables and a single order
		// may carry more than one financial movement. Resolve the typed row using
		// the same inclusion rules as the statement itself.
		ref.ReferenceType = "statement:" + input.ReferenceCode
		err := db.QueryRowContext(ctx, `SELECT f.id,f.id,s.id FROM stores s
			CROSS JOIN LATERAL (`+statementPostedSQL()+`) AS f WHERE s.id=$1 AND s.owner_user_id=$2 AND f.id=$3 AND f.reason=$4 LIMIT 1`,
			input.StoreID, userID, input.ReferenceID, input.ReferenceCode).Scan(&ref.ReferenceID, &ref.Label, &ref.accountID)
		return ref, supportReferenceError(err)
	default:
		return SupportReference{}, ErrStoreInput
	}
	err := db.QueryRowContext(ctx, query, input.ReferenceID, userID, input.StoreID).Scan(&ref.ReferenceID, &ref.Label, &ref.accountID, &ref.orderNumber)
	return ref, supportReferenceError(err)
}

func resolveSupplierSupportReference(ctx context.Context, db supportQueryer, ref SupportReference, input SupportReferenceInput) (SupportReference, error) {
	var query string
	switch {
	case input.Kind == "order" && input.ReferenceType == "":
		ref.ReferenceType = "order"
		err := db.QueryRowContext(ctx, `SELECT o.id,o.order_number,o.order_number FROM orders o WHERE o.id=$2 AND (
			EXISTS (SELECT 1 FROM supplier_shipments ss WHERE ss.order_id=o.id AND ss.supplier_id=$1) OR
			(o.payment_status='paid' AND EXISTS (SELECT 1 `+supplierWarehouseLinesSQL+` WHERE oi.order_id=o.id)))`, ref.accountID, input.ReferenceID).
			Scan(&ref.ReferenceID, &ref.Label, &ref.orderNumber)
		return ref, supportReferenceError(err)
	case input.Kind == "shipment" && input.ReferenceType == "direct_shipment":
		query = `SELECT ss.id,o.order_number,o.order_number FROM supplier_shipments ss JOIN orders o ON o.id=ss.order_id WHERE ss.id=$1 AND ss.supplier_id=$2`
	case input.Kind == "shipment" && input.ReferenceType == "carton":
		id, err := uuid.Parse(input.ReferenceID)
		if err != nil {
			return SupportReference{}, ErrSupportContextNotFound
		}
		input.ReferenceID = id.String()
		query = `SELECT id::text,shipment_code,'' FROM shipments WHERE id=$1 AND supplier_id=$2`
	case input.Kind == "shipment" && input.ReferenceType == "inbound":
		query = `SELECT id,id,'' FROM inbound_shipments WHERE id=$1 AND supplier_id=$2`
	case input.Kind == "return" && input.ReferenceType == "stock_return":
		query = `SELECT id,id,'' FROM supplier_return_requests WHERE id=$1 AND supplier_id=$2`
	case input.Kind == "return" && input.ReferenceType == "":
		id, err := uuid.Parse(input.ReferenceID)
		if err != nil {
			return SupportReference{}, ErrSupportContextNotFound
		}
		input.ReferenceID = id.String()
		ref.ReferenceType = "customer_return"
		query = `SELECT cr.id::text,COALESCE(NULLIF(cr.return_code,''),cr.id::text),COALESCE(o.order_number,'') FROM customer_returns cr
			LEFT JOIN orders o ON o.id=cr.order_id WHERE cr.id=$1 AND EXISTS (
				SELECT 1 FROM return_items ri JOIN order_items oi ON oi.id::text=ri.order_item_id JOIN products p ON p.id=oi.product_id
				WHERE ri.return_id=cr.id AND p.supplier_id=$2)`
	case input.Kind == "financial":
		query = supplierFinancialSupportQuery(input.ReferenceType)
		if query == "" {
			return SupportReference{}, ErrStoreInput
		}
	default:
		return SupportReference{}, ErrStoreInput
	}
	err := db.QueryRowContext(ctx, query, input.ReferenceID, ref.accountID).Scan(&ref.ReferenceID, &ref.Label, &ref.orderNumber)
	return ref, supportReferenceError(err)
}

func supplierFinancialSupportQuery(referenceType string) string {
	switch referenceType {
	case "payout":
		return `SELECT id,id,'' FROM payout_requests WHERE id=$1 AND supplier_id=$2 AND recipient_type='supplier'`
	case "order":
		return `SELECT o.id,o.order_number,o.order_number FROM orders o WHERE o.id=$1 AND o.payment_status='paid'
			AND o.status IN ('delivered','delivered_confirmed') AND EXISTS (
				SELECT 1 FROM order_items oi WHERE oi.order_id=o.id AND ` + supplierOriginAuthority + `=$2)`
	case "storage":
		return `SELECT id,id,'' FROM warehouse_storage_fee_charges WHERE id=$1 AND supplier_id=$2`
	case "adjustment":
		return `SELECT id,id,'' FROM ledger_entries WHERE id=$1 AND target_account='supplier_payable:' || $2 AND target_type='supplier' AND reason LIKE 'سند تعدیل%'`
	case "return_shipping":
		return `SELECT id,id,'' FROM customer_return_shipping_charges WHERE id=$1 AND supplier_id=$2 AND bearer='supplier'`
	case "return_clawback":
		return `SELECT e.id,e.id,o.order_number FROM customer_return_financial_events e JOIN orders o ON o.id=e.order_id
			WHERE e.id=$1 AND e.supplier_id=$2 AND e.component='product_cost' AND o.status<>'cancelled'`
	case "return_event":
		return `SELECT id,id,'' FROM supplier_return_financial_events WHERE id=$1 AND supplier_id=$2`
	default:
		return ""
	}
}

func supportReferenceError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSupportContextNotFound
	}
	return err
}

func (r *StoreRepository) CreateSupportTicket(ctx context.Context, actor SupportActor, input SupportReferenceInput, subject, message string) (string, error) {
	return r.CreateSupportTicketWithKey(ctx, actor, input, subject, message, "")
}

func (r *StoreRepository) CreateSupportTicketWithKey(ctx context.Context, actor SupportActor, input SupportReferenceInput, subject, message, requestKey string) (string, error) {
	subject, message = strings.TrimSpace(subject), strings.TrimSpace(message)
	if subject == "" || message == "" || utf8.RuneCountInString(subject) > 200 || utf8.RuneCountInString(message) > 10000 ||
		strings.ContainsRune(subject, '\x00') || strings.ContainsRune(message, '\x00') {
		return "", ErrStoreInput
	}
	var key uuid.UUID
	if requestKey != "" {
		var err error
		key, err = uuid.Parse(requestKey)
		if err != nil || key == uuid.Nil {
			return "", ErrStoreInput
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	ref, err := resolveSupportReference(ctx, tx, actor, input)
	if err != nil {
		return "", err
	}
	section := ""
	if ref.Category == "financial" {
		section = "finance"
	}
	ticketID := uuid.New()
	if key != uuid.Nil {
		// Keep the same ticket ID for a retried operation while isolating a
		// reused client key across requesters and their accounts.
		name := strings.Join([]string{"sellersho/support_ticket/v1", actor.Role, actor.UserID, ref.accountID, key.String()}, "\x00")
		ticketID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(name))
	}
	id := "ticket-" + ticketID.String()
	insert, err := tx.ExecContext(ctx, `INSERT INTO support_tickets
		(id,requester_user_id,requester_role,requester_account_id,subject,section,category,reference_type,reference_id,reference_label,related_order_number)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,'')) ON CONFLICT (id) DO NOTHING`, id, actor.UserID, actor.Role, ref.accountID, subject, section,
		ref.Category, ref.ReferenceType, ref.ReferenceID, ref.Label, ref.orderNumber)
	if err != nil {
		return "", err
	}
	inserted, err := insert.RowsAffected()
	if err != nil {
		return "", err
	}
	if inserted == 0 {
		var sameRequest bool
		// The insert waits for the winning transaction. Its initial message
		// is committed together with the ticket before this lookup can see it.
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM support_tickets t WHERE t.id=$1 AND t.requester_user_id=$2 AND t.requester_role=$3
			AND t.requester_account_id=$4 AND t.subject=$5 AND t.category=$6 AND t.reference_type=$7 AND t.reference_id=$8
			AND (SELECT m.body FROM support_ticket_messages m WHERE m.ticket_id=t.id AND NOT m.is_internal
				ORDER BY m.created_at,m.id LIMIT 1)=$9
		)`, id, actor.UserID, actor.Role, ref.accountID, subject, ref.Category, ref.ReferenceType, ref.ReferenceID, message).Scan(&sameRequest); err != nil {
			return "", err
		}
		if !sameRequest || key == uuid.Nil {
			return "", ErrSupportRequestConflict
		}
		return id, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO support_ticket_messages(ticket_id,author_user_id,body) VALUES($1,$2,$3)`, id, actor.UserID, message); err != nil {
		return "", err
	}
	return id, tx.Commit()
}
