package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/drup/platform/internal/customdomain"
	"github.com/lib/pq"
)

type PendingApproval struct {
	ID              string                 `json:"id" db:"id"`
	EntityType      string                 `json:"entityType" db:"entity_type"`
	TargetID        string                 `json:"targetId" db:"target_id"`
	Title           string                 `json:"title" db:"title"`
	RequestedData   map[string]interface{} `json:"requestedData"`
	RequestedBy     string                 `json:"requestedBy" db:"requested_by"`
	Status          string                 `json:"status" db:"status"` // 'pending' | 'approved' | 'rejected'
	RejectionReason *string                `json:"rejectionReason" db:"rejection_reason"`
	CreatedAt       time.Time              `json:"createdAt" db:"created_at"`
	ReviewedAt      *time.Time             `json:"reviewedAt" db:"reviewed_at"`
}

type AuditLog struct {
	ID        string    `json:"id" db:"id"`
	Timestamp time.Time `json:"timestamp" db:"timestamp"`
	Operator  string    `json:"operator" db:"operator"`
	Action    string    `json:"action" db:"action"`
	Category  string    `json:"category" db:"category"`
	Details   string    `json:"details" db:"details"`
}

type AdminRepository struct {
	db *sql.DB
}

var (
	ErrInvalidApprovalDecision = errors.New("approval status must be approved or rejected")
	ErrApprovalNotPending      = errors.New("approval is not pending")
	ErrApprovalTargetChanged   = errors.New("approval target no longer matches the request")
	ErrStoreSlugTaken          = errors.New("store slug is already taken")
)

func NewAdminRepository(db *sql.DB) *AdminRepository {
	return &AdminRepository{db: db}
}

// GetPendingApprovals returns items from the approval inbox
func (r *AdminRepository) GetPendingApprovals(ctx context.Context, status string) ([]PendingApproval, error) {
	query := `
		SELECT id, entity_type, target_id, title, requested_data, requested_by,
		       status, rejection_reason, created_at, reviewed_at
		FROM pending_approvals
	`
	var rows *sql.Rows
	var err error

	if status != "" && status != "all" {
		query += " WHERE status = $1 ORDER BY created_at DESC"
		rows, err = r.db.QueryContext(ctx, query, status)
	} else {
		query += " ORDER BY created_at DESC"
		rows, err = r.db.QueryContext(ctx, query)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []PendingApproval
	for rows.Next() {
		var item PendingApproval
		var rawData []byte
		if err := rows.Scan(
			&item.ID, &item.EntityType, &item.TargetID, &item.Title, &rawData, &item.RequestedBy,
			&item.Status, &item.RejectionReason, &item.CreatedAt, &item.ReviewedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(rawData, &item.RequestedData)
		list = append(list, item)
	}
	return list, nil
}

// CreatePendingApproval inserts a new item into the approval inbox
func (r *AdminRepository) CreatePendingApproval(ctx context.Context, item PendingApproval) error {
	rawData, err := json.Marshal(item.RequestedData)
	if err != nil {
		return fmt.Errorf("failed to marshal requested_data: %w", err)
	}

	query := `
		INSERT INTO pending_approvals (id, entity_type, target_id, title, requested_data, requested_by, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', NOW())
	`
	_, err = r.db.ExecContext(ctx, query, item.ID, item.EntityType, item.TargetID, item.Title, rawData, item.RequestedBy)
	if err != nil {
		return err
	}
	// EDGE-01 step 4: a new pending custom_domain row becomes the
	// latest approval for the store, so the allow decision can flip
	// approved -> denied until it is reviewed. Other entity types
	// never affect the allow check.
	if item.EntityType == "custom_domain" {
		if v, ok := item.RequestedData["value"].(string); ok && v != "" {
			customdomain.DefaultAllowCache.Invalidate(v)
		}
	}
	return nil
}

// GetPendingApprovalEntityType returns the entity_type of one
// pending_approvals row (sql.ErrNoRows when the id is unknown). The
// approvals inbox uses it to route proposed_product reviews through
// the supplier proposal pipeline instead of the generic status flip.
func (r *AdminRepository) GetPendingApprovalEntityType(ctx context.Context, id string) (string, error) {
	var entityType string
	err := r.db.QueryRowContext(ctx, `SELECT entity_type FROM pending_approvals WHERE id = $1`, id).Scan(&entityType)
	if err != nil {
		return "", err
	}
	return entityType, nil
}

// ReviewPendingApproval updates status of approval item
func (r *AdminRepository) ReviewPendingApproval(ctx context.Context, id, status, reason string) error {
	if status != "approved" && status != "rejected" {
		return ErrInvalidApprovalDecision
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var entity, title, requester, targetID, currentStatus string
		var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT entity_type,title,requested_by,target_id,status,requested_data
		FROM pending_approvals WHERE id=$1 FOR UPDATE`, id).
		Scan(&entity, &title, &requester, &targetID, &currentStatus, &raw)
	if err != nil {
			return err
		}
	now := time.Now()
	var domain string
	switch entity {
	case "proposed_product":
		return ErrApprovalTargetChanged
	case "warranty_property", "store_name", "store_slug":
		if currentStatus != "pending" {
			return ErrApprovalNotPending
		}
		if status == "approved" {
			reason = ""
		}
		if entity == "warranty_property" {
			err = reviewWarrantyProperty(ctx, tx, id, targetID, requester, status, reason, raw, now)
		} else if status == "approved" {
			err = applyStoreChangeApproval(ctx, tx, targetID, requester, entity, raw, now)
			}
		if err != nil {
				return err
			}
	case "custom_domain":
		var requested struct {
			Value string `json:"value"`
			}
		if json.Unmarshal(raw, &requested) == nil {
			domain = requested.Value
		}
	}
	query := `
		UPDATE pending_approvals
		SET status = $1, rejection_reason = NULLIF($2, ''), reviewed_at = $3
		WHERE id = $4
	`
	if _, err := tx.ExecContext(ctx, query, status, reason, now, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Custom domain approvals remain revocable. Invalidate after the
	// commit so subsequent requests pick up the new allow decision.
	if domain != "" {
			customdomain.DefaultAllowCache.Invalidate(domain)
		}
	if status == "rejected" && currentStatus != "rejected" {
		notifyApprovalRejected(ctx, r.db, id, entity, title, requester, reason)
	}
	return nil
}

func reviewWarrantyProperty(ctx context.Context, tx *sql.Tx, approvalID, requestID, supplierID, status, reason string, raw []byte, now time.Time) error {
	var requested struct {
		RequestID string `json:"warranty_request_id"`
	}
	if err := json.Unmarshal(raw, &requested); err != nil {
		return ErrApprovalTargetChanged
	}
	// Earlier approvals can carry the request ID only in their data.
	if requested.RequestID != "" {
		requestID = requested.RequestID
	}
	var currentStatus, name, description string
	err := tx.QueryRowContext(ctx, `SELECT status,proposed_name,description FROM warranty_property_requests
		WHERE id=$1 AND supplier_id=$2 FOR UPDATE`, requestID, supplierID).Scan(&currentStatus, &name, &description)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrApprovalTargetChanged
	}
	if err != nil {
		return err
	}
	if currentStatus != "pending" {
		return ErrApprovalNotPending
	}
	var propertyID string
	if status == "approved" {
		err := tx.QueryRowContext(ctx, `INSERT INTO warranty_properties(id,name,description,status,created_by)
			VALUES($1,$2,$3,'active',$4)
			ON CONFLICT (lower(trim(name))) DO UPDATE SET status='active' RETURNING id`,
			"WPROP-"+approvalID, name, description, supplierID).Scan(&propertyID)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE warranty_property_requests
		SET status=$1, rejection_reason=NULLIF($2,''), approved_property_id=NULLIF($3,''), reviewed_at=$4 WHERE id=$5`,
		status, reason, propertyID, now, requestID)
	return err
}

func applyStoreChangeApproval(ctx context.Context, tx *sql.Tx, storeID, ownerID, kind string, raw []byte, now time.Time) error {
	var requested struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &requested); err != nil {
		return ErrStoreInput
	}
	value, err := normalizeStoreChangeValue(kind, requested.Value)
	if err != nil {
		return err
	}
	query := `UPDATE stores SET name=$1, updated_at=$2 WHERE id=$3 AND owner_user_id::text=$4 RETURNING id`
	if kind == "store_slug" {
		// The storefront resolves case-insensitively, including legacy
		// mixed-case slugs. The unique constraint handles concurrent
		// writes of the same normalized slug.
		var exists bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM stores WHERE lower(slug)=$1 AND id<>$2)`, value, storeID).Scan(&exists)
		if err != nil {
			return err
		}
		if exists {
			return ErrStoreSlugTaken
		}
		query = `UPDATE stores SET slug=$1, updated_at=$2 WHERE id=$3 AND owner_user_id::text=$4 RETURNING id`
	}
	var updatedID string
	err = tx.QueryRowContext(ctx, query, value, now, storeID, ownerID).Scan(&updatedID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrApprovalTargetChanged
	}
	var pgErr *pq.Error
	if kind == "store_slug" && errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrStoreSlugTaken
	}
	return err
}

// GetAuditLogs returns enterprise system audit trails
func (r *AdminRepository) GetAuditLogs(ctx context.Context, category string) ([]AuditLog, error) {
	query := `SELECT id, timestamp, operator, action, category, details FROM audit_logs`
	var rows *sql.Rows
	var err error

	if category != "" && category != "all" {
		query += " WHERE category = $1 ORDER BY timestamp DESC LIMIT 100"
		rows, err = r.db.QueryContext(ctx, query, category)
	} else {
		query += " ORDER BY timestamp DESC LIMIT 100"
		rows, err = r.db.QueryContext(ctx, query)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]AuditLog, 0)
	for rows.Next() {
		var l AuditLog
		if err := rows.Scan(&l.ID, &l.Timestamp, &l.Operator, &l.Action, &l.Category, &l.Details); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// InsertAuditLog inserts a new enterprise audit entry
func (r *AdminRepository) InsertAuditLog(ctx context.Context, log AuditLog) error {
	query := `
		INSERT INTO audit_logs (id, timestamp, operator, action, category, details)
		VALUES ($1, NOW(), $2, $3, $4, $5)
	`
	_, err := r.db.ExecContext(ctx, query, log.ID, log.Operator, log.Action, log.Category, log.Details)
	return err
}

type AdminUser struct {
	ID        string    `json:"id"`
	Mobile    string    `json:"mobile"`
	FullName  string    `json:"fullName"`
	Role      string    `json:"role"`
	Username  string    `json:"username"`
	IsActive  bool      `json:"isActive"`
	CreatedAt time.Time `json:"createdAt"`
}

type AdminStore struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	Slug                string    `json:"slug"`
	OwnerName           string    `json:"ownerName"`
	ProfitMarginPercent float64   `json:"profitMarginPercent"`
	Status              string    `json:"status"`
	RejectionReason     *string   `json:"rejectionReason,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
}

type SuspiciousTransaction struct {
	ID          string    `json:"id"`
	OrderNumber string    `json:"orderNumber"`
	Amount      int64     `json:"amount"`
	FlagReason  string    `json:"flagReason"`
	RiskScore   int       `json:"riskScore"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (r *AdminRepository) GetUsers(ctx context.Context, role, status string) ([]AdminUser, error) {
	query := `
		SELECT id, mobile, full_name, role, COALESCE(username, ''), is_active, created_at
		FROM users
		WHERE 1=1
	`
	args := []interface{}{}
	argIdx := 1
	if role != "" {
		query += fmt.Sprintf(" AND role = $%d", argIdx)
		args = append(args, role)
		argIdx++
	}
	if status == "active" {
		query += " AND is_active = TRUE"
	} else if status == "blocked" || status == "inactive" {
		query += " AND is_active = FALSE"
	}
	query += " ORDER BY created_at DESC LIMIT 100"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []AdminUser
	for rows.Next() {
		var u AdminUser
		if err := rows.Scan(&u.ID, &u.Mobile, &u.FullName, &u.Role, &u.Username, &u.IsActive, &u.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, u)
	}
	return list, nil
}

func (r *AdminRepository) SetUserActiveStatus(ctx context.Context, userID string, isActive bool) error {
	_, err := r.db.ExecContext(ctx, `UPDATE users SET is_active = $1, updated_at = NOW() WHERE id::text = $2 OR username = $2`, isActive, userID)
	return err
}

func (r *AdminRepository) GetPendingStores(ctx context.Context) ([]AdminStore, error) {
	query := `
		SELECT id, name, slug, owner_name, profit_margin_percent, status, rejection_reason, created_at
		FROM stores
		WHERE status = 'pending'
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []AdminStore
	for rows.Next() {
		var s AdminStore
		if err := rows.Scan(&s.ID, &s.Name, &s.Slug, &s.OwnerName, &s.ProfitMarginPercent, &s.Status, &s.RejectionReason, &s.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, nil
}

func (r *AdminRepository) ReviewStore(ctx context.Context, storeID, status, rejectionReason string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE stores
		SET status = $1, rejection_reason = NULLIF($2, ''), updated_at = NOW()
		WHERE id = $3
	`, status, rejectionReason, storeID)
	return err
}

func (r *AdminRepository) LockStoreMargin(ctx context.Context, storeID string, lockedMargin float64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE stores
		SET is_margin_locked_by_admin = TRUE, custom_store_margin_override = $1, updated_at = NOW()
		WHERE id = $2
	`, lockedMargin, storeID)
	return err
}

func (r *AdminRepository) GetSuspiciousTransactions(ctx context.Context) ([]SuspiciousTransaction, error) {
	query := `
		SELECT o.id, o.order_number, o.total_amount,
		       CASE
		           WHEN o.total_amount > 50000000 THEN 'مبلغ نامتعارف بالاتر از ۵۰ میلیون تومان'
		           WHEN o.status = 'cancelled' THEN 'سفارش لغو شده با تراکنش مشکوک'
		           ELSE 'توالی سفارش سریع در زمان کوتاه'
		       END as flag_reason,
		       85 as risk_score,
		       o.created_at
		FROM orders o
		ORDER BY o.total_amount DESC
		LIMIT 10
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []SuspiciousTransaction
	for rows.Next() {
		var t SuspiciousTransaction
		if err := rows.Scan(&t.ID, &t.OrderNumber, &t.Amount, &t.FlagReason, &t.RiskScore, &t.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}
