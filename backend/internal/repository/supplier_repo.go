package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

type SupplierRepository struct {
	db *sql.DB
}

// FindUserByMobile resolves an existing supplier owner/member without trusting a client-supplied supplier ID.
func (r *SupplierRepository) FindUserByMobile(ctx context.Context, mobile string) (*domain.User, error) {
	var user domain.User
	err := r.db.QueryRowContext(ctx, `
		SELECT u.id, u.mobile, u.full_name, u.role, u.is_active, COALESCE(u.username,''), u.created_at, u.updated_at
		FROM users u
		WHERE u.mobile = $1
		  AND (EXISTS (SELECT 1 FROM suppliers s WHERE s.owner_user_id = u.id)
		       OR EXISTS (SELECT 1 FROM supplier_members sm WHERE sm.user_id = u.id AND sm.is_active = TRUE))
	`, mobile).Scan(&user.ID, &user.Mobile, &user.FullName, &user.Role, &user.IsActive, &user.Username, &user.CreatedAt, &user.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

var SupplierRolePermissions = map[string][]string{
	"manager":    {"supplier.manage", "products.manage", "inventory.manage", "orders.manage", "finance.view", "finance.manage", "marketing.manage", "support.manage"},
	"products":   {"products.manage"},
	"operations": {"inventory.manage", "orders.manage"},
	"finance":    {"finance.view", "finance.manage"},
	"orders":     {"orders.manage"},
	"marketing":  {"marketing.manage"},
}

func (r *SupplierRepository) GetSupplierAccess(ctx context.Context, userID string) (string, []string, bool, error) {
	var supplierID, role string
	var permissionsJSON []byte
	var active bool
	// Fail-closed: a blocked member user (users.is_active = FALSE) keeps no
	// access even when the supplier_members row is still marked active.
	err := r.db.QueryRowContext(ctx, `
		SELECT sm.supplier_id, sm.role_key, sm.permissions, sm.is_active AND u.is_active
		FROM supplier_members sm
		JOIN users u ON u.id = sm.user_id
		WHERE sm.user_id = $1`, userID).Scan(&supplierID, &role, &permissionsJSON, &active)
	if err == nil {
		if !active {
			return "", nil, false, nil
		}
		// A blocked supplier business shuts off its whole team: the owner
		// account being inactive denies member access too.
		if ownerActive, oerr := r.supplierOwnerIsActive(ctx, supplierID); oerr != nil || !ownerActive {
			return "", nil, false, oerr
		}
		// Gap 12: a SUSPENDED supplier business shuts off its whole team
		// too (approval_status = 'suspended' on the suppliers row). The
		// members' own accounts and the owner's customer account are
		// untouched — only panel access ends.
		if suspended, serr := r.supplierIsSuspended(ctx, supplierID); serr != nil || suspended {
			return "", nil, false, serr
		}
		var permissions []string
		if jsonErr := json.Unmarshal(permissionsJSON, &permissions); jsonErr != nil {
			return "", nil, false, jsonErr
		}
		return supplierID, permissions, true, nil
	}
	if err != sql.ErrNoRows {
		return "", nil, false, err
	}
	// Owner path: the owner's own users.is_active flag is the block switch.
	// A blocked owner gets no access (previously this returned active=true
	// unconditionally, making the admin block toggle decorative).
	err = r.db.QueryRowContext(ctx, `
		SELECT s.id FROM suppliers s
		JOIN users u ON u.id = s.owner_user_id
		WHERE s.owner_user_id = $1 AND u.is_active = TRUE
		  AND s.approval_status <> 'suspended'`, userID).Scan(&supplierID)
	if err == sql.ErrNoRows {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	return supplierID, SupplierRolePermissions["manager"], true, nil
}

// supplierIsSuspended reports whether the supplier business is in the
// 'suspended' approval state (gap 12). Unknown suppliers report false:
// suspension is an explicit admin state, never an inference.
func (r *SupplierRepository) supplierIsSuspended(ctx context.Context, supplierID string) (bool, error) {
	var suspended bool
	err := r.db.QueryRowContext(ctx, `
		SELECT approval_status = 'suspended' FROM suppliers WHERE id = $1`, supplierID).Scan(&suspended)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return suspended, nil
}

// supplierOwnerIsActive reports whether the supplier's owner account is
// active. Unknown suppliers fail closed (false, nil) — callers deny access.
func (r *SupplierRepository) supplierOwnerIsActive(ctx context.Context, supplierID string) (bool, error) {
	var active bool
	err := r.db.QueryRowContext(ctx, `
		SELECT u.is_active FROM suppliers s
		JOIN users u ON u.id = s.owner_user_id
		WHERE s.id = $1`, supplierID).Scan(&active)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return active, nil
}

func (r *SupplierRepository) ListMembers(ctx context.Context, supplierID string) ([]domain.SupplierMember, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT sm.id, sm.supplier_id, sm.user_id, u.full_name, COALESCE(u.username, ''), u.mobile, sm.role_key, sm.permissions, sm.is_active FROM supplier_members sm JOIN users u ON u.id = sm.user_id WHERE sm.supplier_id = $1 ORDER BY sm.created_at DESC`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.SupplierMember{}
	for rows.Next() {
		var member domain.SupplierMember
		var raw []byte
		if err := rows.Scan(&member.ID, &member.SupplierID, &member.UserID, &member.FullName, &member.Username, &member.Mobile, &member.RoleKey, &raw, &member.IsActive); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &member.Permissions); err != nil {
			return nil, err
		}
		result = append(result, member)
	}
	return result, rows.Err()
}

func (r *SupplierRepository) CreateMember(ctx context.Context, supplierID, invitedBy, fullName, username, mobile, password, roleKey string) (domain.SupplierMember, error) {
	permissions, ok := SupplierRolePermissions[roleKey]
	if !ok {
		return domain.SupplierMember{}, fmt.Errorf("unsupported supplier role")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return domain.SupplierMember{}, err
	}
	var member domain.SupplierMember
	rawPermissions, _ := json.Marshal(permissions)
	// GAP 16: grant the 'supplier' capability in the same statement that
	// creates the member. AuthMiddleware refreshes capabilities from
	// user_capabilities on every request, so without this row a member
	// created after the last server start held no active capability
	// until the startup backfill (BackfillOwnershipCapabilities) ran
	// again. The row mirrors the backfill's exactly (status defaults to
	// 'active', source 'supplier_membership', source_id = supplier id);
	// ON CONFLICT DO NOTHING keeps an existing grant (e.g. an owner who
	// is also a member elsewhere) untouched, as the backfill does.
	err = r.db.QueryRowContext(ctx, `WITH new_user AS (INSERT INTO users (mobile, full_name, role, is_active, username, password_hash) VALUES ($1, $2, 'supplier', TRUE, $3, $4) RETURNING id), new_member AS (INSERT INTO supplier_members (supplier_id, user_id, role_key, permissions, invited_by) SELECT $5, id, $6, $7, $8 FROM new_user RETURNING id, supplier_id, user_id, role_key, permissions, is_active), new_capability AS (INSERT INTO user_capabilities (user_id, capability, source_type, source_id) SELECT user_id, 'supplier', 'supplier_membership', supplier_id FROM new_member ON CONFLICT (user_id, capability) DO NOTHING RETURNING user_id) SELECT id, supplier_id, user_id, role_key, permissions, is_active FROM new_member`, mobile, fullName, username, string(hash), supplierID, roleKey, rawPermissions, invitedBy).Scan(&member.ID, &member.SupplierID, &member.UserID, &member.RoleKey, &rawPermissions, &member.IsActive)
	if err != nil {
		return domain.SupplierMember{}, err
	}
	member.FullName, member.Username, member.Mobile, member.Permissions = fullName, username, mobile, permissions
	return member, nil
}

func (r *SupplierRepository) SetMemberActive(ctx context.Context, supplierID, memberID string, active bool) error {
	result, err := r.db.ExecContext(ctx, `UPDATE supplier_members SET is_active = $1, updated_at = NOW() WHERE id = $2 AND supplier_id = $3`, active, memberID, supplierID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *SupplierRepository) UpdateMemberRole(ctx context.Context, supplierID, memberID, roleKey string) error {
	permissions, ok := SupplierRolePermissions[roleKey]
	if !ok {
		return fmt.Errorf("unsupported supplier role")
	}
	raw, err := json.Marshal(permissions)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE supplier_members SET role_key = $1, permissions = $2, updated_at = NOW() WHERE id = $3 AND supplier_id = $4`, roleKey, raw, memberID, supplierID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func NewSupplierRepository(db *sql.DB) *SupplierRepository {
	return &SupplierRepository{db: db}
}

func (r *SupplierRepository) OwnsSupplierResource(ctx context.Context, supplierID, path string) bool {
	if strings.HasPrefix(path, "/api/v1/supplier/orders/") {
		id := strings.Split(strings.TrimPrefix(path, "/api/v1/supplier/orders/"), "/")[0]
		var owned bool
		err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM supplier_shipments WHERE (id = $1 OR order_id = $1) AND supplier_id = $2)`, id, supplierID).Scan(&owned)
		return err == nil && owned
	}
	var table, id string
	for prefix, candidate := range map[string]string{
		"/api/v1/supplier/products/":      "products",
		"/api/v1/supplier/inbounds/":      "inbound_shipments",
		"/api/v1/supplier/discrepancies/": "qc_discrepancies",
		"/api/v1/supplier/campaigns/":     "supplier_campaigns",
		"/api/v1/supplier/shipments/":     "supplier_shipments",
	} {
		if strings.HasPrefix(path, prefix) {
			id = strings.Split(strings.TrimPrefix(path, prefix), "/")[0]
			if id == "" || id == "create" || id == "pending" || id == "propose" || id == "proposals" {
				return true
			}
			table = candidate
			break
		}
	}
	if table == "" {
		return true
	}
	var owned bool
	err := r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" WHERE id = $1 AND supplier_id = $2)", id, supplierID).Scan(&owned)
	return err == nil && owned
}

type QCDiscrepancy struct {
	ID               string     `json:"id" db:"id"`
	InboundID        string     `json:"inbound_id" db:"inbound_id"`
	SupplierID       string     `json:"supplier_id" db:"supplier_id"`
	ProductID        string     `json:"product_id" db:"product_id"`
	ProductTitle     string     `json:"product_title" db:"product_title"`
	ExpectedQuantity int        `json:"expected_quantity" db:"expected_quantity"`
	ReceivedQuantity int        `json:"received_quantity" db:"received_quantity"`
	DefectCount      int        `json:"defect_count" db:"defect_count"`
	Reason           string     `json:"reason" db:"reason"`
	Status           string     `json:"status" db:"status"`
	SupplierResponse *string    `json:"supplier_response" db:"supplier_response"`
	ResolutionOffer  *string    `json:"resolution_offer" db:"resolution_offer"`
	Attachments      []string   `json:"attachments"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	ResolvedAt       *time.Time `json:"resolved_at" db:"resolved_at"`
}

type CampaignStats struct {
	CampaignID  string  `json:"campaign_id"`
	Impressions int     `json:"impressions"`
	Clicks      int     `json:"clicks"`
	SpentBudget int64   `json:"spent_budget"`
	CTR         float64 `json:"ctr"`
}

type SupplierSalesReport struct {
	TotalRevenue int64 `json:"total_revenue"`
	ItemsSold    int   `json:"items_sold"`
	OrdersCount  int   `json:"orders_count"`
	AverageOrder int64 `json:"average_order"`
}

// RegisterSupplier creates both a user account and an associated supplier entity
func (r *SupplierRepository) RegisterSupplier(ctx context.Context, companyName, contactPerson, username, password, phone, email, warehouseCity string) (*domain.User, *domain.Supplier, error) {
	if warehouseCity == "" {
		warehouseCity = "تهران"
	}
	if contactPerson == "" {
		contactPerson = companyName
	}

	mobile := phone
	if !strings.HasPrefix(mobile, "09") || len(mobile) != 11 {
		mobile = fmt.Sprintf("098%08d", time.Now().UnixNano()%100000000)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to hash password: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	userID := uuid.New().String()
	now := time.Now()

	// GAP 19a: the registration payload's email is persisted on the user
	// row (users.email, migration 202611050004); it was previously
	// decoded by the handler and silently discarded. Stored trimmed,
	// empty as NULL; no format validation exists in the codebase for
	// stored emails, so none is invented here.
	email = strings.TrimSpace(email)
	queryUser := `
		INSERT INTO users (id, mobile, full_name, role, is_active, username, password_hash, email, created_at, updated_at)
		VALUES ($1, $2, $3, 'supplier', FALSE, $4, $5, NULLIF($6, ''), $7, $7)
		RETURNING id, mobile, full_name, role, is_active, username, COALESCE(email, ''), created_at, updated_at
	`
	var u domain.User
	err = tx.QueryRowContext(ctx, queryUser, userID, mobile, contactPerson, username, string(hashedPassword), email, now).Scan(
		&u.ID, &u.Mobile, &u.FullName, &u.Role, &u.IsActive, &u.Username, &u.Email, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to insert supplier user: %w", err)
	}

	supplierID := fmt.Sprintf("sup-%s", username)
	querySupplier := `
		INSERT INTO suppliers (id, owner_user_id, name, logo_url, rating, total_products, delivery_score, warehouse_city, description, badge, approval_status, signed_contract_version, created_at)
		VALUES ($1, $2, $3, $4, 5.0, 0, 100, $5, $6, 'تأمین‌کننده جدید', 'pending_approval',
		        (SELECT COALESCE(supplier_contract_version, 2) FROM platform_settings WHERE id = 1), $7)
		RETURNING id, owner_user_id, name, logo_url, rating, total_products, delivery_score, warehouse_city, description, badge, approval_status, COALESCE(signed_contract_version, 1), created_at
	`
	var s domain.Supplier
	var ownerUID sql.NullString
	err = tx.QueryRowContext(ctx, querySupplier, supplierID, userID, companyName, "", warehouseCity, companyName, now).Scan(
		&s.ID, &ownerUID, &s.Name, &s.LogoURL, &s.Rating, &s.TotalProducts, &s.DeliveryScore, &s.WarehouseCity, &s.Description, &s.Badge, &s.ApprovalStatus, &s.SignedContractVersion, &s.CreatedAt,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to insert supplier record: %w", err)
	}
	if ownerUID.Valid {
		s.OwnerUserID = &ownerUID.String
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	return &u, &s, nil
}

// Approval workflow (20261019): new supplier registrations start as
// pending_approval. Admin must approve before the account becomes active.

// ListPendingSuppliers returns suppliers awaiting admin approval.
func (r *SupplierRepository) ListPendingSuppliers(ctx context.Context) ([]domain.Supplier, error) {
	query := `
		SELECT s.id, s.owner_user_id, s.name, COALESCE(s.logo_url, ''), s.rating, s.total_products,
		       s.delivery_score, s.warehouse_city, COALESCE(s.description, ''), COALESCE(s.badge, ''),
		       COALESCE(s.supports_direct_shipping, FALSE), s.approval_status, s.created_at,
		       u.mobile, u.full_name
		FROM suppliers s
		JOIN users u ON u.id = s.owner_user_id
		WHERE s.approval_status = 'pending_approval'
		ORDER BY s.created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Supplier
	for rows.Next() {
		var s domain.Supplier
		var ownerUID sql.NullString
		var mobile, fullName string
		if err := rows.Scan(&s.ID, &ownerUID, &s.Name, &s.LogoURL, &s.Rating, &s.TotalProducts,
			&s.DeliveryScore, &s.WarehouseCity, &s.Description, &s.Badge,
			&s.SupportsDirectShipping, &s.ApprovalStatus, &s.CreatedAt, &mobile, &fullName); err != nil {
			return nil, err
		}
		if ownerUID.Valid {
			s.OwnerUserID = &ownerUID.String
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ApproveSupplier activates a pending supplier: flips approval_status,
// activates the user account. Returns the user ID for capability granting.
func (r *SupplierRepository) ApproveSupplier(ctx context.Context, supplierID, approvedBy string) (string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var userID string
	err = tx.QueryRowContext(ctx, `
		UPDATE suppliers SET approval_status = 'approved', approved_by = $2, approved_at = NOW()
		WHERE id = $1 AND approval_status = 'pending_approval'
		RETURNING owner_user_id
	`, supplierID, approvedBy).Scan(&userID)
	if err != nil {
		return "", fmt.Errorf("supplier not found or already processed: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE users SET is_active = TRUE, updated_at = NOW() WHERE id = $1`, userID); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return userID, nil
}

// RejectSupplier marks a pending supplier as rejected with a reason.
func (r *SupplierRepository) RejectSupplier(ctx context.Context, supplierID, rejectedBy, reason string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE suppliers SET approval_status = 'rejected', approved_by = $2, approved_at = NOW(), rejection_reason = $3
		WHERE id = $1 AND approval_status = 'pending_approval'
	`, supplierID, rejectedBy, reason)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("supplier not found or already processed")
	}
	return nil
}

// --- Supplier suspension (gap 12, migration 202611050007) -------------
//
// Suspension is the BUSINESS-level stop, distinct from blocking the
// owner's user account (users.is_active, a person-level switch under
// which products keep selling). A suspended supplier loses panel
// access for owner and team (GetSupplierAccess), disappears from the
// public catalog and PDP, cannot take new orders, and cannot request
// new settlements — while its balance keeps accruing on read and its
// already-pending payout requests stay admin-reviewable.
//
// ErrSupplierStateConflict reports a suspend/reactivate attempted from
// a state that does not allow the transition (pending, rejected,
// already suspended, not suspended, or unknown id). Handlers map it
// to 409.
var ErrSupplierStateConflict = errors.New("supplier not found or not in a state that allows this transition")

// ErrCentralSupplierProtected reports a suspend/reactivate attempted
// on the platform's own central supplier record. The central record
// is identified the way the rest of the tree identifies it: it is
// the OWNERLESS suppliers row (owner_user_id IS NULL) — the platform
// card in the admin records surface. Handlers map it to 409.
var ErrCentralSupplierProtected = errors.New("the central supplier record cannot be suspended or reactivated")

// ErrSupplierSuspended reports a settlement request by a suspended
// supplier. Handlers map it to 409.
var ErrSupplierSuspended = errors.New("supplier is suspended")

// isCentralSupplierRecord reports whether id names the platform's own
// central supplier record (the ownerless suppliers row).
func (r *SupplierRepository) isCentralSupplierRecord(ctx context.Context, supplierID string) (bool, error) {
	var ownerless bool
	err := r.db.QueryRowContext(ctx, `
		SELECT owner_user_id IS NULL FROM suppliers WHERE id = $1`, supplierID).Scan(&ownerless)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return ownerless, nil
}

// insertAdminSupplierAuditTx appends one audit_logs row for an admin
// action on a supplier, in the shape of insertSupplierAuditTx but
// with the acting admin as the operator.
func insertAdminSupplierAuditTx(ctx context.Context, tx *sql.Tx, adminID, action string, details map[string]interface{}) error {
	payload, _ := json.Marshal(details)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO audit_logs (id, timestamp, operator, action, category, details)
		VALUES ($1, NOW(), $2, $3, 'supplier', $4)
	`, "LOG-"+uuid.NewString(), "admin:"+adminID, action, string(payload))
	return err
}

// SuspendSupplier moves an APPROVED supplier to 'suspended',
// recording who suspended it and why. users.is_active is deliberately
// NOT touched: suspension and user-block are orthogonal switches.
func (r *SupplierRepository) SuspendSupplier(ctx context.Context, supplierID, adminID, reason string) error {
	central, err := r.isCentralSupplierRecord(ctx, supplierID)
	if err != nil {
		return err
	}
	if central {
		return ErrCentralSupplierProtected
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
		UPDATE suppliers
		SET approval_status = 'suspended', suspended_at = NOW(), suspended_by = $2, suspension_reason = $3
		WHERE id = $1 AND approval_status = 'approved'
	`, supplierID, adminID, reason)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSupplierStateConflict
	}
	if err := insertAdminSupplierAuditTx(ctx, tx, adminID, "supplier_suspend", map[string]interface{}{
		"supplier_id": supplierID, "reason": reason,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// ReactivateSupplier returns a SUSPENDED supplier to 'approved' and
// clears the suspension bookkeeping. It never touches
// users.is_active: a separately blocked owner stays blocked.
func (r *SupplierRepository) ReactivateSupplier(ctx context.Context, supplierID, adminID string) error {
	central, err := r.isCentralSupplierRecord(ctx, supplierID)
	if err != nil {
		return err
	}
	if central {
		return ErrCentralSupplierProtected
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
		UPDATE suppliers
		SET approval_status = 'approved', suspended_at = NULL, suspended_by = NULL, suspension_reason = NULL
		WHERE id = $1 AND approval_status = 'suspended'
	`, supplierID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrSupplierStateConflict
	}
	if err := insertAdminSupplierAuditTx(ctx, tx, adminID, "supplier_reactivate", map[string]interface{}{
		"supplier_id": supplierID,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// RejectProposal performs the full-proposal rejection of one
// proposed_product pending approval: exactly the writes the generic
// approvals inbox makes when it rejects (ReviewPendingApproval —
// status 'rejected', rejection_reason, reviewed_at), scoped to a
// product proposal that is still pending. The pending guard mirrors
// ApproveProposal's atomic backstop: an already-approved proposal
// can never be flipped back to rejected, and a double rejection
// reports ErrProposalNotPending instead of rewriting the row.
func (r *SupplierRepository) RejectProposal(ctx context.Context, proposalID, reason string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE pending_approvals
		SET status = 'rejected', rejection_reason = NULLIF($2, ''), reviewed_at = NOW()
		WHERE id = $1 AND entity_type = 'proposed_product' AND status = 'pending'
	`, proposalID, reason)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrProposalNotPending
	}
	return nil
}

// IsPendingApproval reports whether the user's supplier account is still
// waiting for admin approval (registered but never activated).
func (r *SupplierRepository) IsPendingApproval(ctx context.Context, userID string) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM suppliers
		WHERE owner_user_id = $1 AND approval_status = 'pending_approval'
	`, userID).Scan(&n)
	return n > 0, err
}

// GetSupplierByUserID retrieves the supplier profile for a given user ID
func (r *SupplierRepository) GetSupplierByUserID(ctx context.Context, userID string) (*domain.Supplier, error) {
	query := `
		SELECT id, owner_user_id, name, COALESCE(logo_url, ''), rating, total_products, delivery_score, warehouse_city, COALESCE(description, ''), COALESCE(badge, ''), created_at, COALESCE(supports_direct_shipping, FALSE),
		       default_markup_type, default_markup_value, COALESCE(signed_contract_version, 1),
		       approval_status, approved_by, approved_at, rejection_reason, tapin_shop_id,
		       COALESCE(warehouse_city_confirmed, FALSE), warehouse_city_source
		FROM suppliers
		WHERE owner_user_id = $1
	`
	row := r.db.QueryRowContext(ctx, query, userID)
	var s domain.Supplier
	var ownerUID sql.NullString
	var approvedBy, rejectionReason, tapinShopID, warehouseCitySource sql.NullString
	var approvedAt sql.NullTime
	if err := row.Scan(&s.ID, &ownerUID, &s.Name, &s.LogoURL, &s.Rating, &s.TotalProducts, &s.DeliveryScore, &s.WarehouseCity, &s.Description, &s.Badge, &s.CreatedAt, &s.SupportsDirectShipping, &s.DefaultMarkupType, &s.DefaultMarkupValue, &s.SignedContractVersion, &s.ApprovalStatus, &approvedBy, &approvedAt, &rejectionReason, &tapinShopID, &s.WarehouseCityConfirmed, &warehouseCitySource); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if ownerUID.Valid {
		s.OwnerUserID = &ownerUID.String
	}
	if approvedBy.Valid {
		s.ApprovedBy = &approvedBy.String
	}
	if approvedAt.Valid {
		s.ApprovedAt = &approvedAt.Time
	}
	if rejectionReason.Valid {
		s.RejectionReason = &rejectionReason.String
	}
	if tapinShopID.Valid {
		s.TapinShopID = &tapinShopID.String
	}
	if warehouseCitySource.Valid {
		s.WarehouseCitySource = &warehouseCitySource.String
	}
	return &s, nil
}

// GetSupplierContractVersion returns the platform's current supplier
// contract version (used to gate the supplier re-confirmation flow).
func (r *SupplierRepository) GetSupplierContractVersion(ctx context.Context) (int, error) {
	var v int
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(supplier_contract_version, 2) FROM platform_settings WHERE id = 1`).Scan(&v)
	if err != nil {
		return 2, err
	}
	return v, nil
}

// UpdateSignedContractVersion records the platform supplier contract version
// the supplier accepted (registration sets it implicitly; this is used by the
// re-confirmation flow when the platform publishes a newer contract).
func (r *SupplierRepository) UpdateSignedContractVersion(ctx context.Context, supplierID string, version int) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE suppliers SET signed_contract_version = $2 WHERE id = $1
	`, supplierID, version)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetSuppliersByIDs fetches many suppliers in ONE query (P2-9 N+1 fix):
// WHERE id = ANY($1), returned keyed by supplier ID.
func (r *SupplierRepository) GetSuppliersByIDs(ctx context.Context, ids []string) (map[string]*domain.Supplier, error) {
	out := make(map[string]*domain.Supplier, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, owner_user_id, name, COALESCE(logo_url, ''), rating, total_products, delivery_score, warehouse_city, COALESCE(description, ''), COALESCE(badge, ''), created_at, COALESCE(supports_direct_shipping, FALSE),
		       default_markup_type, default_markup_value, COALESCE(signed_contract_version, 1),
		       approval_status, approved_by, approved_at, rejection_reason, tapin_shop_id,
		       COALESCE(warehouse_city_confirmed, FALSE), warehouse_city_source
		FROM suppliers
		WHERE id = ANY($1)
	`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s domain.Supplier
		var ownerUID sql.NullString
		var approvedBy, rejectionReason, tapinShopID, warehouseCitySource sql.NullString
		var approvedAt sql.NullTime
		if err := rows.Scan(&s.ID, &ownerUID, &s.Name, &s.LogoURL, &s.Rating, &s.TotalProducts, &s.DeliveryScore, &s.WarehouseCity, &s.Description, &s.Badge, &s.CreatedAt, &s.SupportsDirectShipping, &s.DefaultMarkupType, &s.DefaultMarkupValue, &s.SignedContractVersion, &s.ApprovalStatus, &approvedBy, &approvedAt, &rejectionReason, &tapinShopID, &s.WarehouseCityConfirmed, &warehouseCitySource); err != nil {
			return nil, err
		}
		if ownerUID.Valid {
			s.OwnerUserID = &ownerUID.String
		}
		if approvedBy.Valid {
			s.ApprovedBy = &approvedBy.String
		}
		if approvedAt.Valid {
			s.ApprovedAt = &approvedAt.Time
		}
		if rejectionReason.Valid {
			s.RejectionReason = &rejectionReason.String
		}
		if tapinShopID.Valid {
			s.TapinShopID = &tapinShopID.String
		}
		if warehouseCitySource.Valid {
			s.WarehouseCitySource = &warehouseCitySource.String
		}
		sup := s
		out[sup.ID] = &sup
	}
	return out, rows.Err()
}

// GetSupplierByID retrieves supplier by primary key
func (r *SupplierRepository) GetSupplierByID(ctx context.Context, id string) (*domain.Supplier, error) {
	query := `
		SELECT id, owner_user_id, name, COALESCE(logo_url, ''), rating, total_products, delivery_score, warehouse_city, COALESCE(description, ''), COALESCE(badge, ''), created_at, COALESCE(supports_direct_shipping, FALSE),
		       default_markup_type, default_markup_value, COALESCE(signed_contract_version, 1),
		       approval_status, approved_by, approved_at, rejection_reason, tapin_shop_id, tapin_token,
		       COALESCE(warehouse_city_confirmed, FALSE), warehouse_city_source
		FROM suppliers
		WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, id)
	var s domain.Supplier
	var ownerUID sql.NullString
	var approvedBy, rejectionReason, tapinShopID, tapinToken, warehouseCitySource sql.NullString
	var approvedAt sql.NullTime
	if err := row.Scan(&s.ID, &ownerUID, &s.Name, &s.LogoURL, &s.Rating, &s.TotalProducts, &s.DeliveryScore, &s.WarehouseCity, &s.Description, &s.Badge, &s.CreatedAt, &s.SupportsDirectShipping, &s.DefaultMarkupType, &s.DefaultMarkupValue, &s.SignedContractVersion, &s.ApprovalStatus, &approvedBy, &approvedAt, &rejectionReason, &tapinShopID, &tapinToken, &s.WarehouseCityConfirmed, &warehouseCitySource); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if ownerUID.Valid {
		s.OwnerUserID = &ownerUID.String
	}
	if approvedBy.Valid {
		s.ApprovedBy = &approvedBy.String
	}
	if approvedAt.Valid {
		s.ApprovedAt = &approvedAt.Time
	}
	if rejectionReason.Valid {
		s.RejectionReason = &rejectionReason.String
	}
	if tapinShopID.Valid {
		s.TapinShopID = &tapinShopID.String
	}
	if tapinToken.Valid {
		s.TapinToken = tapinToken.String
	}
	if warehouseCitySource.Valid {
		s.WarehouseCitySource = &warehouseCitySource.String
	}
	return &s, nil
}

// SupplierMarkupSummary is the slim admin-facing supplier row for the pricing
// engine tab: identity + current agreement markup (P2-11 — the public product
// listing strips supplier identity, so the admin tab needs its own source).
type SupplierMarkupSummary struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	DefaultMarkupType  *string  `json:"defaultMarkupType"`
	DefaultMarkupValue *float64 `json:"defaultMarkupValue"`
}

// ListSuppliersForAdmin returns every supplier with its current agreement
// markup for the admin pricing engine tab.
func (r *SupplierRepository) ListSuppliersForAdmin(ctx context.Context) ([]SupplierMarkupSummary, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, default_markup_type, default_markup_value
		FROM suppliers
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierMarkupSummary{}
	for rows.Next() {
		var s SupplierMarkupSummary
		if err := rows.Scan(&s.ID, &s.Name, &s.DefaultMarkupType, &s.DefaultMarkupValue); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CreateDirectProduct inserts a verified or supplier-direct product into products catalog
func (r *SupplierRepository) CreateDirectProduct(ctx context.Context, p domain.Product) error {
	if p.ID == "" {
		p.ID = fmt.Sprintf("prod-%d", time.Now().UnixNano()/1e6)
	}
	if p.FulfillmentMethod == "" {
		p.FulfillmentMethod = "supplier_direct"
	}
	if p.OriginalPrice == 0 {
		p.OriginalPrice = p.SupplierBaseCost + 500000
	}
	if p.PlatformMargin == 0 {
		p.PlatformMargin = 100000
	}
	if p.WarehouseName == "" {
		p.WarehouseName = "انبار اختصاصی تأمین‌کننده"
	}
	if p.PreparationDays == 0 {
		p.PreparationDays = 1
	}

	specsJSON, _ := json.Marshal(p.Specs)
	if len(specsJSON) == 0 {
		specsJSON = []byte("{}")
	}

	query := `
		INSERT INTO products (
			id, title, slug, category_slug, supplier_id, original_price,
			supplier_base_cost, platform_margin, is_original, stock,
			warehouse_name, preparation_days, fulfillment_method, image_url, description, specs, brand, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, NOW())
		ON CONFLICT (id) DO UPDATE SET
			stock = EXCLUDED.stock,
			supplier_base_cost = EXCLUDED.supplier_base_cost,
			image_url = EXCLUDED.image_url,
			description = EXCLUDED.description,
			specs = EXCLUDED.specs,
			brand = EXCLUDED.brand,
			fulfillment_method = EXCLUDED.fulfillment_method
	`
	_, err := r.db.ExecContext(ctx, query,
		p.ID, p.Title, p.Slug, p.CategorySlug, p.SupplierID, p.OriginalPrice,
		p.SupplierBaseCost, p.PlatformMargin, p.IsOriginal, p.Stock,
		p.WarehouseName, p.PreparationDays, p.FulfillmentMethod, p.ImageURL,
		p.Description, specsJSON, strings.TrimSpace(p.Brand),
	)
	return err
}

// ProposeProduct saves a new product proposal to pending_approvals
// FIX 52 — live 409 challenge path: find another supplier's active product
// with the same normalized title. Live's exact duplicate rule is unproven;
// title match (excluding the challenger's own products) is the
// conservative rule: the candidate's form sends no usable barcode.
type DuplicateProduct struct {
	ID         string
	Title      string
	SupplierID string
}

func (r *SupplierRepository) FindDuplicateProduct(ctx context.Context, supplierID, title string) (*DuplicateProduct, error) {
	norm := strings.TrimSpace(title)
	if norm == "" {
		return nil, nil
	}
	var dup DuplicateProduct
	err := r.db.QueryRowContext(ctx, `
		SELECT id, title, COALESCE(supplier_id, '')
		FROM products
		WHERE supplier_id <> $1
		  AND is_active
		  AND lower(regexp_replace(btrim(title), '\s+', ' ', 'g')) = lower(regexp_replace(btrim($2), '\s+', ' ', 'g'))
		ORDER BY created_at ASC
		LIMIT 1
	`, supplierID, norm).Scan(&dup.ID, &dup.Title, &dup.SupplierID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &dup, nil
}

// CreateExclusivityChallenge inserts a challenger's request. The holder is
// the matched product's supplier; the full proposal is stored as the
// challenger payload so reviewers see exactly what was proposed.
func (r *SupplierRepository) CreateExclusivityChallenge(ctx context.Context, supplierID, branch string, proposedPrice int64, proposal map[string]interface{}, holder *DuplicateProduct, variantName, variantAttrsJSON string, variantNames []string) (string, error) {
	id := "chal-" + uuid.NewString()[:12]
	title, _ := proposal["title"].(string)
	brand, _ := proposal["brand"].(string)
	catSlug, _ := proposal["categorySlug"].(string)
	barcode, _ := proposal["barcode"].(string)
	identityKey := "title:" + strings.ToLower(regexp.MustCompile(`\s+`).ReplaceAllString(strings.TrimSpace(title), " "))
	if strings.TrimSpace(barcode) != "" {
		identityKey = "barcode:" + strings.TrimSpace(barcode)
	}
	proposalJSON, _ := json.Marshal(proposal)
	variantNamesJSON, _ := json.Marshal(variantNames)
	if variantNames == nil {
		variantNamesJSON = []byte("[]")
	}
	holderTitle := ""
	holderSupplier := ""
	holderProduct := ""
	if holder != nil {
		holderTitle = holder.Title
		holderSupplier = holder.SupplierID
		holderProduct = holder.ID
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO exclusivity_challenges
		  (id, supplier_id, branch, proposed_price, product_title, product_brand,
		   product_category, barcode, product_identity_key, holder_kind, holder_id,
		   holder_title, holder_supplier_id, holder_product_id, variant_name,
		   variant_attributes, variant_names, challenger_payload, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'product', $10, $11, $12, $13, $14, $15, $16, $17, 'pending')
	`, id, supplierID, branch, proposedPrice, title, brand, catSlug, barcode,
		identityKey, holderProduct, holderTitle, holderSupplier, holderProduct,
		variantName, variantAttrsJSON, variantNamesJSON, proposalJSON)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (r *SupplierRepository) ProposeProduct(ctx context.Context, supplierID string, data map[string]interface{}) (string, error) {
	data["supplierId"], data["supplier_id"] = supplierID, supplierID
	proposalID := fmt.Sprintf("prop-%d", time.Now().UnixNano()/1e6)
	title, _ := data["title"].(string)
	if title == "" {
		title = "پیشنهاد محصول جدید"
	}

	rawData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	query := `
		INSERT INTO pending_approvals (id, entity_type, target_id, title, requested_data, requested_by, status, created_at)
		VALUES ($1, 'proposed_product', $1, $2, $3, $4, 'pending', NOW())
	`
	_, err = r.db.ExecContext(ctx, query, proposalID, title, rawData, supplierID)
	if err != nil {
		return "", err
	}
	return proposalID, nil
}

// GetSupplierProposals returns proposals submitted by this supplier.
// Fix 8: besides new-product proposals this now includes the supplier's
// catalog-mutation proposals (gallery updates, variant creations) so their
// review state is visible in the supplier panel.
func (r *SupplierRepository) GetSupplierProposals(ctx context.Context, supplierID string) ([]map[string]interface{}, error) {
	query := `
		SELECT id, entity_type, target_id, title, requested_data, requested_by, status, rejection_reason, created_at, reviewed_at
		FROM pending_approvals
		WHERE entity_type IN ('proposed_product', 'product_gallery_update', 'product_variant_create')
		  AND requested_by = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var id, entityType, targetID, title, requestedBy, status string
		var rawData []byte
		var rejectionReason sql.NullString
		var createdAt time.Time
		var reviewedAt *time.Time

		if err := rows.Scan(&id, &entityType, &targetID, &title, &rawData, &requestedBy, &status, &rejectionReason, &createdAt, &reviewedAt); err != nil {
			return nil, err
		}

		var parsedData map[string]interface{}
		_ = json.Unmarshal(rawData, &parsedData)
		if parsedData == nil {
			parsedData = make(map[string]interface{})
		}
		parsedData["proposal_id"] = id
		parsedData["id"] = id
		parsedData["entity_type"] = entityType
		parsedData["title"] = title
		parsedData["status"] = status
		parsedData["created_at"] = createdAt
		if rejectionReason.Valid {
			parsedData["rejection_reason"] = rejectionReason.String
		}
		if reviewedAt != nil {
			parsedData["reviewed_at"] = reviewedAt
		}
		result = append(result, parsedData)
	}
	return result, nil
}

// GetAllProposals lists every proposed_product approval for the admin queue.
// Group A audit fix: the admin endpoint previously reused the supplier-scoped
// query, which filtered on an empty supplier id and could never return rows.
func (r *SupplierRepository) GetAllProposals(ctx context.Context) ([]map[string]interface{}, error) {
	query := `
		SELECT id, target_id, title, requested_data, requested_by, status, rejection_reason, created_at, reviewed_at
		FROM pending_approvals
		WHERE entity_type = 'proposed_product'
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var id, targetID, title, requestedBy, status string
		var rawData []byte
		var rejectionReason sql.NullString
		var createdAt time.Time
		var reviewedAt *time.Time

		if err := rows.Scan(&id, &targetID, &title, &rawData, &requestedBy, &status, &rejectionReason, &createdAt, &reviewedAt); err != nil {
			return nil, err
		}

		var parsedData map[string]interface{}
		_ = json.Unmarshal(rawData, &parsedData)
		if parsedData == nil {
			parsedData = make(map[string]interface{})
		}
		parsedData["proposal_id"] = id
		parsedData["id"] = id
		parsedData["status"] = status
		parsedData["created_at"] = createdAt
		if rejectionReason.Valid {
			parsedData["rejection_reason"] = rejectionReason.String
		}
		if reviewedAt != nil {
			parsedData["reviewed_at"] = reviewedAt
		}

		result = append(result, parsedData)
	}
	if result == nil {
		result = []map[string]interface{}{}
	}
	return result, nil
}

// ApproveProposal approves a proposal and inserts it into products catalog
func (r *SupplierRepository) ApproveProposal(ctx context.Context, proposalID, notes string) (string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	querySelect := `
		SELECT title, requested_data, requested_by
		FROM pending_approvals
		WHERE id = $1 AND entity_type = 'proposed_product'
	`
	var title, requestedBy string
	var rawData []byte
	err = tx.QueryRowContext(ctx, querySelect, proposalID).Scan(&title, &rawData, &requestedBy)
	if err != nil {
		return "", err
	}

	var data map[string]interface{}
	_ = json.Unmarshal(rawData, &data)

	productID := fmt.Sprintf("prod-%d", time.Now().UnixNano()/1e6)
	if pID, ok := data["product_id"].(string); ok && pID != "" {
		productID = pID
	}
	slug := fmt.Sprintf("product-%d", time.Now().UnixNano()/1e6)
	categorySlug, _ := data["category"].(string)
	if categorySlug == "" {
		categorySlug, _ = data["categorySlug"].(string)
	}
	if categorySlug == "" {
		categorySlug = "digital"
	}
	desc, _ := data["description"].(string)
	warehouse, _ := data["warehouse"].(string)
	if warehouse == "" {
		warehouse = "انبار مرکزی تهران"
	}
	warranty, _ := data["supplier_warranty"].(string)
	if warranty == "" {
		warranty, _ = data["warranty"].(string)
	}
	if warranty == "" {
		warranty = "۱۸ ماه گارانتی شرکتی"
	}

	baseCost := int64(3500000)
	if c, ok := data["supplier_base_cost"].(float64); ok && c > 0 {
		baseCost = int64(c)
	} else if c, ok := data["supplierBaseCost"].(float64); ok && c > 0 {
		baseCost = int64(c)
	}
	platformMargin := int64(120000)
	if m, ok := data["suggested_platform_margin"].(float64); ok && m > 0 {
		platformMargin = int64(m)
	}
	stock := 50
	if s, ok := data["stock"].(float64); ok && s >= 0 {
		stock = int(s)
	}
	prepDays := 2
	if p, ok := data["preparation_days"].(float64); ok && p > 0 {
		prepDays = int(p)
	} else if p, ok := data["preparationDays"].(float64); ok && p > 0 {
		prepDays = int(p)
	}

	imgURL := ""
	var gallery []string
	if imgs, ok := data["images"].([]interface{}); ok {
		for _, im := range imgs {
			if u, ok := im.(string); ok && strings.TrimSpace(u) != "" {
				gallery = append(gallery, strings.TrimSpace(u))
			}
		}
	}
	if len(gallery) > 0 {
		imgURL = gallery[0]
	}
	if imgURL == "" {
		if u, ok := data["imageUrl"].(string); ok && u != "" {
			imgURL = u
		} else if u, ok := data["image_url"].(string); ok && u != "" {
			imgURL = u
		} else if u, ok := data["image"].(string); ok && u != "" {
			imgURL = u
		}
	}
	if imgURL != "" {
		imgURL = strings.TrimSpace(imgURL)
		// A lone image_url still counts as a 1-image gallery.
		if len(gallery) == 0 {
			gallery = []string{imgURL}
		}
	}

	// Fix 8: the barcode collected + validated in the dossier was never
	// persisted. products.barcode is the canonical column now (migration
	// 202610180001); BarcodeExists reads it for the pre-approval check.
	barcode := ""
	if b, ok := data["barcode"].(string); ok {
		barcode = strings.TrimSpace(b)
	}

	// Brand (store-gaps spec §۴.۱, migration 202610200003): supplier-typed
	// free text from the proposal payload, persisted verbatim (trimmed) —
	// never silently truncated. Over-length input is rejected here, at
	// finalize time, like the other field validations.
	brand := ""
	if b, ok := data["brand"].(string); ok {
		brand = strings.TrimSpace(b)
	}
	if len([]rune(brand)) > 100 {
		return "", fmt.Errorf("brand exceeds 100 characters")
	}

	isOriginal := true
	if orig, ok := data["isOriginal"].(bool); ok {
		isOriginal = orig
	} else if orig, ok := data["is_original"].(bool); ok {
		isOriginal = orig
	}
	fulfillmentMethod := "warehouse"
	if method, ok := data["fulfillmentMethod"].(string); ok && method == "supplier_direct" {
		fulfillmentMethod = method
	}

	// Gap 8: write the proposal's measurements into the product's
	// measurement columns at birth (the same columns
	// RecordProductMeasurement later overwrites: length_mm, width_mm,
	// height_mm, weight_grams). The propose handler now refuses
	// proposals without all four, but approval still tolerates older
	// pending proposals that predate the gate — absent/invalid values
	// insert NULL and the product behaves exactly like a pre-fix
	// approval (warehouse measures later). measurement_status is
	// deliberately NOT written: it stays 'unverified', because the
	// warehouse verification remains the trust step that unlocks
	// putaway. The ON CONFLICT clause below intentionally does not
	// update these columns either — a re-approval must never clobber
	// warehouse-verified measurements.
	var measLength, measWidth, measHeight, measWeight sql.NullInt64
	if v, ok := payloadMeasurementValue(data, "lengthMm", "length_mm"); ok {
		measLength = sql.NullInt64{Int64: int64(v), Valid: true}
	}
	if v, ok := payloadMeasurementValue(data, "widthMm", "width_mm"); ok {
		measWidth = sql.NullInt64{Int64: int64(v), Valid: true}
	}
	if v, ok := payloadMeasurementValue(data, "heightMm", "height_mm"); ok {
		measHeight = sql.NullInt64{Int64: int64(v), Valid: true}
	}
	if v, ok := payloadMeasurementValue(data, "weightGrams", "weight_grams"); ok {
		measWeight = sql.NullInt64{Int64: int64(v), Valid: true}
	}

	specsBytes, _ := json.Marshal(data["specs"])

	queryProduct := `
		INSERT INTO products (id, title, slug, category_slug, supplier_id, original_price, supplier_base_cost, platform_margin, is_original, stock, image_url, description, warehouse_name, preparation_days, supplier_warranty, specs, fulfillment_method, barcode, brand, length_mm, width_mm, height_mm, weight_grams, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, NULLIF($18, ''), $19, $20, $21, $22, $23, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET stock = EXCLUDED.stock, supplier_base_cost = EXCLUDED.supplier_base_cost, image_url = EXCLUDED.image_url, description = EXCLUDED.description, specs = EXCLUDED.specs, brand = EXCLUDED.brand,
		                               barcode = CASE WHEN EXCLUDED.barcode IS NOT NULL THEN EXCLUDED.barcode ELSE products.barcode END
	`
	originalPrice := int64(float64(baseCost) * 1.25)
	// Fix 9: distinguish a first approval (sync the full gallery) from a
	// re-approval (leave the supplier-curated gallery untouched).
	var existedBefore bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id = $1)`, productID).Scan(&existedBefore); err != nil {
		return "", fmt.Errorf("failed to check product existence: %w", err)
	}
	_, err = tx.ExecContext(ctx, queryProduct, productID, title, slug, categorySlug, requestedBy, originalPrice, baseCost, platformMargin, isOriginal, stock, imgURL, desc, warehouse, prepDays, warranty, specsBytes, fulfillmentMethod, barcode, brand, measLength, measWidth, measHeight, measWeight)
	if err != nil {
		return "", fmt.Errorf("failed to insert product: %w", err)
	}

	// Fix 9: persist the proposal's full gallery into product_images so it
	// is no longer dropped during promotion (only images[0] reached
	// products.image_url). Only on first approval — product_images has no
	// provenance column, so on re-approval we must not clobber rows the
	// supplier may have added/curated since.
	if !existedBefore && len(gallery) > 0 {
		for i, gURL := range gallery {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO product_images (product_id, image_url, sort_order)
				VALUES ($1, $2, $3)
				ON CONFLICT DO NOTHING`, productID, gURL, i); err != nil {
				return "", fmt.Errorf("failed to persist gallery image %d: %w", i, err)
			}
		}
	}

	// --- Audit C6: write product_niches on approval. ---
	// Downstream engines filter the catalog by niche (catalog_repo.go) and
	// apply niche-tier pricing overrides (internal/pricing) via product_niches;
	// without rows here every approved product was invisible to both.
	//
	// Mapping choice (documented): explicit nicheIds in the proposal payload
	// (the supplier's/dossier's stated intent) take precedence and are
	// validated against the niches table. Otherwise we derive from the
	// product's category_slug: every niche carrying that category_slug gets a
	// row. The mapping is 1:N (e.g. category 'digital' spans 5 niches), so the
	// fallback deliberately over-assigns rather than leaving the product
	// absent from niche filters — assignments remain admin-correctable.
	var nicheIDs []string
	for _, key := range []string{"nicheIds", "niche_ids"} {
		if rawNiches, ok := data[key].([]interface{}); ok {
			for _, n := range rawNiches {
				if s, ok := n.(string); ok && s != "" {
					nicheIDs = append(nicheIDs, s)
				}
			}
		}
		if len(nicheIDs) > 0 {
			break
		}
	}
	for _, nid := range nicheIDs {
		// Unknown niche ids are skipped silently (WHERE n.id) rather than
		// violating the FK; re-approvals are idempotent via ON CONFLICT.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO product_niches (product_id, niche_id)
			SELECT $1, n.id FROM niches n WHERE n.id = $2
			ON CONFLICT (product_id, niche_id) DO NOTHING`, productID, nid); err != nil {
			return "", fmt.Errorf("failed to link product to niche %s: %w", nid, err)
		}
	}
	var linkedNiches int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM product_niches WHERE product_id = $1`, productID).Scan(&linkedNiches); err != nil {
		return "", err
	}
	if linkedNiches == 0 {
		// No explicit (or no valid) niche ids: fall back to the category mapping.
		nicheRows, err := tx.QueryContext(ctx,
			`SELECT id FROM niches WHERE category_slug = $1`, categorySlug)
		if err != nil {
			return "", fmt.Errorf("failed to resolve niches for category %s: %w", categorySlug, err)
		}
		var fallback []string
		for nicheRows.Next() {
			var nid string
			if err := nicheRows.Scan(&nid); err != nil {
				nicheRows.Close()
				return "", err
			}
			fallback = append(fallback, nid)
		}
		if err := nicheRows.Err(); err != nil {
			nicheRows.Close()
			return "", err
		}
		nicheRows.Close()
		for _, nid := range fallback {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO product_niches (product_id, niche_id)
				VALUES ($1, $2)
				ON CONFLICT (product_id, niche_id) DO NOTHING`, productID, nid); err != nil {
				return "", fmt.Errorf("failed to link product to niche %s: %w", nid, err)
			}
		}
	}

	// --- Gap 9a: persist the proposal's Digikala category attributes. ---
	// product_attribute_values (migration 202610280012) is the product ↔
	// dk-value link the faceted filters and the PDP attribute table are
	// built on, but its approval write path was never implemented, so
	// approval silently dropped categoryAttributes. First approval only
	// (the Fix 9 no-clobber rule). The platform is single-select per
	// attribute (Oct 29 rule), so only the FIRST proposed value per
	// attribute is stored. Validation is the join, per the migration
	// contract: a (attribute, value) pair that does not exist in
	// dk_attribute_values inserts nothing. categoryAttributes.colorId /
	// brandId are NOT persisted here: no product link table exists for
	// them (the brand pick already lands as the free-text brand above).
	if !existedBefore {
		for attrID, valueID := range proposalAttributePicks(data) {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO product_attribute_values (product_id, dk_attribute_id, dk_value_id)
				SELECT $1, v.dk_attr_id, v.dk_value_id
				FROM dk_attribute_values v
				WHERE v.dk_attr_id = $2 AND v.dk_value_id = $3
				ON CONFLICT (product_id, dk_value_id) DO NOTHING`, productID, attrID, valueID); err != nil {
				return "", fmt.Errorf("failed to persist attribute pick %d/%d: %w", attrID, valueID, err)
			}
		}
	}

	// --- Gap 9b: create the proposal's variants. ---
	// Approval used to drop variants[] entirely (only specs persisted),
	// forcing the supplier to re-create variants post-approval with
	// stock 0 and no cost. Reconciliation rule: when the payload carries
	// at least one VALID variant entry, the product ends with exactly
	// the proposed set — the first valid entry becomes the default — and
	// the synthetic 'پیش‌فرض' variant in the C7 block below is skipped
	// (its hasVariant guard sees these rows). With no valid entries, C7
	// behaves exactly as before. Malformed entries (not an object, blank
	// name, duplicate name) are skipped with an audit note; they never
	// fail the approval itself. Variant stock lives only on the variant
	// row in this codebase (UpdateProductStock is product-level), so the
	// proposed stock is written there; the proposed supplier cost goes
	// to product_variants.supplier_base_cost (gap 10's column).
	if !existedBefore {
		seenVariantNames := map[string]bool{}
		var skippedVariants []string
		createdVariants := 0
		for i, pv := range parseProposedVariants(data) {
			if pv.Name == "" || seenVariantNames[pv.Name] {
				skippedVariants = append(skippedVariants, fmt.Sprintf("entry %d (%q)", i, pv.RawName))
				continue
			}
			seenVariantNames[pv.Name] = true
			attrJSON, mErr := json.Marshal(pv.Attributes)
			if mErr != nil {
				skippedVariants = append(skippedVariants, fmt.Sprintf("entry %d (%q)", i, pv.RawName))
				continue
			}
			spnc, sErr := uniqueSPNCTx(ctx, tx)
			if sErr != nil {
				return "", fmt.Errorf("failed to generate variant SKU for %s: %w", productID, sErr)
			}
			var variantCost interface{}
			if pv.Cost > 0 {
				variantCost = pv.Cost
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO product_variants (product_id, spnc, variant_name, attributes, is_default, stock, supplier_base_cost)
				VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7)`,
				productID, spnc, pv.Name, string(attrJSON), createdVariants == 0, pv.Stock, variantCost); err != nil {
				return "", fmt.Errorf("failed to create proposed variant %q: %w", pv.Name, err)
			}
			createdVariants++
		}
		if len(skippedVariants) > 0 && requestedBy != "" {
			if err := insertSupplierAuditTx(ctx, tx, requestedBy, "proposal_variants_skipped", map[string]interface{}{
				"proposal_id": proposalID, "product_id": productID, "skipped": skippedVariants,
			}); err != nil {
				return "", fmt.Errorf("failed to audit skipped proposal variants: %w", err)
			}
		}
	}

	// --- Audit C7: canonical inventory row + default SKU variant. ---
	// The approval previously entered on the legacy path only (products.stock
	// set directly, no inventory_balances row, no product_variants row):
	// returns-QC restock failed closed for it and SKU label printing had
	// nothing to print. Seed the supplier-owned sellable balance so the
	// product participates in the canonical ledger from birth (the DB trigger
	// keeps the products.stock projection in sync), and create a default
	// variant so the SKU engine can see it. Re-approvals are idempotent
	// (upsert + EXISTS guard).
	if requestedBy != "" {
		if err := setSupplierSellableTotalTx(ctx, tx, productID, requestedBy, stock); err != nil {
			return "", fmt.Errorf("failed to seed supplier inventory for %s: %w", productID, err)
		}
	}
	var hasVariant bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM product_variants WHERE product_id = $1)`,
		productID).Scan(&hasVariant); err != nil {
		return "", err
	}
	if !hasVariant {
		spnc, err := uniqueSPNCTx(ctx, tx)
		if err != nil {
			return "", fmt.Errorf("failed to generate variant SKU for %s: %w", productID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO product_variants (product_id, spnc, variant_name, attributes, is_default, stock)
			VALUES ($1, $2, 'پیش‌فرض', '{}', TRUE, $3)`,
			productID, spnc, stock); err != nil {
			return "", fmt.Errorf("failed to create default variant for %s: %w", productID, err)
		}
	}

	queryUpdate := `
		UPDATE pending_approvals
		SET status = 'approved', reviewed_at = NOW(), rejection_reason = NULLIF($2, '')
		WHERE id = $1 AND status = 'pending'
	`
	res, err := tx.ExecContext(ctx, queryUpdate, proposalID, notes)
	if err != nil {
		return "", err
	}
	// P1 fix 2026-10-20: atomic backstop against double approval. If another
	// finalize already approved this proposal (concurrent double-submit),
	// this UPDATE touches zero rows and we roll back instead of re-running
	// the product upsert with stale proposal data.
	if n, _ := res.RowsAffected(); n == 0 {
		return "", ErrProposalNotPending
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return productID, nil
}

// payloadMeasurementValue reads a positive measurement number from a
// proposal payload map (JSON numbers arrive as float64; plain ints are
// accepted too). Missing, non-numeric or non-positive values report as
// absent.
func payloadMeasurementValue(data map[string]interface{}, keys ...string) (int, bool) {
	for _, key := range keys {
		switch v := data[key].(type) {
		case float64:
			if v > 0 {
				return int(v), true
			}
		case int:
			if v > 0 {
				return v, true
			}
		case int64:
			if v > 0 {
				return int(v), true
			}
		}
	}
	return 0, false
}

// proposalAttributePicks extracts the first value per attribute from a
// proposal's categoryAttributes, in the shape SupplierProposeForm sends:
// {"attributes": {"<dkAttrId>": [dkValueId, ...]}, "colorId":…, "brandId":…}.
// Single-select is the platform rule (Oct 29), so later values in each
// list are dropped. Unparseable keys/values are ignored.
func proposalAttributePicks(data map[string]interface{}) map[int64]int64 {
	out := map[int64]int64{}
	ca, ok := data["categoryAttributes"].(map[string]interface{})
	if !ok {
		return out
	}
	attrs, ok := ca["attributes"].(map[string]interface{})
	if !ok {
		return out
	}
	for key, raw := range attrs {
		attrID, err := strconv.ParseInt(strings.TrimSpace(key), 10, 64)
		if err != nil || attrID <= 0 {
			continue
		}
		list, ok := raw.([]interface{})
		if !ok || len(list) == 0 {
			continue
		}
		if v, ok := list[0].(float64); ok && v > 0 {
			out[attrID] = int64(v)
		}
	}
	return out
}

// proposedVariant is one parsed entry of a proposal's variants[] list.
type proposedVariant struct {
	Name       string // trimmed variant_name; empty means malformed
	RawName    string // variant_name as sent (for audit notes)
	Attributes map[string]string
	Stock      int
	Cost       int64 // proposed supplier cost; 0 = not proposed (inherit)
}

// parseProposedVariants extracts the variants[] entries from a proposal
// payload in the shape SupplierProposeForm sends:
// {variant_name, attributes{...}, stock, supplier_base_cost?}.
// Malformed entries are returned with an empty Name so the caller can
// skip-and-audit them without failing the approval.
func parseProposedVariants(data map[string]interface{}) []proposedVariant {
	raw, ok := data["variants"].([]interface{})
	if !ok {
		return nil
	}
	out := make([]proposedVariant, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			out = append(out, proposedVariant{})
			continue
		}
		pv := proposedVariant{Attributes: map[string]string{}}
		pv.RawName, _ = m["variant_name"].(string)
		pv.Name = strings.TrimSpace(pv.RawName)
		if attrs, ok := m["attributes"].(map[string]interface{}); ok {
			for k, v := range attrs {
				if s, ok := v.(string); ok {
					pv.Attributes[k] = s
				}
			}
		}
		if s, ok := m["stock"].(float64); ok && s > 0 {
			pv.Stock = int(s)
		}
		if c, ok := m["supplier_base_cost"].(float64); ok && c > 0 {
			pv.Cost = int64(c)
		} else if c, ok := m["supplierBaseCost"].(float64); ok && c > 0 {
			pv.Cost = int64(c)
		}
		out = append(out, pv)
	}
	return out
}

// uniqueSPNCTx generates a unique 8-char uppercase-hex SPNC (variant code),
// replicating the SKU backfill logic from migration 202610160006 for newly
// approved products. Retries on collision; the UNIQUE constraint is the final
// guard.
func uniqueSPNCTx(ctx context.Context, tx *sql.Tx) (string, error) {
	for i := 0; i < 10; i++ {
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		spnc := strings.ToUpper(hex.EncodeToString(b[:]))
		var exists bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM product_variants WHERE spnc = $1)`, spnc).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return spnc, nil
		}
	}
	return "", fmt.Errorf("could not generate a unique SPNC after 10 attempts")
}

// GetSupplierProducts returns active products for a supplier
func (r *SupplierRepository) GetSupplierProducts(ctx context.Context, supplierID string) ([]domain.Product, error) {
	query := `
		SELECT id, title, slug, category_slug, supplier_id, original_price, supplier_base_cost,
		       platform_margin, is_original, stock, COALESCE(image_url, ''), COALESCE(description, ''),
		       warehouse_name, preparation_days, COALESCE(supplier_warranty, ''), is_featured, is_special_deal,
		       COALESCE(supply_option, '1day'), COALESCE(warranty_property_id, ''),
		       COALESCE(brand, ''), rating, reviews_count, created_at, is_active, origin_supplier_warehouse_id, COALESCE(fulfillment_method, 'warehouse')
		FROM products
		WHERE supplier_id = $1 OR $1 = 'all'
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []domain.Product
	for rows.Next() {
		var p domain.Product
		if err := rows.Scan(
			&p.ID, &p.Title, &p.Slug, &p.CategorySlug, &p.SupplierID, &p.OriginalPrice,
			&p.SupplierBaseCost, &p.PlatformMargin, &p.IsOriginal, &p.Stock,
			&p.ImageURL, &p.Description, &p.WarehouseName,
			&p.PreparationDays, &p.SupplierWarranty, &p.IsFeatured, &p.IsSpecialDeal,
			&p.SupplyOption, &p.WarrantyPropertyID,
			&p.Brand, &p.Rating, &p.ReviewsCount, &p.CreatedAt, &p.IsActive, &p.OriginSupplierWarehouseId, &p.FulfillmentMethod,
		); err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, nil
}

// setSupplierSellableTotalTx sets the supplier-owned sellable balance for a
// product to total, inside the caller's transaction.
//
// TODO (team-00, live-parity): this helper was referenced by ApproveProposal
// and UpdateProductStock, but its defining file (inventory_unify.go, named by
// migration 202610170001) was never committed in any branch, so its exact live
// semantics are unverified. This is the minimal version matching every call
// site and the comments at those sites:
//   - the supplier-owned sellable row is the one
//     debitSupplierSellableForQCReceiptTx identifies: owner_type='supplier',
//     location_type='supplier', location_key='supplier:'+supplierID,
//     status='sellable';
//   - that row is upserted to exactly total (clamped at zero, never negative);
//   - other sellable rows (e.g. warehouse-held) are untouched, so after the
//     sync below the products.stock projection may legitimately exceed total
//     (see UpdateProductStock's comment);
//   - the projection is re-synced via syncProductStockTx, which is a no-op for
//     products with no balance rows (legacy path keeps products.stock).
//
// No inventory_movements row is written: neither call site passes an actor or
// a reference, and inventing one would fabricate audit history. The new row
// is marked reconciled=FALSE because supplier-declared stock has not been
// warehouse-reconciled; an existing row keeps its reconciled flag.
func setSupplierSellableTotalTx(ctx context.Context, tx *sql.Tx, productID, supplierID string, total int) error {
	if total < 0 {
		total = 0
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO inventory_balances (id, product_id, supplier_id, owner_type, location_type, location_key, status, quantity, reconciled)
		VALUES ($1, $2, $3, 'supplier', 'supplier', $4, 'sellable', $5, FALSE)
		ON CONFLICT (product_id, owner_type, location_type, location_key, status)
		DO UPDATE SET quantity = EXCLUDED.quantity, updated_at = NOW()`,
		"INV-"+uuid.NewString(), productID, supplierID, "supplier:"+supplierID, total); err != nil {
		return err
	}
	return syncProductStockTx(ctx, tx, productID)
}

// UpdateProductStock updates inventory for a product through the canonical
// inventory_balances ledger: the supplier-owned sellable balance is adjusted so
// the canonical sellable total equals newStock, and the products.stock
// projection is re-synced. Warehouse-held sellable stock is never reduced by a
// supplier edit (the supplier row is clamped at zero); in that case the stored
// projection legitimately exceeds newStock.
func (r *SupplierRepository) UpdateProductStock(ctx context.Context, supplierID, productID string, newStock int, reason string) error {
	if newStock < 0 {
		return fmt.Errorf("stock cannot be negative")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM products WHERE id = $1 AND supplier_id = $2)`,
		productID, supplierID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("product %s not found", productID)
	}
	if err := setSupplierSellableTotalTx(ctx, tx, productID, supplierID, newStock); err != nil {
		return err
	}
	// P2-9: persist the caller-supplied reason in audit_logs instead of
	// silently dropping it — «who changed stock/price and why» must be traceable.
	if err := insertSupplierAuditTx(ctx, tx, supplierID, "supplier_stock_update", map[string]interface{}{
		"product_id": productID, "new_stock": newStock, "reason": reason,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

type SupplierReturnRequest struct {
	ID                string     `json:"id"`
	SupplierID        string     `json:"supplierId"`
	ProductID         string     `json:"productId"`
	ProductTitle      string     `json:"productTitle"`
	WarehouseName     string     `json:"warehouseName"`
	BinCode           string     `json:"binCode,omitempty"`
	RequestedQuantity int        `json:"requestedQuantity"`
	EligibleQuantity  int        `json:"eligibleQuantity"`
	CollectionMethod  string     `json:"collectionMethod"`
	PostalCharge      int64      `json:"postalCharge"`
	Status            string     `json:"status"`
	Reason            string     `json:"reason"`
	CreatedAt         time.Time  `json:"createdAt"`
	ReviewedAt        *time.Time `json:"reviewedAt,omitempty"`
	HandoverAt        *time.Time `json:"handoverAt,omitempty"`
	ReceivedAt        *time.Time `json:"receivedAt,omitempty"`
	InspectedAt       *time.Time `json:"inspectedAt,omitempty"`
	InspectionStatus  string     `json:"inspectionStatus,omitempty"`
	AcceptedQuantity  int        `json:"acceptedQuantity"`
	FailedQuantity    int        `json:"failedQuantity"`
}

func (r *SupplierRepository) CreateReturnRequest(ctx context.Context, supplierID, productID string, quantity int, method, reason string) (*SupplierReturnRequest, error) {
	return r.CreateReturnRequestWithIdempotency(ctx, supplierID, productID, quantity, method, reason, "legacy-"+uuid.NewString(), "")
}

func (r *SupplierRepository) CreateReturnRequestWithIdempotency(ctx context.Context, supplierID, productID string, quantity int, method, reason, idempotencyKey, actorUserID string) (*SupplierReturnRequest, error) {
	if quantity <= 0 || (method != "supplier_collect" && method != "postal") || strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("invalid return request")
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		// Preserve the pre-idempotency API contract for legacy callers. New HTTP
		// callers should always send Idempotency-Key to make retries replay-safe.
		idempotencyKey = "legacy-" + uuid.NewString()
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var existing SupplierReturnRequest
	var existingCreated time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT rr.id, rr.supplier_id, rr.product_id, p.title, COALESCE(p.warehouse_name,''),
		       rr.requested_quantity, p.stock, rr.collection_method, rr.postal_charge,
		       rr.status, rr.reason, rr.created_at, rr.reviewed_at, rr.handover_at,
		       rr.received_at, rr.inspected_at, COALESCE(rr.inspection_status,''),
		       rr.accepted_quantity, rr.failed_quantity
		FROM supplier_return_requests rr
		JOIN products p ON p.id = rr.product_id
		WHERE rr.idempotency_key = $1 AND rr.supplier_id = $2
		FOR UPDATE`, idempotencyKey, supplierID).Scan(
		&existing.ID, &existing.SupplierID, &existing.ProductID, &existing.ProductTitle,
		&existing.WarehouseName, &existing.RequestedQuantity, &existing.EligibleQuantity,
		&existing.CollectionMethod, &existing.PostalCharge, &existing.Status, &existing.Reason,
		&existingCreated, &existing.ReviewedAt, &existing.HandoverAt, &existing.ReceivedAt,
		&existing.InspectedAt, &existing.InspectionStatus, &existing.AcceptedQuantity, &existing.FailedQuantity,
	)
	if err == nil {
		existing.CreatedAt = existingCreated
		return &existing, tx.Commit()
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	var title, warehouse string
	if err := tx.QueryRowContext(ctx, `SELECT title, COALESCE(warehouse_name,'') FROM products WHERE id=$1 AND supplier_id=$2 FOR UPDATE`, productID, supplierID).Scan(&title, &warehouse); err != nil {
		return nil, err
	}
	// Canonical sellable balances are the only source of returnable quantity.
	// products.stock is a legacy projection and may legitimately lag behind it.
	type allocation struct {
		balanceID   string
		locationKey string
		quantity    int
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, location_key, quantity
		FROM inventory_balances
		WHERE product_id=$1 AND supplier_id=$2 AND owner_type='supplier' AND status='sellable' AND quantity > 0
		ORDER BY reconciled DESC, updated_at, id
		FOR UPDATE`, productID, supplierID)
	if err != nil {
		return nil, err
	}
	allocations := make([]allocation, 0)
	eligible := 0
	for rows.Next() {
		var item allocation
		if err := rows.Scan(&item.balanceID, &item.locationKey, &item.quantity); err != nil {
			return nil, err
		}
		allocations = append(allocations, item)
		eligible += item.quantity
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if quantity > eligible {
		return nil, fmt.Errorf("return quantity exceeds canonical eligible quantity")
	}
	var charge *int64
	if err := tx.QueryRowContext(ctx, `SELECT supplier_return_postal_charge FROM platform_settings WHERE id=1`).Scan(&charge); err != nil {
		return nil, err
	}
	postalCharge := int64(0)
	if method == "postal" {
		if charge == nil {
			return nil, fmt.Errorf("postal return charge policy is not configured")
		}
		postalCharge = *charge
	}
	id := "RET-" + uuid.NewString()
	remaining := quantity
	firstBalanceID := ""
	allocationIndex := 0
	for _, item := range allocations {
		if remaining == 0 {
			break
		}
		reserved := item.quantity
		if reserved > remaining {
			reserved = remaining
		}
		if firstBalanceID == "" {
			firstBalanceID = item.balanceID
		}
		if _, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity-$1,updated_at=NOW() WHERE id=$2 AND status='sellable' AND quantity >= $1`, reserved, item.balanceID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inventory_balances (id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled)
			SELECT $1,product_id,supplier_id,owner_type,location_type,location_key,'return_requested',$2,reconciled
			FROM inventory_balances WHERE id=$3
			ON CONFLICT (product_id,owner_type,location_type,location_key,status)
			DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW()`,
			"INV-"+uuid.NewString(), reserved, item.balanceID); err != nil {
			return nil, err
		}
		reservationID := "RES-" + uuid.NewString()
		if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_reservations (id,product_id,supplier_id,location_key,quantity,reference_type,reference_id,status,idempotency_key) VALUES ($1,$2,$3,$4,$5,'supplier_return',$6,'active',$7)`, reservationID, productID, supplierID, item.locationKey, reserved, id, fmt.Sprintf("%s:reservation:%d", idempotencyKey, allocationIndex)); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements (id,product_id,supplier_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata) VALUES ($1,$2,$3,$4,'sellable','return_requested',$5,$5,'supplier_return',$6,$7,NULLIF($8,''),'{}'::jsonb)`, "IM-"+uuid.NewString(), productID, supplierID, reserved, item.locationKey, id, fmt.Sprintf("%s:movement:%d", idempotencyKey, allocationIndex), actorUserID); err != nil {
			return nil, err
		}
		remaining -= reserved
		allocationIndex++
	}
	if remaining != 0 || firstBalanceID == "" {
		return nil, fmt.Errorf("return reservation allocation is inconsistent")
	}
	// Fix P3-14: the canonical source of truth is inventory_balances. Recompute
	// the products.stock projection from ALL sellable rows (supplier +
	// warehouse), never a supplier-only partial SUM, and never overwrite the
	// trigger-maintained projection with a partial sum. syncProductStockTx is a
	// no-op for products with no balance rows (legacy path keeps products.stock
	// authoritative).
	if err := syncProductStockTx(ctx, tx, productID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO supplier_return_requests
		  (id,supplier_id,product_id,requested_quantity,collection_method,postal_charge,status,reason,idempotency_key,inventory_reference_id)
		VALUES ($1,$2,$3,$4,$5,$6,'awaiting_return_test',$7,$8,$9)`, id, supplierID, productID, quantity, method, postalCharge, reason, idempotencyKey, firstBalanceID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO supplier_return_events (id,return_request_id,to_status,reason,actor_user_id,idempotency_key)
		VALUES ($1,$2,'awaiting_return_test',$3,NULLIF($4,''),$5)`,
		"RET-EVT-"+uuid.NewString(), id, reason, actorUserID, idempotencyKey+":event"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &SupplierReturnRequest{ID: id, SupplierID: supplierID, ProductID: productID, ProductTitle: title, WarehouseName: warehouse, RequestedQuantity: quantity, EligibleQuantity: eligible - quantity, CollectionMethod: method, PostalCharge: postalCharge, Status: "awaiting_return_test", Reason: reason}, nil
}

func (r *SupplierRepository) GetReturnRequests(ctx context.Context, supplierID string) ([]SupplierReturnRequest, error) {
	query := `SELECT rr.id,rr.supplier_id,rr.product_id,p.title,COALESCE(p.warehouse_name,''),rr.requested_quantity,p.stock,rr.collection_method,rr.postal_charge,rr.status,rr.reason,rr.created_at,rr.reviewed_at,rr.handover_at,rr.received_at,rr.inspected_at,COALESCE(rr.inspection_status,''),rr.accepted_quantity,rr.failed_quantity FROM supplier_return_requests rr JOIN products p ON p.id=rr.product_id`
	args := []interface{}{}
	if supplierID != "all" {
		query += " WHERE rr.supplier_id=$1"
		args = append(args, supplierID)
	}
	query += " ORDER BY rr.created_at DESC"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []SupplierReturnRequest{}
	for rows.Next() {
		var v SupplierReturnRequest
		if err := rows.Scan(&v.ID, &v.SupplierID, &v.ProductID, &v.ProductTitle, &v.WarehouseName, &v.RequestedQuantity, &v.EligibleQuantity, &v.CollectionMethod, &v.PostalCharge, &v.Status, &v.Reason, &v.CreatedAt, &v.ReviewedAt, &v.HandoverAt, &v.ReceivedAt, &v.InspectedAt, &v.InspectionStatus, &v.AcceptedQuantity, &v.FailedQuantity); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, nil
}

// ReviewReturn applies the warehouse/admin review transition. Rejection releases
// the sellable reservation exactly once; approval keeps it unavailable until handover.
func (r *SupplierRepository) ReviewReturn(ctx context.Context, returnID, status, reason, actorUserID string) error {
	if status != "approved" && status != "rejected" {
		return fmt.Errorf("invalid return review status")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var current, productID, supplierID, inventoryKey string
	var quantity int
	if err := tx.QueryRowContext(ctx, `
		SELECT status, product_id, supplier_id, requested_quantity,
		       COALESCE((SELECT location_key FROM inventory_balances ib WHERE ib.id=rr.inventory_reference_id), 'legacy:' || rr.product_id)
		FROM supplier_return_requests rr
		WHERE rr.id=$1 FOR UPDATE`, returnID).Scan(&current, &productID, &supplierID, &quantity, &inventoryKey); err != nil {
		return err
	}
	if current == status {
		return tx.Commit()
	}
	if current != "awaiting_return_test" {
		return fmt.Errorf("return is not awaiting review")
	}
	now := time.Now()
	if status == "rejected" {
		if err := r.releaseReturnReservations(ctx, tx, returnID, productID, supplierID, actorUserID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE supplier_return_requests
		SET status=$1,rejection_reason=NULLIF($2,''),reviewed_by=NULLIF($3,''),reviewed_at=$4
		WHERE id=$5 AND status='awaiting_return_test'`, status, reason, actorUserID, now, returnID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO supplier_return_events (id,return_request_id,from_status,to_status,reason,actor_user_id,idempotency_key)
		VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7)
		ON CONFLICT (idempotency_key) DO NOTHING`, "RET-EVT-"+uuid.NewString(), returnID, current, status, reason, actorUserID, returnID+":"+status); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SupplierRepository) releaseReturnReservations(ctx context.Context, tx *sql.Tx, returnID, productID, supplierID, actorUserID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT location_key,quantity FROM inventory_reservations WHERE reference_type='supplier_return' AND reference_id=$1 AND status='active' FOR UPDATE`, returnID)
	if err != nil {
		return err
	}
	type reservation struct {
		locationKey string
		quantity    int
	}
	reservations := make([]reservation, 0)
	for rows.Next() {
		var item reservation
		if err := rows.Scan(&item.locationKey, &item.quantity); err != nil {
			return err
		}
		reservations = append(reservations, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	count := 0
	for _, item := range reservations {
		result, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity-$1,updated_at=NOW() WHERE product_id=$2 AND supplier_id=$3 AND location_key=$4 AND status='return_requested' AND quantity >= $1`, item.quantity, productID, supplierID, item.locationKey)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			return fmt.Errorf("return reservation was already released or is inconsistent")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_balances (id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) SELECT $1,product_id,supplier_id,owner_type,location_type,location_key,'sellable',$2,reconciled FROM inventory_balances WHERE product_id=$3 AND supplier_id=$4 AND location_key=$5 AND status='return_requested' ON CONFLICT (product_id,owner_type,location_type,location_key,status) DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW()`, "INV-"+uuid.NewString(), item.quantity, productID, supplierID, item.locationKey); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements (id,product_id,supplier_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata) VALUES ($1,$2,$3,$4,'return_requested','sellable',$5,$5,'supplier_return',$6,$7,NULLIF($8,''),'{}'::jsonb) ON CONFLICT (idempotency_key) DO NOTHING`, "IM-"+uuid.NewString(), productID, supplierID, item.quantity, item.locationKey, returnID, returnID+":release:"+item.locationKey, actorUserID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE inventory_reservations SET status='released',released_at=NOW() WHERE reference_id=$1 AND reference_type='supplier_return' AND location_key=$2 AND status='active'`, returnID, item.locationKey); err != nil {
			return err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("return reservation was already released or is inconsistent")
	}
	// Fix P3-14: the canonical source of truth is inventory_balances. Recompute
	// the products.stock projection from ALL sellable rows (supplier +
	// warehouse), never a supplier-only partial SUM, and never overwrite the
	// trigger-maintained projection with a partial sum. syncProductStockTx is a
	// no-op for products with no balance rows (legacy path keeps products.stock
	// authoritative).
	if err := syncProductStockTx(ctx, tx, productID); err != nil {
		return err
	}
	return nil
}

func (r *SupplierRepository) consumeReturnReservations(ctx context.Context, tx *sql.Tx, returnID, productID, supplierID, actorUserID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT location_key,quantity FROM inventory_reservations WHERE reference_type='supplier_return' AND reference_id=$1 AND status='active' FOR UPDATE`, returnID)
	if err != nil {
		return err
	}
	type reservation struct {
		locationKey string
		quantity    int
	}
	reservations := make([]reservation, 0)
	for rows.Next() {
		var item reservation
		if err := rows.Scan(&item.locationKey, &item.quantity); err != nil {
			return err
		}
		reservations = append(reservations, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	count := 0
	for _, item := range reservations {
		result, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity-$1,updated_at=NOW() WHERE product_id=$2 AND supplier_id=$3 AND location_key=$4 AND status='return_requested' AND quantity >= $1`, item.quantity, productID, supplierID, item.locationKey)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			return fmt.Errorf("return reservation is inconsistent")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_balances (id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES ($1,$2,$3,'supplier','in_transit',$4,'in_transit',$5,FALSE) ON CONFLICT (product_id,owner_type,location_type,location_key,status) DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW()`, "INV-"+uuid.NewString(), productID, supplierID, "return:"+returnID, item.quantity); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements (id,product_id,supplier_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id) VALUES ($1,$2,$3,$4,'return_requested','in_transit',$5,$6,'supplier_return',$7,$8,NULLIF($9,'')) ON CONFLICT (idempotency_key) DO NOTHING`, "IM-"+uuid.NewString(), productID, supplierID, item.quantity, item.locationKey, "return:"+returnID, returnID, "handover:"+returnID+":"+item.locationKey, actorUserID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE inventory_reservations SET status='consumed' WHERE reference_id=$1 AND reference_type='supplier_return' AND location_key=$2 AND status='active'`, returnID, item.locationKey); err != nil {
			return err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("return reservation is inconsistent")
	}
	// Storage-fee positions (gap 7 lifecycle): handover is the
	// physical warehouse exit for the reserved supplier stock, so
	// the consumed quantity leaves the position ledger (FIFO) in
	// the same transaction. Reservations carry no warehouse id
	// (bin lots predate the warehouses table), so the FIFO runs
	// over this supplier's positions for the product un-narrowed.
	totalConsumed := 0
	for _, item := range reservations {
		totalConsumed += item.quantity
	}
	if err := decrementStoragePositionsFIFO(ctx, tx, productID, supplierID, "", totalConsumed); err != nil {
		return err
	}
	return nil
}

// CancelReturn releases a pre-handover reservation exactly once.
func (r *SupplierRepository) CancelReturn(ctx context.Context, supplierID, returnID, reason, actorUserID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current, productID string
	var quantity int
	if err := tx.QueryRowContext(ctx, `
		SELECT status,product_id,requested_quantity
		FROM supplier_return_requests rr
		WHERE rr.id=$1 AND rr.supplier_id=$2 FOR UPDATE`, returnID, supplierID).Scan(&current, &productID, &quantity); err != nil {
		return err
	}
	if current == "cancelled" {
		return tx.Commit()
	}
	if current != "awaiting_return_test" && current != "approved" && current != "pickup_pending" && current != "postal_pending" {
		return fmt.Errorf("return cannot be cancelled in state %s", current)
	}
	if err := r.releaseReturnReservations(ctx, tx, returnID, productID, supplierID, actorUserID); err != nil {
		return err
	}
	now := time.Now()
	if _, err := tx.ExecContext(ctx, `UPDATE supplier_return_requests SET status='cancelled',rejection_reason=NULLIF($1,''),cancelled_by=NULLIF($2,''),reviewed_at=COALESCE(reviewed_at,$3) WHERE id=$4 AND supplier_id=$5`, reason, actorUserID, now, returnID, supplierID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO supplier_return_events (id,return_request_id,from_status,to_status,reason,actor_user_id,idempotency_key)
		VALUES ($1,$2,$3,'cancelled',$4,NULLIF($5,''),$6)
		ON CONFLICT (idempotency_key) DO NOTHING`, "RET-EVT-"+uuid.NewString(), returnID, current, reason, actorUserID, returnID+":cancelled"); err != nil {
		return err
	}
	return tx.Commit()
}

// HandoverReturn records the physical carrier handover after every parcel has
// been confirmed. The request quantity leaves the sellable return reservation
// and enters transit exactly once.
// ErrSupplierReturnNotFound is returned when a supplier return request id
// does not exist. Warehouse handlers map it to 404 with a Persian message
// instead of leaking the raw sql.ErrNoRows text to staff screens.
var ErrSupplierReturnNotFound = errors.New("مرجوعی تأمین‌کننده یافت نشد")

// ErrProposalNotPending is returned by ApproveProposal when its atomic
// backstop UPDATE touches zero rows because the proposal was already reviewed.
var ErrProposalNotPending = errors.New("proposal is not pending")

func (r *SupplierRepository) HandoverReturn(ctx context.Context, returnID, actorUserID, idempotencyKey string) error {
	if strings.TrimSpace(idempotencyKey) == "" {
		return fmt.Errorf("idempotency key required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current, productID, supplierID string
	var quantity int
	if err := tx.QueryRowContext(ctx, `SELECT status,product_id,supplier_id,requested_quantity FROM supplier_return_requests WHERE id=$1 FOR UPDATE`, returnID).Scan(&current, &productID, &supplierID, &quantity); err != nil {
		if err == sql.ErrNoRows {
			return ErrSupplierReturnNotFound
		}
		return err
	}
	if current == "in_transit" || current == "received_pending_qc" || current == "completed" {
		return tx.Commit()
	}
	if current != "packed" && current != "pickup_pending" && current != "postal_pending" {
		return fmt.Errorf("return is not ready for handover")
	}
	var parcelCount, confirmedCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER (WHERE status IN ('confirmed','handed_over')) FROM supplier_return_parcels WHERE return_request_id=$1`, returnID).Scan(&parcelCount, &confirmedCount); err != nil {
		return err
	}
	if parcelCount == 0 || parcelCount != confirmedCount {
		return fmt.Errorf("all parcels must be confirmed before handover")
	}
	if err := r.consumeReturnReservations(ctx, tx, returnID, productID, supplierID, actorUserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE supplier_return_parcels SET status='handed_over',handed_over_at=NOW(),updated_at=NOW() WHERE return_request_id=$1 AND status='confirmed'`, returnID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE supplier_return_requests SET status='in_transit',handover_at=NOW(),updated_at=NOW() WHERE id=$1`, returnID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO supplier_return_events (id,return_request_id,from_status,to_status,actor_user_id,idempotency_key) VALUES ($1,$2,$3,'in_transit',NULLIF($4,''),$5) ON CONFLICT (idempotency_key) DO NOTHING`, "RET-EVT-"+uuid.NewString(), returnID, current, actorUserID, idempotencyKey+":event"); err != nil {
		return err
	}
	return tx.Commit()
}

// ReceiveReturn records physical receipt at the destination and moves the
// quantity into quarantine until the final disposition is recorded.
func (r *SupplierRepository) ReceiveReturn(ctx context.Context, returnID, actorUserID, idempotencyKey string) error {
	if strings.TrimSpace(idempotencyKey) == "" {
		return fmt.Errorf("idempotency key required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current, productID, supplierID string
	var quantity int
	if err := tx.QueryRowContext(ctx, `SELECT status,product_id,supplier_id,requested_quantity FROM supplier_return_requests WHERE id=$1 FOR UPDATE`, returnID).Scan(&current, &productID, &supplierID, &quantity); err != nil {
		if err == sql.ErrNoRows {
			return ErrSupplierReturnNotFound
		}
		return err
	}
	if current == "received_pending_qc" || current == "completed" || current == "qc_failed" {
		return tx.Commit()
	}
	if current != "in_transit" {
		return fmt.Errorf("return is not in transit")
	}
	result, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity-$1,updated_at=NOW() WHERE product_id=$2 AND supplier_id=$3 AND location_key=$4 AND status='in_transit' AND quantity >= $1`, quantity, productID, supplierID, "return:"+returnID)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		return fmt.Errorf("in-transit inventory is inconsistent")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_balances (id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES ($1,$2,$3,'supplier','warehouse','return:' || $4,'quarantine',$5,FALSE) ON CONFLICT (product_id,owner_type,location_type,location_key,status) DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW()`, "INV-"+uuid.NewString(), productID, supplierID, returnID, quantity); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements (id,product_id,supplier_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id) VALUES ($1,$2,$3,$4,'in_transit','quarantine',$5,$6,'supplier_return',$7,$8,NULLIF($9,'')) ON CONFLICT (idempotency_key) DO NOTHING`, "IM-"+uuid.NewString(), productID, supplierID, quantity, "return:"+returnID, "return:"+returnID, returnID, "receive:"+returnID, actorUserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE supplier_return_parcels SET status='received',updated_at=NOW() WHERE return_request_id=$1 AND status='handed_over'`, returnID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE supplier_return_requests SET status='received_pending_qc',received_at=NOW(),inspection_status='pending',updated_at=NOW() WHERE id=$1`, returnID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO supplier_return_events (id,return_request_id,from_status,to_status,actor_user_id,idempotency_key) VALUES ($1,$2,'in_transit','received_pending_qc',NULLIF($3,''),$4) ON CONFLICT (idempotency_key) DO NOTHING`, "RET-EVT-"+uuid.NewString(), returnID, actorUserID, idempotencyKey+":event"); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SupplierRepository) InspectReturn(ctx context.Context, returnID, actorUserID, idempotencyKey string, accepted, failed int, reason string) error {
	if strings.TrimSpace(idempotencyKey) == "" || accepted < 0 || failed < 0 || accepted+failed <= 0 {
		return fmt.Errorf("valid inspection and idempotency key required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current, productID, supplierID string
	var requested int
	if err := tx.QueryRowContext(ctx, `SELECT status,product_id,supplier_id,requested_quantity FROM supplier_return_requests WHERE id=$1 FOR UPDATE`, returnID).Scan(&current, &productID, &supplierID, &requested); err != nil {
		if err == sql.ErrNoRows {
			return ErrSupplierReturnNotFound
		}
		return err
	}
	if current == "completed" || current == "qc_failed" {
		return tx.Commit()
	}
	if current != "received_pending_qc" || accepted+failed != requested {
		return fmt.Errorf("invalid inspection state or quantity")
	}
	status := "completed"
	inspection := "passed"
	if failed > 0 {
		status = "qc_failed"
		inspection = "partial"
		if accepted == 0 {
			inspection = "failed"
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity-$1,updated_at=NOW() WHERE product_id=$2 AND supplier_id=$3 AND location_key=$4 AND status='quarantine' AND quantity >= $1`, accepted+failed, productID, supplierID, "return:"+returnID)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		return fmt.Errorf("quarantine inventory is inconsistent")
	}
	if accepted > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_balances (id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES ($1,$2,$3,'supplier','warehouse',$4,'sellable',$5,FALSE) ON CONFLICT (product_id,owner_type,location_type,location_key,status) DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW()`, "INV-"+uuid.NewString(), productID, supplierID, "return:"+returnID, accepted); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements (id,product_id,supplier_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id) VALUES ($1,$2,$3,$4,'quarantine','sellable',$5,$6,'supplier_return',$7,$8,NULLIF($9,'')) ON CONFLICT (idempotency_key) DO NOTHING`, "IM-"+uuid.NewString(), productID, supplierID, accepted, "return:"+returnID, "return:"+returnID, returnID, fmt.Sprintf("inspect:%s:accepted", returnID), actorUserID); err != nil {
			return err
		}
	}
	if failed > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_balances (id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled) VALUES ($1,$2,$3,'supplier','warehouse',$4,'damaged',$5,FALSE) ON CONFLICT (product_id,owner_type,location_type,location_key,status) DO UPDATE SET quantity=inventory_balances.quantity+EXCLUDED.quantity,updated_at=NOW()`, "INV-"+uuid.NewString(), productID, supplierID, "return:"+returnID, failed); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO inventory_movements (id,product_id,supplier_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id) VALUES ($1,$2,$3,$4,'quarantine','damaged',$5,$6,'supplier_return',$7,$8,NULLIF($9,'')) ON CONFLICT (idempotency_key) DO NOTHING`, "IM-"+uuid.NewString(), productID, supplierID, failed, "return:"+returnID, "return:"+returnID, returnID, fmt.Sprintf("inspect:%s:failed", returnID), actorUserID); err != nil {
			return err
		}
	}
	// Fix P3-14: the canonical source of truth is inventory_balances. Recompute
	// the products.stock projection from ALL sellable rows (supplier +
	// warehouse), never a supplier-only partial SUM, and never overwrite the
	// trigger-maintained projection with a partial sum. syncProductStockTx is a
	// no-op for products with no balance rows (legacy path keeps products.stock
	// authoritative).
	if err := syncProductStockTx(ctx, tx, productID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE supplier_return_requests SET status=$1,inspection_status=$2,accepted_quantity=$3,failed_quantity=$4,inspected_at=NOW(),rejection_reason=NULLIF($5,''),updated_at=NOW() WHERE id=$6`, status, inspection, accepted, failed, reason, returnID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO supplier_return_events (id,return_request_id,from_status,to_status,reason,actor_user_id,idempotency_key) VALUES ($1,$2,'received_pending_qc',$3,$4,NULLIF($5,''),$6) ON CONFLICT (idempotency_key) DO NOTHING`, "RET-EVT-"+uuid.NewString(), returnID, status, reason, actorUserID, idempotencyKey+":event"); err != nil {
		return err
	}
	return tx.Commit()
}

type WarrantyProperty struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

func (r *SupplierRepository) GetWarrantyProperties(ctx context.Context) ([]WarrantyProperty, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,description,status FROM warranty_properties WHERE status='active' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WarrantyProperty{}
	for rows.Next() {
		var v WarrantyProperty
		if err := rows.Scan(&v.ID, &v.Name, &v.Description, &v.Status); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func (r *SupplierRepository) RequestWarrantyProperty(ctx context.Context, supplierID, productID, name, description string, images []string) (string, error) {
	if strings.TrimSpace(name) == "" || len(strings.TrimSpace(description)) < 30 || len(images) == 0 {
		return "", fmt.Errorf("warranty name, full description and at least one image are required")
	}
	raw, _ := json.Marshal(images)
	id := "WREQ-" + uuid.NewString()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO warranty_property_requests(id,supplier_id,product_id,proposed_name,description,image_urls) VALUES($1,$2,NULLIF($3,''),$4,$5,$6)`, id, supplierID, productID, name, description, raw); err != nil {
		return "", err
	}
	data, _ := json.Marshal(map[string]interface{}{"supplier_id": supplierID, "product_id": productID, "proposed_name": name, "description": description, "image_urls": images, "warranty_request_id": id})
	if _, err = tx.ExecContext(ctx, `INSERT INTO pending_approvals(id,entity_type,target_id,title,requested_data,requested_by,status) VALUES($1,'warranty_property',$2,$3,$4,$5,'pending')`, id, id, name, data, supplierID); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

// UpdateProductPricing updates wholesale cost C
// insertSupplierAuditTx appends one audit_logs row inside the caller's
// transaction (P2-9: stock/price edit reasons must not be dropped).
func insertSupplierAuditTx(ctx context.Context, tx *sql.Tx, supplierID, action string, details map[string]interface{}) error {
	payload, _ := json.Marshal(details)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO audit_logs (id, timestamp, operator, action, category, details)
		VALUES ($1, NOW(), $2, $3, 'supplier', $4)
	`, "LOG-"+uuid.NewString(), "supplier:"+supplierID, action, string(payload))
	return err
}

func (r *SupplierRepository) UpdateProductPricing(ctx context.Context, supplierID, productID string, newBaseCost int64, reason string) error {
	if newBaseCost <= 0 {
		return fmt.Errorf("supplier base cost must be positive")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := `UPDATE products SET supplier_base_cost = $1, updated_at = NOW() WHERE id = $2 AND supplier_id = $3`
	res, err := tx.ExecContext(ctx, query, newBaseCost, productID, supplierID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("product %s not found", productID)
	}
	// P2-9: persist the caller-supplied reason in audit_logs.
	if err := insertSupplierAuditTx(ctx, tx, supplierID, "supplier_pricing_update", map[string]interface{}{
		"product_id": productID, "new_supplier_base_cost": newBaseCost, "reason": reason,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// GetSupplierProductBaseCost loads the product's current wholesale cost
// under the caller's ownership (gap 11: the pricing handler compares
// against this before choosing the direct vs governed path). A product
// that is missing or belongs to another supplier returns the same error
// text UpdateProductPricing produces.
func (r *SupplierRepository) GetSupplierProductBaseCost(ctx context.Context, supplierID, productID string) (int64, error) {
	var cost int64
	err := r.db.QueryRowContext(ctx, `
		SELECT supplier_base_cost FROM products WHERE id = $1 AND supplier_id = $2
	`, productID, supplierID).Scan(&cost)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("product %s not found", productID)
	}
	if err != nil {
		return 0, err
	}
	return cost, nil
}

// ErrExceptionTicketProductNotFound: the exception-ticket target product
// does not belong to the requesting supplier.
var ErrExceptionTicketProductNotFound = errors.New("product not found for supplier")

// CreatePriceIncreaseExceptionTicket files the supplier's reference-price
// exception request as a finance-section support ticket (migration
// 202610310003 routes these by section = 'finance'). Only the columns the
// existing ticket inserts use are written (id, requester_user_id,
// requester_role, subject) plus that section column; the requested
// newReference and free-text message are validated by the handler but
// have no sanctioned column and are not persisted.
func (r *SupplierRepository) CreatePriceIncreaseExceptionTicket(ctx context.Context, supplierID, requesterUserID, productID string) (string, error) {
	var title string
	err := r.db.QueryRowContext(ctx, `
		SELECT title FROM products WHERE id = $1 AND supplier_id = $2
	`, productID, supplierID).Scan(&title)
	if err == sql.ErrNoRows {
		return "", ErrExceptionTicketProductNotFound
	}
	if err != nil {
		return "", err
	}
	if requesterUserID == "" {
		// support_tickets.requester_user_id is a UUID column: fall back to
		// the supplier's owner user id, never to the supplier record id.
		var ownerID sql.NullString
		if err := r.db.QueryRowContext(ctx, `
			SELECT owner_user_id FROM suppliers WHERE id = $1
		`, supplierID).Scan(&ownerID); err != nil {
			return "", err
		}
		if ownerID.Valid {
			requesterUserID = ownerID.String
		}
	}
	id := "ticket-" + uuid.NewString()
	subject := "درخواست استثنای افزایش قیمت — " + title
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO support_tickets (id, requester_user_id, requester_role, subject, section)
		VALUES ($1, $2, 'supplier', $3, 'finance')
	`, id, requesterUserID, subject); err != nil {
		return "", err
	}
	return id, nil
}

// CreateInbound registers a new ASN shipment to the warehouse
func (r *SupplierRepository) CreateInbound(ctx context.Context, supplierID string, data map[string]interface{}) (string, error) {
	inboundID := "INB-" + uuid.NewString()
	carrier, _ := data["carrier"].(string)
	if carrier == "" {
		carrier = "ناوگان اختصاصی تأمین‌کننده"
	}
	plate, _ := data["vehicle_plate"].(string)
	if plate == "" {
		plate, _ = data["truck_plate"].(string)
	}
	notes, _ := data["notes"].(string)

	itemsCount := 0
	cartonsCount := 0
	var itemsList []interface{}
	if itms, ok := data["items"].([]interface{}); ok {
		itemsList = itms
		for _, item := range itms {
			if m, ok := item.(map[string]interface{}); ok {
				q, qOK := m["quantity"].(float64)
				c, cOK := m["carton_count"].(float64)
				productID, _ := m["product_id"].(string)
				if !qOK || !cOK || q < 1 || c < 1 || q > 1000000 || c > 1000000 || q != math.Trunc(q) || c != math.Trunc(c) || productID == "" {
					return "", fmt.Errorf("قلم ورودی نامعتبر است")
				}
				var owned bool
				if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id = $1 AND supplier_id = $2)`, productID, supplierID).Scan(&owned); err != nil {
					return "", err
				}
				if !owned {
					return "", fmt.Errorf("این کالا متعلق به تأمین‌کننده شما نیست")
				}
				itemsCount += int(q)
				cartonsCount += int(c)
			} else {
				return "", fmt.Errorf("قلم ورودی نامعتبر است")
			}
		}
	}
	if itemsCount == 0 {
		return "", fmt.Errorf("ثبت محموله نیازمند حداقل یک قلم ورودی معتبر است")
	}
	if cartonsCount == 0 {
		return "", fmt.Errorf("تعداد کارتن الزامی است")
	}

	itemsBytes, _ := json.Marshal(itemsList)

	sup, err := r.GetSupplierByID(ctx, supplierID)
	if err != nil {
		return "", err
	}
	if sup == nil {
		return "", sql.ErrNoRows
	}
	supName := sup.Name

	// FIX 45: live's createInbound carries warehouseId + dispatchAt and
	// creates a 'scheduled' inbound (the card's dispatch/cancel buttons
	// act on scheduled rows); the old code hardcoded the warehouse name
	// and 'in_transit'.
	warehouseID, _ := data["warehouseId"].(string)
	if warehouseID == "" {
		warehouseID, _ = data["warehouse_id"].(string)
	}
	warehouseName := "انبار مرکزی تهران"
	if warehouseID != "" {
		var wn string
		if qerr := r.db.QueryRowContext(ctx, `SELECT name FROM warehouses WHERE id = $1`, warehouseID).Scan(&wn); qerr == nil && wn != "" {
			warehouseName = wn
		}
	}
	dispatchStr, _ := data["dispatchAt"].(string)
	if dispatchStr == "" {
		dispatchStr, _ = data["dispatch_at"].(string)
	}
	var dispatchAt sql.NullTime
	if dispatchStr != "" {
		if t, perr := time.Parse("2006-01-02T15:04", dispatchStr); perr == nil {
			dispatchAt = sql.NullTime{Time: t, Valid: true}
		} else if t, perr := time.Parse(time.RFC3339, dispatchStr); perr == nil {
			dispatchAt = sql.NullTime{Time: t, Valid: true}
		}
	}
	query := `
		INSERT INTO inbound_shipments (id, supplier_id, supplier_name, warehouse_id, warehouse_name, items_count, cartons_count, carrier, truck_plate, status, notes, items, dispatch_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'scheduled', $10, $11, $12, NOW())
	`
	_, err = r.db.ExecContext(ctx, query, inboundID, supplierID, supName, sql.NullString{String: warehouseID, Valid: warehouseID != ""}, warehouseName, itemsCount, cartonsCount, carrier, plate, notes, itemsBytes, dispatchAt)
	if err != nil {
		return "", err
	}
	return inboundID, nil
}

// GetSupplierInbounds retrieves all ASN inbounds for supplier
func (r *SupplierRepository) GetSupplierInbounds(ctx context.Context, supplierID string) ([]InboundShipment, error) {
	query := `
		SELECT id, supplier_id, supplier_name, warehouse_name, items_count, cartons_count,
		       COALESCE(carrier, ''), COALESCE(truck_plate, ''), status, created_at, received_at,
		       COALESCE(items, '[]'::jsonb), dispatch_at, dispatched_at
		FROM inbound_shipments
		WHERE supplier_id = $1 OR $1 = 'all'
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []InboundShipment{}
	for rows.Next() {
		var s InboundShipment
		if err := rows.Scan(
			&s.ID, &s.SupplierID, &s.SupplierName, &s.WarehouseName, &s.ItemsCount, &s.CartonsCount,
			&s.Carrier, &s.TruckPlate, &s.Status, &s.CreatedAt, &s.ReceivedAt,
			&s.Items, &s.DispatchAt, &s.DispatchedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	r.enrichInboundItems(ctx, list)
	return list, nil
}

// enrichInboundItems fills product_title on stored inbound items: the
// create payload stores product_id only, while live's card shows the
// title (its own fallback chain is product_title || product_id ||
// productId || کالا).
func (r *SupplierRepository) enrichInboundItems(ctx context.Context, list []InboundShipment) {
	ids := []string{}
	parsed := make([][]map[string]interface{}, len(list))
	for k := range list {
		if len(list[k].Items) == 0 {
			continue
		}
		var items []map[string]interface{}
		if err := json.Unmarshal(list[k].Items, &items); err != nil {
			continue
		}
		parsed[k] = items
		for _, it := range items {
			if pid, _ := it["product_id"].(string); pid != "" {
				ids = append(ids, pid)
			}
		}
	}
	if len(ids) == 0 {
		return
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, title FROM products WHERE id = ANY($1::varchar[])`, pq.Array(ids))
	if err != nil {
		return
	}
	defer rows.Close()
	titles := map[string]string{}
	for rows.Next() {
		var id, title string
		if err := rows.Scan(&id, &title); err == nil {
			titles[id] = title
		}
	}
	for k := range list {
		if parsed[k] == nil {
			continue
		}
		for _, it := range parsed[k] {
			if pid, _ := it["product_id"].(string); pid != "" {
				if t, ok := titles[pid]; ok {
					it["product_title"] = t
				}
			}
		}
		if b, err := json.Marshal(parsed[k]); err == nil {
			list[k].Items = b
		}
	}
}

// DispatchInbound moves a scheduled inbound to in_transit and stamps
// dispatched_at (live's tn «ارسال شد» button). Only the owning supplier's
// scheduled row moves; anything else is sql.ErrNoRows.
func (r *SupplierRepository) DispatchInbound(ctx context.Context, supplierID, inboundID string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE inbound_shipments SET status = 'in_transit', dispatched_at = NOW()
		WHERE id = $1 AND supplier_id = $2 AND status = 'scheduled'
	`, inboundID, supplierID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CancelInbound moves a scheduled inbound to cancelled (live's tn «لغو»
// button), with the same ownership/state guard as DispatchInbound.
func (r *SupplierRepository) CancelInbound(ctx context.Context, supplierID, inboundID string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE inbound_shipments SET status = 'cancelled'
		WHERE id = $1 AND supplier_id = $2 AND status = 'scheduled'
	`, inboundID, supplierID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CartonShipment is one row of live's ln «محموله‌های قبلی» list and
// the createShipment response: shipment_code is the hand-written carton
// code, item_count is the number of variant lines (live renders
// «{item_count} مدل»).
type CartonShipment struct {
	ID           string    `json:"id"`
	ShipmentCode string    `json:"shipment_code"`
	ItemCount    int       `json:"item_count"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}

// CartonShipmentItem is one createShipment line.
type CartonShipmentItem struct {
	VariantID   string `json:"variant_id"`
	ExpectedQty int    `json:"expected_qty"`
}

// cartonShipmentCode alphabet omits confusables (0/O, 1/I) because the
// code is hand-written on cartons. Live's exact code format is unproven;
// this 8-character shape is provisional.
const cartonShipmentCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func newCartonShipmentCode() string {
	var b [8]byte
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return ""
	}
	for i, v := range raw {
		b[i] = cartonShipmentCodeAlphabet[int(v)%len(cartonShipmentCodeAlphabet)]
	}
	return string(b[:])
}

// CreateCartonShipment inserts a carton shipment (status 'draft', the
// only fresh state the source CHECK allows; live's an map also covers
// draft as «پیش‌نویس») with its variant lines. Variants must belong to
// this supplier's products. Idempotency is per-call: live's client sends
// no key for createShipment, so no replay handling is added.
func (r *SupplierRepository) CreateCartonShipment(ctx context.Context, supplierID string, items []CartonShipmentItem) (*CartonShipment, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("at least one variant line is required")
	}
	for _, it := range items {
		if it.VariantID == "" || it.ExpectedQty <= 0 {
			return nil, fmt.Errorf("invalid shipment line")
		}
		var owned bool
		if err := r.db.QueryRowContext(ctx, `
			SELECT EXISTS(SELECT 1 FROM product_variants pv
				JOIN products p ON p.id = pv.product_id
				WHERE pv.id = $1 AND p.supplier_id = $2)
		`, it.VariantID, supplierID).Scan(&owned); err != nil {
			return nil, err
		}
		if !owned {
			return nil, sql.ErrNoRows
		}
	}
	var id, code string
	inserted := false
	for attempt := 0; attempt < 5; attempt++ {
		code = newCartonShipmentCode()
		if code == "" {
			return nil, fmt.Errorf("could not generate shipment code")
		}
		id = uuid.NewString()
		_, err := r.db.ExecContext(ctx, `
			INSERT INTO shipments (id, shipment_code, supplier_id, status)
			VALUES ($1, $2, $3, 'draft')
		`, id, code, supplierID)
		if err == nil {
			inserted = true
			break
		}
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			continue
		}
		return nil, err
	}
	if !inserted {
		return nil, fmt.Errorf("could not generate a unique shipment code")
	}
	for _, it := range items {
		if _, err := r.db.ExecContext(ctx, `
			INSERT INTO shipment_items (shipment_id, variant_id, expected_qty)
			VALUES ($1, $2, $3)
		`, id, it.VariantID, it.ExpectedQty); err != nil {
			return nil, err
		}
	}
	var out CartonShipment
	err := r.db.QueryRowContext(ctx, `
		SELECT s.id, s.shipment_code,
			(SELECT COUNT(*) FROM shipment_items WHERE shipment_id = s.id),
			s.status, s.created_at
		FROM shipments s WHERE s.id = $1
	`, id).Scan(&out.ID, &out.ShipmentCode, &out.ItemCount, &out.Status, &out.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListCartonShipments returns this supplier's carton shipments, newest
// first, each with its variant-line count.
func (r *SupplierRepository) ListCartonShipments(ctx context.Context, supplierID string) ([]CartonShipment, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.id, s.shipment_code,
			(SELECT COUNT(*) FROM shipment_items WHERE shipment_id = s.id),
			s.status, s.created_at
		FROM shipments s WHERE s.supplier_id = $1
		ORDER BY s.created_at DESC
	`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CartonShipment{}
	for rows.Next() {
		var c CartonShipment
		if err := rows.Scan(&c.ID, &c.ShipmentCode, &c.ItemCount, &c.Status, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}


// WarehouseShipmentItemDto is one line of a warehouse shipment lookup: the
// fields live's receive tab renders, plus the stored receive state.
type WarehouseShipmentItemDto struct {
	VariantID    string `json:"variant_id"`
	VariantName  string `json:"variant_name"`
	ProductTitle string `json:"product_title"`
	SPNC         string `json:"spnc"`
	ExpectedQty  int    `json:"expected_qty"`
	ReceivedQty  int    `json:"received_qty"`
	QCStatus     string `json:"qc_status"`
}

// WarehouseShipmentDto is the GET /warehouse/shipments/{code} response
// shape live reads: { shipment: {...} }.
type WarehouseShipmentDto struct {
	ShipmentCode string                     `json:"shipment_code"`
	SupplierName string                     `json:"supplier_name"`
	Status       string                     `json:"status"`
	Items        []WarehouseShipmentItemDto `json:"items"`
}

// GetWarehouseShipmentByCode returns a supplier carton shipment by its
// hand-written code, for the warehouse receive tab. Codes match
// case-insensitively because staff type or scan them by hand.
func (r *SupplierRepository) GetWarehouseShipmentByCode(ctx context.Context, code string) (*WarehouseShipmentDto, error) {
	var out WarehouseShipmentDto
	var shipmentID, supplierID string
	err := r.db.QueryRowContext(ctx, `
		SELECT s.id, s.shipment_code, s.status, s.supplier_id
		FROM shipments s WHERE UPPER(s.shipment_code) = UPPER($1)
	`, code).Scan(&shipmentID, &out.ShipmentCode, &out.Status, &supplierID)
	if err != nil {
		return nil, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(name, '') FROM suppliers WHERE id = $1`, supplierID).Scan(&out.SupplierName); err != nil {
		out.SupplierName = ""
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT si.variant_id, pv.variant_name, p.title, pv.spnc, si.expected_qty, si.received_qty, si.qc_status
		FROM shipment_items si
		JOIN product_variants pv ON pv.id = si.variant_id
		JOIN products p ON p.id = pv.product_id
		WHERE si.shipment_id = $1
		ORDER BY pv.variant_name ASC
	`, shipmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out.Items = []WarehouseShipmentItemDto{}
	for rows.Next() {
		var item WarehouseShipmentItemDto
		if err := rows.Scan(&item.VariantID, &item.VariantName, &item.ProductTitle, &item.SPNC, &item.ExpectedQty, &item.ReceivedQty, &item.QCStatus); err != nil {
			return nil, err
		}
		out.Items = append(out.Items, item)
	}
	return &out, rows.Err()
}

// WarehouseReceiveItem is one line of the live receive request.
type WarehouseReceiveItem struct {
	VariantID   string `json:"variant_id"`
	ReceivedQty int    `json:"received_qty"`
	QCStatus    string `json:"qc_status"`
}

// ReceiveWarehouseShipment records a warehouse receipt of a carton shipment.
// Live sends qc_status "pass"/"fail"; the schema stores "passed"/"failed".
// Status becomes 'received' when every line is fully received, otherwise
// 'partially_received' (the schema CHECK has no 'qc_passed' state, unlike
// live's UI). An already-'received' shipment is rejected, matching live's
// UI disabling the form for received shipments.
func (r *SupplierRepository) ReceiveWarehouseShipment(ctx context.Context, code string, items []WarehouseReceiveItem) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("at least one received item is required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var shipmentID, status string
	err = tx.QueryRowContext(ctx, `SELECT id, status FROM shipments WHERE UPPER(shipment_code) = UPPER($1) FOR UPDATE`, code).Scan(&shipmentID, &status)
	if err != nil {
		return "", err
	}
	if status == "received" {
		return "", fmt.Errorf("shipment already received")
	}
	for _, item := range items {
		var stored string
		switch item.QCStatus {
		case "pass", "passed":
			stored = "passed"
		case "fail", "failed":
			stored = "failed"
		default:
			return "", fmt.Errorf("invalid qc_status for variant %s", item.VariantID)
		}
		if item.ReceivedQty < 0 {
			return "", fmt.Errorf("invalid received_qty for variant %s", item.VariantID)
		}
		res, err := tx.ExecContext(ctx, `UPDATE shipment_items SET received_qty = $1, qc_status = $2 WHERE shipment_id = $3 AND variant_id = $4`, item.ReceivedQty, stored, shipmentID, item.VariantID)
		if err != nil {
			return "", err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "", fmt.Errorf("variant %s is not part of shipment %s", item.VariantID, code)
		}
	}
	var total, fully int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE received_qty >= expected_qty) FROM shipment_items WHERE shipment_id = $1`, shipmentID).Scan(&total, &fully); err != nil {
		return "", err
	}
	newStatus := "partially_received"
	if total > 0 && fully == total {
		newStatus = "received"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE shipments SET status = $1, received_at = NOW() WHERE id = $2`, newStatus, shipmentID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return newStatus, nil
}

// SupplierProductVariant is one variant of live's Fl modal (variant_name +
// spnc + stock + attributes + is_default + cart limits).
type SupplierProductVariant struct {
	ID          string            `json:"id"`
	VariantName string            `json:"variant_name"`
	SPNC        string            `json:"spnc"`
	Stock       int               `json:"stock"`
	Attributes  map[string]string `json:"attributes"`
	IsDefault   bool              `json:"is_default"`
	MinCartQty  *int              `json:"min_cart_qty"`
	MaxCartQty  *int              `json:"max_cart_qty"`
	// SupplierBaseCost is the variant's own cost C (product_variants.
	// supplier_base_cost, gap 10); nil = inherit the product's cost.
	SupplierBaseCost *int64 `json:"supplier_base_cost"`
}

func (r *SupplierRepository) ListProductVariants(ctx context.Context, supplierID, productID string) ([]SupplierProductVariant, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT pv.id, pv.variant_name, pv.spnc, pv.stock, pv.attributes, pv.is_default, pv.min_cart_qty, pv.max_cart_qty, pv.supplier_base_cost
		FROM product_variants pv
		JOIN products p ON p.id = pv.product_id
		WHERE pv.product_id = $1 AND p.supplier_id = $2
		ORDER BY pv.created_at ASC
	`, productID, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierProductVariant{}
	for rows.Next() {
		var v SupplierProductVariant
		var attrs []byte
		var minQ, maxQ, cost sql.NullInt64
		if err := rows.Scan(&v.ID, &v.VariantName, &v.SPNC, &v.Stock, &attrs, &v.IsDefault, &minQ, &maxQ, &cost); err != nil {
			return nil, err
		}
		v.Attributes = map[string]string{}
		if len(attrs) > 0 {
			_ = json.Unmarshal(attrs, &v.Attributes)
		}
		if minQ.Valid {
			n := int(minQ.Int64)
			v.MinCartQty = &n
		}
		if maxQ.Valid {
			n := int(maxQ.Int64)
			v.MaxCartQty = &n
		}
		if cost.Valid {
			n := cost.Int64
			v.SupplierBaseCost = &n
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// FIX 47 — live gallery modal (Ll) + variants modal (Fl) backend.
// Gallery rule (live): at most 8 images per product; the FIRST image
// (sort_order 0) is the cover shown in lists. Cover change and reorder
// only rewrite sort_order; delete re-sequences so the invariant holds.
// Variant/label writes apply immediately: live's server may queue such
// changes for manager review (its pendingReview flag), but no review
// queue for gallery/variant changes exists in this codebase, so the
// frontend's pendingReview branch is kept for a future server that
// returns it. Documented divergence, not a UI gap.
type SupplierProductImage struct {
	ID        string `json:"id"`
	ImageURL  string `json:"imageUrl"`
	SortOrder int    `json:"sortOrder"`
}

// ErrGalleryFull is returned when an upload would exceed live's 8-image cap.
var ErrGalleryFull = errors.New("gallery full")

func (r *SupplierRepository) supplierOwnsProduct(ctx context.Context, supplierID, productID string) (bool, error) {
	var ok bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id = $1 AND supplier_id = $2)`, productID, supplierID).Scan(&ok)
	return ok, err
}

func (r *SupplierRepository) ListProductImages(ctx context.Context, supplierID, productID string) ([]SupplierProductImage, error) {
	owns, err := r.supplierOwnsProduct(ctx, supplierID, productID)
	if err != nil {
		return nil, err
	}
	if !owns {
		return nil, sql.ErrNoRows
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, image_url, sort_order FROM product_images WHERE product_id = $1 ORDER BY sort_order ASC, created_at ASC`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierProductImage{}
	for rows.Next() {
		var im SupplierProductImage
		if err := rows.Scan(&im.ID, &im.ImageURL, &im.SortOrder); err != nil {
			return nil, err
		}
		out = append(out, im)
	}
	return out, rows.Err()
}

func (r *SupplierRepository) AddProductImages(ctx context.Context, supplierID, productID string, urls []string) ([]SupplierProductImage, error) {
	owns, err := r.supplierOwnsProduct(ctx, supplierID, productID)
	if err != nil {
		return nil, err
	}
	if !owns {
		return nil, sql.ErrNoRows
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM product_images WHERE product_id = $1`, productID).Scan(&count); err != nil {
		return nil, err
	}
	if count+len(urls) > 8 {
		return nil, ErrGalleryFull
	}
	for k, u := range urls {
		if _, err := tx.ExecContext(ctx, `INSERT INTO product_images (product_id, image_url, sort_order) VALUES ($1, $2, $3)`, productID, u, count+k); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.ListProductImages(ctx, supplierID, productID)
}

func (r *SupplierRepository) resequenceGalleryTx(ctx context.Context, tx *sql.Tx, productID string, orderedIDs []string) error {
	for k, id := range orderedIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE product_images SET sort_order = $1 WHERE id = $2 AND product_id = $3`, k, id, productID); err != nil {
			return err
		}
	}
	return nil
}

func (r *SupplierRepository) galleryOrderedIDs(ctx context.Context, supplierID, productID string) ([]string, error) {
	imgs, err := r.ListProductImages(ctx, supplierID, productID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(imgs))
	for _, im := range imgs {
		ids = append(ids, im.ID)
	}
	return ids, nil
}

func (r *SupplierRepository) DeleteProductImage(ctx context.Context, supplierID, productID, imageID string) error {
	ids, err := r.galleryOrderedIDs(ctx, supplierID, productID)
	if err != nil {
		return err
	}
	found := false
	rest := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == imageID {
			found = true
			continue
		}
		rest = append(rest, id)
	}
	if !found {
		return sql.ErrNoRows
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM product_images WHERE id = $1 AND product_id = $2`, imageID, productID); err != nil {
		return err
	}
	if err := r.resequenceGalleryTx(ctx, tx, productID, rest); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SupplierRepository) SetProductCoverImage(ctx context.Context, supplierID, productID, imageID string) error {
	ids, err := r.galleryOrderedIDs(ctx, supplierID, productID)
	if err != nil {
		return err
	}
	found := false
	rest := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == imageID {
			found = true
			continue
		}
		rest = append(rest, id)
	}
	if !found {
		return sql.ErrNoRows
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := r.resequenceGalleryTx(ctx, tx, productID, append([]string{imageID}, rest...)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SupplierRepository) ReorderProductImages(ctx context.Context, supplierID, productID string, orderedIDs []string) error {
	ids, err := r.galleryOrderedIDs(ctx, supplierID, productID)
	if err != nil {
		return err
	}
	if len(orderedIDs) != len(ids) {
		return fmt.Errorf("ordered ids do not match gallery")
	}
	seen := map[string]bool{}
	for _, id := range orderedIDs {
		seen[id] = true
	}
	for _, id := range ids {
		if !seen[id] {
			return fmt.Errorf("ordered ids do not match gallery")
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := r.resequenceGalleryTx(ctx, tx, productID, orderedIDs); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateProductVariant inserts one variant with a fresh unique SPNC.
func (r *SupplierRepository) CreateProductVariant(ctx context.Context, supplierID, productID, name string, attributes map[string]string) (SupplierProductVariant, error) {
	var out SupplierProductVariant
	owns, err := r.supplierOwnsProduct(ctx, supplierID, productID)
	if err != nil {
		return out, err
	}
	if !owns {
		return out, sql.ErrNoRows
	}
	attrJSON, err := json.Marshal(attributes)
	if err != nil {
		return out, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	spnc, err := uniqueSPNCTx(ctx, tx)
	if err != nil {
		return out, err
	}
	var id string
	if err := tx.QueryRowContext(ctx, `INSERT INTO product_variants (product_id, spnc, variant_name, attributes, is_default, stock) VALUES ($1, $2, $3, $4::jsonb, FALSE, 0) RETURNING id`, productID, spnc, name, string(attrJSON)).Scan(&id); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	out = SupplierProductVariant{ID: id, VariantName: name, SPNC: spnc, Stock: 0, Attributes: attributes, IsDefault: false}
	return out, nil
}

// CreateProductVariantsBulk inserts many variants in one transaction.
// Live caps a bulk request at 24 rows and rejects duplicate or
// already-registered names; the frontend validates the same rules first.
func (r *SupplierRepository) CreateProductVariantsBulk(ctx context.Context, supplierID, productID string, variants []SupplierProductVariant) (int, error) {
	owns, err := r.supplierOwnsProduct(ctx, supplierID, productID)
	if err != nil {
		return 0, err
	}
	if !owns {
		return 0, sql.ErrNoRows
	}
	if len(variants) == 0 || len(variants) > 24 {
		return 0, fmt.Errorf("bulk variant count out of range")
	}
	seen := map[string]bool{}
	for _, v := range variants {
		if strings.TrimSpace(v.VariantName) == "" {
			return 0, fmt.Errorf("variant name required")
		}
		if seen[v.VariantName] {
			return 0, fmt.Errorf("duplicate variant name: %s", v.VariantName)
		}
		seen[v.VariantName] = true
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT variant_name FROM product_variants WHERE product_id = $1`, productID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return 0, err
		}
		if seen[name] {
			rows.Close()
			return 0, fmt.Errorf("variant already exists: %s", name)
		}
	}
	rows.Close()
	for _, v := range variants {
		attrJSON, err := json.Marshal(v.Attributes)
		if err != nil {
			return 0, err
		}
		spnc, err := uniqueSPNCTx(ctx, tx)
		if err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO product_variants (product_id, spnc, variant_name, attributes, is_default, stock) VALUES ($1, $2, $3, $4::jsonb, FALSE, 0)`, productID, spnc, v.VariantName, string(attrJSON)); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(variants), nil
}

// UpdateVariantCartLimits sets a variant's per-basket min/max quantities.
func (r *SupplierRepository) UpdateVariantCartLimits(ctx context.Context, supplierID, productID, variantID string, minQty, maxQty *int) (SupplierProductVariant, error) {
	var out SupplierProductVariant
	owns, err := r.supplierOwnsProduct(ctx, supplierID, productID)
	if err != nil {
		return out, err
	}
	if !owns {
		return out, sql.ErrNoRows
	}
	var minQ, maxQ sql.NullInt64
	if minQty != nil {
		minQ = sql.NullInt64{Int64: int64(*minQty), Valid: true}
	}
	if maxQty != nil {
		maxQ = sql.NullInt64{Int64: int64(*maxQty), Valid: true}
	}
	err = r.db.QueryRowContext(ctx, `UPDATE product_variants SET min_cart_qty = $1, max_cart_qty = $2 WHERE id = $3 AND product_id = $4 RETURNING id, variant_name, spnc, stock, attributes, is_default, min_cart_qty, max_cart_qty`, minQ, maxQ, variantID, productID).Scan(&out.ID, &out.VariantName, &out.SPNC, &out.Stock, new([]byte), &out.IsDefault, &minQ, &maxQ)
	if err != nil {
		return out, err
	}
	// Re-read attributes + limits cleanly (the RETURNING scan above
	// discards attributes; limits are re-derived here for clarity).
	var attrs []byte
	var mn, mx sql.NullInt64
	if err := r.db.QueryRowContext(ctx, `SELECT attributes, min_cart_qty, max_cart_qty FROM product_variants WHERE id = $1`, variantID).Scan(&attrs, &mn, &mx); err == nil {
		out.Attributes = map[string]string{}
		if len(attrs) > 0 {
			_ = json.Unmarshal(attrs, &out.Attributes)
		}
		if mn.Valid {
			n := int(mn.Int64)
			out.MinCartQty = &n
		}
		if mx.Valid {
			n := int(mx.Int64)
			out.MaxCartQty = &n
		}
	}
	return out, nil
}

// UpdateVariantPricing sets (or clears) a variant's own supplier base
// cost — gap 10: pricing has always READ product_variants.
// supplier_base_cost (NULL = inherit the product's cost), but no write
// path existed. Mirrors UpdateProductPricing: the product must belong
// to the caller (else sql.ErrNoRows, surfaced as 404), a set cost must
// be positive (the column's CHECK agrees), and the change is written
// to audit_logs with the caller's reason. clear=true restores
// inheritance by setting the column back to NULL; a non-nil cost wins
// over clear when both are sent.
// GetVariantEffectiveBaseCost returns a variant's own cost (nil when
// the variant inherits) and its EFFECTIVE cost — the variant's own
// supplier_base_cost when set, else the product's current cost, the
// inheritance every pricing read applies. The product must belong to
// the caller and the variant to the product; any miss is
// sql.ErrNoRows (the handler surfaces it as 404, matching
// UpdateVariantPricing).
func (r *SupplierRepository) GetVariantEffectiveBaseCost(ctx context.Context, supplierID, productID, variantID string) (own *int64, effective int64, err error) {
	var ownCost sql.NullInt64
	var productCost int64
	err = r.db.QueryRowContext(ctx, `
		SELECT pv.supplier_base_cost, p.supplier_base_cost
		FROM product_variants pv
		JOIN products p ON p.id = pv.product_id
		WHERE pv.id = $1 AND pv.product_id = $2 AND p.supplier_id = $3
	`, variantID, productID, supplierID).Scan(&ownCost, &productCost)
	if err != nil {
		return nil, 0, err
	}
	effective = productCost
	if ownCost.Valid {
		v := ownCost.Int64
		own = &v
		effective = v
	}
	return own, effective, nil
}

// GetVariantOwnAndProductCost returns a variant's own cost (nil when
// the variant inherits) alongside the product's current cost — the
// pair the variant-pricing handler compares to decide whether a
// clear-to-inherit LOWERS the effective cost (direct) or RAISES it to
// the product's cost (governed, migration 202611050010). Same
// ownership/miss semantics as GetVariantEffectiveBaseCost.
func (r *SupplierRepository) GetVariantOwnAndProductCost(ctx context.Context, supplierID, productID, variantID string) (own *int64, productCost int64, err error) {
	var ownCost sql.NullInt64
	err = r.db.QueryRowContext(ctx, `
		SELECT pv.supplier_base_cost, p.supplier_base_cost
		FROM product_variants pv
		JOIN products p ON p.id = pv.product_id
		WHERE pv.id = $1 AND pv.product_id = $2 AND p.supplier_id = $3
	`, variantID, productID, supplierID).Scan(&ownCost, &productCost)
	if err != nil {
		return nil, 0, err
	}
	if ownCost.Valid {
		v := ownCost.Int64
		own = &v
	}
	return own, productCost, nil
}

// GetSupplierVariant returns one variant of a supplier-owned product
// in ListProductVariants' row shape (used to answer the governed
// variant-pricing PUT with the variant's post-write state).
func (r *SupplierRepository) GetSupplierVariant(ctx context.Context, supplierID, productID, variantID string) (SupplierProductVariant, error) {
	var out SupplierProductVariant
	var attrs []byte
	var minQ, maxQ, cost sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT pv.id, pv.variant_name, pv.spnc, pv.stock, pv.attributes, pv.is_default, pv.min_cart_qty, pv.max_cart_qty, pv.supplier_base_cost
		FROM product_variants pv
		JOIN products p ON p.id = pv.product_id
		WHERE pv.id = $1 AND pv.product_id = $2 AND p.supplier_id = $3
	`, variantID, productID, supplierID).Scan(&out.ID, &out.VariantName, &out.SPNC, &out.Stock, &attrs, &out.IsDefault, &minQ, &maxQ, &cost)
	if err != nil {
		return out, err
	}
	out.Attributes = map[string]string{}
	if len(attrs) > 0 {
		_ = json.Unmarshal(attrs, &out.Attributes)
	}
	if minQ.Valid {
		n := int(minQ.Int64)
		out.MinCartQty = &n
	}
	if maxQ.Valid {
		n := int(maxQ.Int64)
		out.MaxCartQty = &n
	}
	if cost.Valid {
		n := cost.Int64
		out.SupplierBaseCost = &n
	}
	return out, nil
}

func (r *SupplierRepository) UpdateVariantPricing(ctx context.Context, supplierID, productID, variantID string, newBaseCost *int64, clear bool, reason string) (SupplierProductVariant, error) {
	var out SupplierProductVariant
	owns, err := r.supplierOwnsProduct(ctx, supplierID, productID)
	if err != nil {
		return out, err
	}
	if !owns {
		return out, sql.ErrNoRows
	}
	var costArg sql.NullInt64
	if newBaseCost != nil {
		if *newBaseCost <= 0 {
			return out, fmt.Errorf("supplier base cost must be positive")
		}
		costArg = sql.NullInt64{Int64: *newBaseCost, Valid: true}
	} else if !clear {
		return out, fmt.Errorf("supplier base cost must be positive")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var attrs []byte
	var minQ, maxQ, cost sql.NullInt64
	err = tx.QueryRowContext(ctx, `UPDATE product_variants SET supplier_base_cost = $1 WHERE id = $2 AND product_id = $3 RETURNING id, variant_name, spnc, stock, attributes, is_default, min_cart_qty, max_cart_qty, supplier_base_cost`, costArg, variantID, productID).Scan(&out.ID, &out.VariantName, &out.SPNC, &out.Stock, &attrs, &out.IsDefault, &minQ, &maxQ, &cost)
	if err != nil {
		return out, err
	}
	out.Attributes = map[string]string{}
	if len(attrs) > 0 {
		_ = json.Unmarshal(attrs, &out.Attributes)
	}
	if minQ.Valid {
		n := int(minQ.Int64)
		out.MinCartQty = &n
	}
	if maxQ.Valid {
		n := int(maxQ.Int64)
		out.MaxCartQty = &n
	}
	if cost.Valid {
		n := cost.Int64
		out.SupplierBaseCost = &n
	}
	details := map[string]interface{}{
		"product_id": productID, "variant_id": variantID, "reason": reason,
	}
	if newBaseCost != nil {
		details["new_supplier_base_cost"] = *newBaseCost
	} else {
		details["cleared"] = true
	}
	if err := insertSupplierAuditTx(ctx, tx, supplierID, "supplier_variant_pricing_update", details); err != nil {
		return out, err
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	return out, nil
}

// SupplierVariantLabel is one row of live's Fl print-label sheet.
type SupplierVariantLabel struct {
	SPNC         string `json:"spnc"`
	VariantName  string `json:"variant_name"`
	QRCode       string `json:"qr_payload"`
}

// GetProductLabels returns live's getLabels shape {spn, labels}.
// spn is the products.spn column (live's exact labels.spn value is
// unproven — it could be spn_number; documented provisional).
// qr_payload is provisionally the SPNC itself: live's payload format
// is unproven and no QR library exists in this codebase (same known
// gap as Fix 46's carton print).
func (r *SupplierRepository) GetProductLabels(ctx context.Context, supplierID, productID string) (string, []SupplierVariantLabel, error) {
	var spn sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT spn FROM products WHERE id = $1 AND supplier_id = $2`, productID, supplierID).Scan(&spn)
	if err != nil {
		return "", nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT variant_name, spnc FROM product_variants WHERE product_id = $1 ORDER BY created_at ASC`, productID)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	labels := []SupplierVariantLabel{}
	for rows.Next() {
		var l SupplierVariantLabel
		if err := rows.Scan(&l.VariantName, &l.SPNC); err != nil {
			return "", nil, err
		}
		l.QRCode = l.SPNC
		labels = append(labels, l)
	}
	if err := rows.Err(); err != nil {
		return "", nil, err
	}
	return spn.String, labels, nil
}

// GetInboundByID retrieves a specific inbound
func (r *SupplierRepository) GetInboundByID(ctx context.Context, inboundID string) (*InboundShipment, error) {
	query := `
		SELECT id, supplier_id, supplier_name, warehouse_name, items_count, cartons_count,
		       COALESCE(carrier, ''), COALESCE(truck_plate, ''), status, created_at, received_at,
		       COALESCE(items, '[]'::jsonb), dispatch_at, dispatched_at
		FROM inbound_shipments
		WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, inboundID)
	var s InboundShipment
	if err := row.Scan(
		&s.ID, &s.SupplierID, &s.SupplierName, &s.WarehouseName, &s.ItemsCount, &s.CartonsCount,
		&s.Carrier, &s.TruckPlate, &s.Status, &s.CreatedAt, &s.ReceivedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// UpdateProductSupplyOption updates only the supplier fulfillment option.
func (r *SupplierRepository) UpdateProductSupplyOption(ctx context.Context, supplierID, productID, supplyOption string) error {
	allowed := map[string]bool{"3hour_local": true, "1day": true, "3day": true}
	if !allowed[supplyOption] {
		return fmt.Errorf("invalid supply_option: %s", supplyOption)
	}
	result, err := r.db.ExecContext(ctx, `UPDATE products SET supply_option = $1, updated_at = NOW() WHERE id = $2 AND supplier_id = $3`, supplyOption, productID, supplierID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("product %s not found", productID)
	}
	return nil
}

// CreateCampaign starts a promotional campaign
func (r *SupplierRepository) CreateCampaign(ctx context.Context, supplierID string, data map[string]interface{}) (string, error) {
	campaignID := "cmp-" + uuid.NewString()
	campaignName, _ := data["campaign_name"].(string)
	b, _ := data["budget"].(float64)
	c, _ := data["cpc_bid"].(float64)
	if strings.TrimSpace(campaignName) == "" || len(campaignName) > 200 || b <= 0 || c <= 0 || b > 9007199254740991 || c > b || b != math.Trunc(b) || c != math.Trunc(c) {
		return "", fmt.Errorf("نام کمپین، بودجه یا مبلغ پیشنهادی نامعتبر است")
	}
	budget, cpcBid := int64(b), int64(c)
	ids, ok := data["product_ids"].([]interface{})
	if !ok || len(ids) == 0 {
		return "", fmt.Errorf("برای کمپین حداقل یک کالا انتخاب کنید")
	}
	for _, raw := range ids {
		id, ok := raw.(string)
		if !ok || id == "" {
			return "", fmt.Errorf("کالای کمپین نامعتبر است")
		}
		var owned bool
		if err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id = $1 AND supplier_id = $2)`, id, supplierID).Scan(&owned); err != nil {
			return "", err
		}
		if !owned {
			return "", fmt.Errorf("این کالا متعلق به تأمین‌کننده شما نیست")
		}
	}

	prodBytes, _ := json.Marshal(data["product_ids"])
	catBytes, _ := json.Marshal(data["target_categories"])

	query := `
		INSERT INTO supplier_campaigns (id, supplier_id, campaign_name, product_ids, budget, spent_budget, target_categories, cpc_bid, impressions, clicks, status, created_at)
		VALUES ($1, $2, $3, $4, $5, 0, $6, $7, 0, 0, 'active', NOW())
	`
	_, err := r.db.ExecContext(ctx, query, campaignID, supplierID, campaignName, prodBytes, budget, catBytes, cpcBid)
	if err != nil {
		return "", err
	}
	return campaignID, nil
}

// GetCampaignStats returns impressions, clicks and CTR
func (r *SupplierRepository) GetCampaignStats(ctx context.Context, supplierID, campaignID string) (*CampaignStats, error) {
	query := `
		SELECT id, impressions, clicks, spent_budget
		FROM supplier_campaigns
		WHERE id = $1 AND supplier_id = $2
	`
	row := r.db.QueryRowContext(ctx, query, campaignID, supplierID)
	var stats CampaignStats
	if err := row.Scan(&stats.CampaignID, &stats.Impressions, &stats.Clicks, &stats.SpentBudget); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if stats.Impressions > 0 {
		stats.CTR = float64(stats.Clicks) / float64(stats.Impressions) * 100
	}
	return &stats, nil
}

// GetSalesReport calculates total revenue and sales for supplier
func (r *SupplierRepository) GetSalesReport(ctx context.Context, supplierID, startDate, endDate string) (*SupplierSalesReport, error) {
	var report SupplierSalesReport
	query := `
		SELECT COALESCE(SUM(oi.wholesale_cost * oi.quantity), 0),
		       COALESCE(SUM(oi.quantity), 0),
		       COALESCE(COUNT(DISTINCT oi.order_id), 0)
		FROM order_items oi
		JOIN products p ON oi.product_id = p.id
		JOIN orders o ON oi.order_id = o.id
		WHERE p.supplier_id = $1 AND o.payment_status = 'paid' AND o.status <> 'cancelled'
		AND ($2 = '' OR o.created_at >= NULLIF($2, '')::date)
		AND ($3 = '' OR o.created_at < NULLIF($3, '')::date + INTERVAL '1 day')
	`
	err := r.db.QueryRowContext(ctx, query, supplierID, startDate, endDate).Scan(&report.TotalRevenue, &report.ItemsSold, &report.OrdersCount)
	if err != nil {
		return nil, err
	}
	if report.OrdersCount > 0 {
		report.AverageOrder = report.TotalRevenue / int64(report.OrdersCount)
	}
	return &report, nil
}

// GetDiscrepancies returns QC issues for supplier
func (r *SupplierRepository) GetDiscrepancies(ctx context.Context, supplierID string) ([]QCDiscrepancy, error) {
	query := `
		SELECT qc.id, qc.inbound_id, qc.supplier_id, qc.product_id, COALESCE(p.title, '') AS product_title,
		       qc.expected_quantity, qc.received_quantity,
		       qc.defect_count, qc.reason, qc.status, qc.supplier_response, qc.resolution_offer, qc.created_at, qc.resolved_at
		FROM qc_discrepancies qc
		LEFT JOIN products p ON p.id = qc.product_id
		WHERE qc.supplier_id = $1 OR $1 = 'all'
		ORDER BY qc.created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []QCDiscrepancy{}
	for rows.Next() {
		var d QCDiscrepancy
		if err := rows.Scan(
			&d.ID, &d.InboundID, &d.SupplierID, &d.ProductID, &d.ProductTitle, &d.ExpectedQuantity, &d.ReceivedQuantity,
			&d.DefectCount, &d.Reason, &d.Status, &d.SupplierResponse, &d.ResolutionOffer, &d.CreatedAt, &d.ResolvedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}

// RespondDiscrepancy updates a QC discrepancy
func (r *SupplierRepository) RespondDiscrepancy(ctx context.Context, supplierID, discrepancyID, response, resolutionOffer string) error {
	query := `
		UPDATE qc_discrepancies
		SET supplier_response = $1, resolution_offer = $2, status = 'resolved', resolved_at = NOW()
		WHERE id = $3 AND supplier_id = $4
	`
	res, err := r.db.ExecContext(ctx, query, response, resolutionOffer, discrepancyID, supplierID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RequestSettlement submits payout request
func (r *SupplierRepository) RequestSettlement(ctx context.Context, supplierID string, amount int64, shaba, bankName, accountHolder string) (string, error) {
	if amount <= 0 {
		return "", fmt.Errorf("settlement amount must be positive")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var locked string
	var approvalStatus string
	if err := tx.QueryRowContext(ctx, `SELECT id, approval_status FROM suppliers WHERE id = $1 FOR UPDATE`, supplierID).Scan(&locked, &approvalStatus); err != nil {
		return "", err
	}
	// Gap 12: a suspended supplier cannot request NEW settlements
	// (already-pending requests stay admin-reviewable). The balance
	// computation itself is untouched: Earned keeps accruing on read.
	if approvalStatus == "suspended" {
		return "", ErrSupplierSuspended
	}
	var balance SupplierBalance
	if err := scanSupplierBalance(ctx, tx, supplierID, &balance); err != nil {
		return "", err
	}
	if amount > balance.Available {
		return "", fmt.Errorf("insufficient settlement balance")
	}
	requestID := "pay-" + uuid.NewString()
	query := `
		INSERT INTO payout_requests (id, store_id, supplier_id, recipient_type, recipient_name, shaba, bank_name, amount, status, created_at)
		VALUES ($1, NULL, $2, 'supplier', $3, $4, $5, $6, 'pending', NOW())
	`
	_, err = tx.ExecContext(ctx, query, requestID, supplierID, accountHolder, shaba, bankName, amount)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return requestID, nil
}

// GetSettlementHistory returns settlement payouts
func (r *SupplierRepository) GetSettlementHistory(ctx context.Context, supplierID string) ([]map[string]interface{}, error) {
	query := `
		SELECT id, amount, status, shaba, bank_name, recipient_name, created_at, processed_at, transfer_reference
		FROM payout_requests
		WHERE supplier_id = $1 OR $1 = 'all'
		ORDER BY created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []map[string]interface{}{}
	for rows.Next() {
		var id, status, shabaVal, bankName, recipientName string
		var amount int64
		var createdAt time.Time
		var processedAt *time.Time
		var transferRef *string
		if err := rows.Scan(&id, &amount, &status, &shabaVal, &bankName, &recipientName, &createdAt, &processedAt, &transferRef); err != nil {
			return nil, err
		}
		item := map[string]interface{}{
			"id":             id,
			"amount":         amount,
			"status":         status,
			"shaba":          shabaVal,
			"iban":           shabaVal,
			"bank_name":      bankName,
			"account_holder": recipientName,
			"recipient_name": recipientName,
			"created_at":     createdAt,
		}
		if processedAt != nil {
			item["processed_at"] = processedAt
		}
		if transferRef != nil && *transferRef != "" {
			item["transfer_reference"] = *transferRef
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

// ---------------------------------------------------------------------------
// Live-parity supplier reads (Team 06, Phase 2). These queries read only
// tables/columns that already exist in the source migrations; they invent
// no new schema. tapin_token is selected only by GetSupplierByID (for
// the server-side Tapin connectivity test) and never leaves the server.
// Bodies/responses of the live p4a routes are largely unverified (U07);
// functions below are therefore limited to what the DB evidence confirms.
// ---------------------------------------------------------------------------

// ListSuppliersForAdminDetail returns every supplier with the D55 record
// fields (approval, contract version, markup, warehouse-city provenance).
// It backs GET /api/v1/admin/suppliers/list and /records (D15); the two
// routes share this data and differ only in the fields the handlers emit.
func (r *SupplierRepository) ListSuppliersForAdminDetail(ctx context.Context) ([]domain.Supplier, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, owner_user_id, name, COALESCE(logo_url, ''), rating, total_products, delivery_score,
		       warehouse_city, COALESCE(description, ''), COALESCE(badge, ''), created_at,
		       COALESCE(supports_direct_shipping, FALSE), default_markup_type, default_markup_value,
		       COALESCE(signed_contract_version, 1), approval_status, approved_by, approved_at,
		       rejection_reason, tapin_shop_id, COALESCE(warehouse_city_confirmed, FALSE),
		       warehouse_city_source, suspended_at, suspended_by, suspension_reason
		FROM suppliers
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Supplier{}
	for rows.Next() {
		var s domain.Supplier
		var ownerUID, approvedBy, rejectionReason, tapinShopID, warehouseCitySource, suspendedBy, suspensionReason sql.NullString
		var approvedAt, suspendedAt sql.NullTime
		if err := rows.Scan(&s.ID, &ownerUID, &s.Name, &s.LogoURL, &s.Rating, &s.TotalProducts,
			&s.DeliveryScore, &s.WarehouseCity, &s.Description, &s.Badge, &s.CreatedAt,
			&s.SupportsDirectShipping, &s.DefaultMarkupType, &s.DefaultMarkupValue,
			&s.SignedContractVersion, &s.ApprovalStatus, &approvedBy, &approvedAt,
			&rejectionReason, &tapinShopID, &s.WarehouseCityConfirmed, &warehouseCitySource,
			&suspendedAt, &suspendedBy, &suspensionReason); err != nil {
			return nil, err
		}
		if suspendedAt.Valid {
			s.SuspendedAt = &suspendedAt.Time
		}
		if suspendedBy.Valid {
			s.SuspendedBy = &suspendedBy.String
		}
		if suspensionReason.Valid {
			s.SuspensionReason = &suspensionReason.String
		}
		if ownerUID.Valid {
			s.OwnerUserID = &ownerUID.String
		}
		if approvedBy.Valid {
			s.ApprovedBy = &approvedBy.String
		}
		if approvedAt.Valid {
			s.ApprovedAt = &approvedAt.Time
		}
		if rejectionReason.Valid {
			s.RejectionReason = &rejectionReason.String
		}
		if tapinShopID.Valid {
			s.TapinShopID = &tapinShopID.String
		}
		if warehouseCitySource.Valid {
			s.WarehouseCitySource = &warehouseCitySource.String
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SupplierShippingMethod mirrors one row of supplier_shipping_methods.
// supplier_shipping_methods.owner_user_id holds the supplier OWNER's user id
// (migration 202610170012), so lookups join through suppliers.owner_user_id
// and also accept an owner id directly.
type SupplierShippingMethod struct {
	SupplierID  string     `json:"supplierId"`
	Method      string     `json:"method"`
	Status      string     `json:"status"`
	RequestedAt *time.Time `json:"requestedAt,omitempty"`
	DecidedAt   *time.Time `json:"decidedAt,omitempty"`
	DecidedBy   *string    `json:"decidedBy,omitempty"`
}

const supplierShippingMethodsWhere = `
	FROM supplier_shipping_methods ssm
	JOIN suppliers s ON s.owner_user_id::text = ssm.owner_user_id
	WHERE s.id = $1 OR ssm.owner_user_id = $1
`

func scanSupplierShippingMethods(rows *sql.Rows) ([]SupplierShippingMethod, error) {
	out := []SupplierShippingMethod{}
	for rows.Next() {
		var m SupplierShippingMethod
		var decidedBy sql.NullString
		if err := rows.Scan(&m.SupplierID, &m.Method, &m.Status, &m.RequestedAt, &m.DecidedAt, &decidedBy); err != nil {
			return nil, err
		}
		if decidedBy.Valid {
			m.DecidedBy = &decidedBy.String
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListSupplierShippingMethods returns the direct/warehouse method rows for
// one supplier (D11 supplier/shipping-methods).
func (r *SupplierRepository) ListSupplierShippingMethods(ctx context.Context, supplierID string) ([]SupplierShippingMethod, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT s.id, ssm.method, ssm.status, ssm.requested_at, ssm.decided_at, ssm.decided_by`+supplierShippingMethodsWhere+` ORDER BY ssm.method`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSupplierShippingMethods(rows)
}

// ListShippingMethodsBySupplierIDs returns the shipping-method
// status maps for many suppliers in ONE query, keyed
// supplierID -> method -> status — the exact shape
// routing.ResolveItemRouting consumes. It reuses the join shape of
// supplierShippingMethodsWhere (method rows are keyed by the
// supplier OWNER's user id, joined through suppliers). Suppliers
// with no method rows are absent from the result; callers treat
// absence as "no methods enabled" (the routing fail-safe).
func (r *SupplierRepository) ListShippingMethodsBySupplierIDs(ctx context.Context, supplierIDs []string) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	if len(supplierIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.id, ssm.method, ssm.status
		FROM supplier_shipping_methods ssm
		JOIN suppliers s ON s.owner_user_id::text = ssm.owner_user_id
		WHERE s.id = ANY($1::text[])
	`, pq.Array(supplierIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var supplierID, method, status string
		if err := rows.Scan(&supplierID, &method, &status); err != nil {
			return nil, err
		}
		m, ok := out[supplierID]
		if !ok {
			m = map[string]string{}
			out[supplierID] = m
		}
		m[method] = status
	}
	return out, rows.Err()
}

// ListAllSupplierShippingMethods returns every supplier's method rows for
// GET /api/v1/admin/suppliers/shipping-methods (D15).
func (r *SupplierRepository) ListAllSupplierShippingMethods(ctx context.Context) ([]SupplierShippingMethod, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.id, ssm.method, ssm.status, ssm.requested_at, ssm.decided_at, ssm.decided_by
		FROM supplier_shipping_methods ssm
		JOIN suppliers s ON s.owner_user_id::text = ssm.owner_user_id
		ORDER BY s.name, ssm.method
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSupplierShippingMethods(rows)
}

// RequestSupplierShippingMethod moves a method disabled -> requested, per
// the migration 202610170012 policy: suppliers may only REQUEST a method;
// only an admin may enable or disable it. A missing row is created as
// requested (new suppliers have no backfilled rows).
func (r *SupplierRepository) RequestSupplierShippingMethod(ctx context.Context, supplierID, method string) error {
	if method != "direct" && method != "warehouse" {
		return fmt.Errorf("invalid shipping method")
	}
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO supplier_shipping_methods (owner_user_id, method, status, requested_at)
		SELECT s.owner_user_id, $2, 'requested', NOW() FROM suppliers s WHERE s.id = $1
		ON CONFLICT (owner_user_id, method) DO UPDATE
		  SET status = 'requested', requested_at = NOW()
		  WHERE supplier_shipping_methods.status = 'disabled'
	`, supplierID, method)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("supplier not found or method already requested/enabled")
	}
	return nil
}

// DecideSupplierShippingMethod is the admin side of the same policy:
// requested -> enabled/disabled (and enabled -> disabled). It never
// accepts 'requested' from an admin decision.
func (r *SupplierRepository) DecideSupplierShippingMethod(ctx context.Context, supplierID, method, status, decidedBy string) error {
	if method != "direct" && method != "warehouse" {
		return fmt.Errorf("invalid shipping method")
	}
	if status != "enabled" && status != "disabled" {
		return fmt.Errorf("invalid shipping method status")
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE supplier_shipping_methods ssm SET status = $3, decided_at = NOW(), decided_by = $4
		FROM suppliers s
		WHERE s.owner_user_id::text = ssm.owner_user_id AND s.id = $1 AND ssm.method = $2
	`, supplierID, method, status, decidedBy)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("supplier shipping method not found")
	}
	return nil
}

// UpdateTapinShopID stores only the supplier's Tapin shop id (D11).
// The Tapin token is stored separately by UpdateTapinToken (Fix61);
// neither method ever returns a token, and no handler emits one.
func (r *SupplierRepository) UpdateTapinShopID(ctx context.Context, supplierID, shopID string) error {
	if strings.TrimSpace(shopID) == "" {
		res, err := r.db.ExecContext(ctx, `UPDATE suppliers SET tapin_shop_id = NULL WHERE id = $1`, supplierID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return sql.ErrNoRows
		}
		return nil
	}
	res, err := r.db.ExecContext(ctx, `UPDATE suppliers SET tapin_shop_id = $2 WHERE id = $1`, supplierID, shopID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpdateTapinToken stores the supplier's Tapin token (Fix61), mirroring
// UpdateTapinShopID. An empty token clears the stored value (NULL).
// The token is internal only: it is selected solely for the server-side
// connectivity test and is never emitted in any API response.
func (r *SupplierRepository) UpdateTapinToken(ctx context.Context, supplierID, token string) error {
	if strings.TrimSpace(token) == "" {
		res, err := r.db.ExecContext(ctx, `UPDATE suppliers SET tapin_token = NULL WHERE id = $1`, supplierID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return sql.ErrNoRows
		}
		return nil
	}
	res, err := r.db.ExecContext(ctx, `UPDATE suppliers SET tapin_token = $2 WHERE id = $1`, supplierID, token)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SupplierWarehouse mirrors supplier_warehouses (migration 202610280001).
type SupplierWarehouse struct {
	ID           string    `json:"id"`
	SupplierID   string    `json:"supplierId"`
	Name         string    `json:"name"`
	ProvinceName string    `json:"provinceName"`
	CityName     string    `json:"cityName"`
	Address      string    `json:"address,omitempty"`
	PostalCode   string    `json:"postalCode,omitempty"`
	Phone        string    `json:"phone,omitempty"`
	IsActive     bool      `json:"isActive"`
	CreatedAt    time.Time `json:"createdAt"`
}

// ListSupplierWarehouses returns the supplier's own warehouses
// (D12 supplier/warehouses).
func (r *SupplierRepository) ListSupplierWarehouses(ctx context.Context, supplierID string) ([]SupplierWarehouse, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, supplier_id, name, province_name, city_name,
		       COALESCE(address, ''), COALESCE(postal_code, ''), COALESCE(phone, ''),
		       is_active, created_at
		FROM supplier_warehouses WHERE supplier_id = $1 ORDER BY created_at DESC
	`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierWarehouse{}
	for rows.Next() {
		var w SupplierWarehouse
		if err := rows.Scan(&w.ID, &w.SupplierID, &w.Name, &w.ProvinceName, &w.CityName,
			&w.Address, &w.PostalCode, &w.Phone, &w.IsActive, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ErrWarehouseNameTaken is returned by Create/UpdateSupplierWarehouse when
// the UNIQUE(supplier_id, name) constraint of migration 202610280001 fires.
var ErrWarehouseNameTaken = errors.New("warehouse name already taken")

// CreateSupplierWarehouse inserts a supplier-owned warehouse (Fix 37, live
// parity: POST /supplier/warehouses). The id is server-minted 'swh-' + UUID
// per migration 202610280001; new warehouses start active (DB default TRUE).
func (r *SupplierRepository) CreateSupplierWarehouse(ctx context.Context, supplierID string, w SupplierWarehouse) (*SupplierWarehouse, error) {
	w.ID = "swh-" + uuid.NewString()
	w.SupplierID = supplierID
	w.IsActive = true
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO supplier_warehouses (id, supplier_id, name, province_name, city_name, address, postal_code, phone, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING created_at`,
		w.ID, supplierID, w.Name, w.ProvinceName, w.CityName, w.Address, w.PostalCode, w.Phone, true,
	).Scan(&w.CreatedAt)
	if err != nil {
		if pgErr, ok := err.(*pq.Error); ok && pgErr.Code == "23505" {
			return nil, ErrWarehouseNameTaken
		}
		return nil, err
	}
	return &w, nil
}

// UpdateSupplierWarehouse updates a supplier-owned warehouse (Fix 37, live
// parity: PUT /supplier/warehouses/{id}). Ownership is enforced by the
// supplier_id predicate; activation stays admin-controlled (live's form
// never edits isActive). Returns sql.ErrNoRows when the id is not the
// supplier's own.
func (r *SupplierRepository) UpdateSupplierWarehouse(ctx context.Context, supplierID, id string, w SupplierWarehouse) (*SupplierWarehouse, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE supplier_warehouses
		SET name = $1, province_name = $2, city_name = $3, address = $4, postal_code = $5, phone = $6, updated_at = NOW()
		WHERE id = $7 AND supplier_id = $8`,
		w.Name, w.ProvinceName, w.CityName, w.Address, w.PostalCode, w.Phone, id, supplierID)
	if err != nil {
		if pgErr, ok := err.(*pq.Error); ok && pgErr.Code == "23505" {
			return nil, ErrWarehouseNameTaken
		}
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, sql.ErrNoRows
	}
	var out SupplierWarehouse
	err = r.db.QueryRowContext(ctx, `
		SELECT id, supplier_id, name, province_name, city_name,
		       COALESCE(address, ''), COALESCE(postal_code, ''), COALESCE(phone, ''),
		       is_active, created_at
		FROM supplier_warehouses WHERE id = $1 AND supplier_id = $2`, id, supplierID,
	).Scan(&out.ID, &out.SupplierID, &out.Name, &out.ProvinceName, &out.CityName,
		&out.Address, &out.PostalCode, &out.Phone, &out.IsActive, &out.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &out, nil
}


// SupplierOrderAddon mirrors one row of live's GET /supplier/orders/{id}/addons.
type SupplierOrderAddon struct {
	Title        string `json:"title"`
	ProductTitle string `json:"productTitle"`
	FeePerUnit   int64  `json:"feePerUnit"`
	Quantity     int    `json:"quantity"`
	Total        int64  `json:"total"`
}

// GetSupplierOrderAddons returns the supplier's own add-on lines for one
// order (Fix 38, live parity: getOrderAddons). Only rows snapshotted to
// this supplier (owner_type_snapshot='supplier',
// owner_supplier_id_snapshot=supplierID) are returned; the product title
// comes from the parent order item. The per-row total is fee_per_unit x
// quantity, matching live's row rendering.
func (r *SupplierRepository) GetSupplierOrderAddons(ctx context.Context, supplierID, orderID string) ([]SupplierOrderAddon, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT oia.title_snapshot, p.title, oia.fee_per_unit, oia.quantity
		FROM order_item_addons oia
		JOIN order_items oi ON oi.id = oia.order_item_id
		JOIN products p ON p.id = oi.product_id
		WHERE oi.order_id = $1 AND oia.owner_type_snapshot = 'supplier' AND oia.owner_supplier_id_snapshot = $2
		ORDER BY oia.created_at`, orderID, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierOrderAddon{}
	for rows.Next() {
		var a SupplierOrderAddon
		if err := rows.Scan(&a.Title, &a.ProductTitle, &a.FeePerUnit, &a.Quantity); err != nil {
			return nil, err
		}
		a.Total = a.FeePerUnit * int64(a.Quantity)
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpdateProductOrigin sets a product's origin warehouse (Fix 39, live
// parity: PUT /supplier/products/{id}/origin). Ownership is enforced by
// the supplier_id predicate; an empty warehouse id clears the origin back
// to the profile-city fallback. Returns sql.ErrNoRows when the id is not
// the supplier's own.
func (r *SupplierRepository) UpdateProductOrigin(ctx context.Context, supplierID, productID, warehouseID string) error {
	var originID interface{}
	if warehouseID != "" {
		originID = warehouseID
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE products SET origin_supplier_warehouse_id = $1
		WHERE id = $2 AND supplier_id = $3`,
		originID, productID, supplierID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CentralWarehouse mirrors the platform warehouses table
// (migration 202610240001), used by GET /supplier/warehouses/central.
// TODO (U07): the live meaning of "central" on this route is unverified;
// this returns the platform warehouses table, the only central-warehouse
// table in the source, and must be re-checked against the live response.
type CentralWarehouse struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	ProvinceName          string   `json:"provinceName"`
	CityName              string   `json:"cityName"`
	Priority              int      `json:"priority"`
	IsActive              bool     `json:"isActive"`
	WarehouseID           string   `json:"warehouseId"`
	WarehouseName         string   `json:"warehouseName"`
	RemainingUnits        *int     `json:"remainingUnits"`
	RemainingVolumeM3     *float64 `json:"remainingVolumeM3"`
	UnknownDimensionUnits int      `json:"unknownDimensionUnits"`
	IsReceivingOpen       bool     `json:"isReceivingOpen"`
}

func (r *SupplierRepository) ListCentralWarehouses(ctx context.Context) ([]CentralWarehouse, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, province_name, city_name, priority, is_active
		FROM warehouses WHERE is_active = TRUE ORDER BY priority ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CentralWarehouse{}
	for rows.Next() {
		var w CentralWarehouse
		if err := rows.Scan(&w.ID, &w.Name, &w.ProvinceName, &w.CityName, &w.Priority, &w.IsActive); err != nil {
			return nil, err
		}
		w.WarehouseID = w.ID
		w.WarehouseName = w.Name
		w.IsReceivingOpen = w.IsActive
		out = append(out, w)
	}
	return out, rows.Err()
}

// SupplierExclusivityChallenge is the supplier-facing whitelist for
// exclusivity_challenges. Holder columns and the challenger payload are
// admin-only by migration 202610220004 and are never selected here.
type SupplierExclusivityChallenge struct {
	ID              string          `json:"id"`
	Branch          string          `json:"branch"`
	ProposedPrice   int64           `json:"proposedPrice"`
	ProductTitle    string          `json:"productTitle"`
	ProductBrand    string          `json:"productBrand"`
	ProductCat      string          `json:"productCategory"`
	Status          string          `json:"status"`
	RejectionReason *string         `json:"rejectionReason,omitempty"`
	VariantName     string          `json:"variantName,omitempty"`
	VariantNames    json.RawMessage `json:"variantNames,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
}

func (r *SupplierRepository) ListSupplierExclusivityChallenges(ctx context.Context, supplierID string) ([]SupplierExclusivityChallenge, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, branch, proposed_price, product_title, product_brand, product_category,
		       status, rejection_reason, variant_name, variant_names, created_at
		FROM exclusivity_challenges WHERE supplier_id = $1 ORDER BY created_at DESC
	`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierExclusivityChallenge{}
	for rows.Next() {
		var c SupplierExclusivityChallenge
		var rejection sql.NullString
		if err := rows.Scan(&c.ID, &c.Branch, &c.ProposedPrice, &c.ProductTitle, &c.ProductBrand,
			&c.ProductCat, &c.Status, &rejection, &c.VariantName, &c.VariantNames, &c.CreatedAt); err != nil {
			return nil, err
		}
		if rejection.Valid {
			c.RejectionReason = &rejection.String
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SupplierPriceIncrease mirrors supplier_price_increases
// (migration 202610310003) as a read-only list (D12). Rows are
// written by the governed pricing paths (PricingRepository): a
// product-level increase has VariantID nil; a variant-level one
// (migration 202611050009) names its product_variants row, whose
// name is joined in for display where available.
type SupplierPriceIncrease struct {
	ID              string   `json:"id"`
	ProductID       string   `json:"productId"`
	VariantID       *string  `json:"variantId,omitempty"`
	VariantName     string   `json:"variantName,omitempty"`
	OldReference    int64    `json:"oldReference"`
	NewReference    int64    `json:"newReference"`
	Pct             float64  `json:"pct"`
	Status          string   `json:"status"`
	IsException     bool     `json:"isException"`
	ExceptionCapPct *float64 `json:"exceptionCapPct,omitempty"`
	AutoApproved    bool     `json:"autoApproved"`
	AgreementReview bool     `json:"agreementReview"`
	// ClearOnApprove (migration 202611050010): a variant row whose
	// approval applies as a clear-to-inherit (variant cost back to
	// NULL) rather than setting NewReference on the variant.
	ClearOnApprove bool       `json:"clearOnApprove"`
	CreatedAt      time.Time  `json:"createdAt"`
	ReviewedAt     *time.Time `json:"reviewedAt,omitempty"`
}

func (r *SupplierRepository) ListSupplierPriceIncreases(ctx context.Context, supplierID string) ([]SupplierPriceIncrease, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT spi.id, spi.product_id, spi.variant_id, COALESCE(pv.variant_name, ''),
		       spi.old_reference, spi.new_reference, spi.pct, spi.status, spi.is_exception,
		       spi.exception_cap_pct, COALESCE(spi.auto_approved, FALSE), COALESCE(spi.agreement_review, FALSE),
		       COALESCE(spi.clear_on_approve, FALSE), spi.created_at, spi.reviewed_at
		FROM supplier_price_increases spi
		LEFT JOIN product_variants pv ON pv.id::text = spi.variant_id
		WHERE spi.supplier_id = $1 ORDER BY spi.created_at DESC
	`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierPriceIncrease{}
	for rows.Next() {
		var p SupplierPriceIncrease
		if err := rows.Scan(&p.ID, &p.ProductID, &p.VariantID, &p.VariantName, &p.OldReference, &p.NewReference, &p.Pct, &p.Status,
			&p.IsException, &p.ExceptionCapPct, &p.AutoApproved, &p.AgreementReview, &p.ClearOnApprove, &p.CreatedAt, &p.ReviewedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SupplierImplicitAgreementProduct is a supplier product carrying the
// admin-set implicit-agreement flag (migration 202610310006), listed
// read-only (D12 supplier/products/implicit-agreements).
type SupplierImplicitAgreementProduct struct {
	ID                     string     `json:"id"`
	Title                  string     `json:"title"`
	ImplicitAgreementUntil *time.Time `json:"implicitAgreementUntil,omitempty"`
}

func (r *SupplierRepository) ListSupplierImplicitAgreementProducts(ctx context.Context, supplierID string) ([]SupplierImplicitAgreementProduct, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, title, implicit_agreement_until FROM products
		WHERE supplier_id = $1 AND implicit_agreement = TRUE ORDER BY title ASC
	`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierImplicitAgreementProduct{}
	for rows.Next() {
		var p SupplierImplicitAgreementProduct
		if err := rows.Scan(&p.ID, &p.Title, &p.ImplicitAgreementUntil); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SupplierCustomerReturnBackRequest mirrors supplier_return_back_requests
// (migration 202610250301), shaped for live's An back-request block:
// status badge + Pn timeline use createdAt/reviewedAt/shippedAt/
// completedAt, trackingNumber and reviewReason.
type SupplierCustomerReturnBackRequest struct {
	ID             string     `json:"id"`
	ReturnItemID   string     `json:"returnItemId"`
	Quantity       int        `json:"quantity"`
	Status         string     `json:"status"`
	Reason         string     `json:"reason"`
	ReviewReason   string     `json:"reviewReason"`
	ReviewedAt     *time.Time `json:"reviewedAt,omitempty"`
	ShippedAt      *time.Time `json:"shippedAt,omitempty"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
	CarrierCode    *string    `json:"carrierCode,omitempty"`
	TrackingNumber *string    `json:"trackingNumber,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

// SupplierCustomerReturnItem is one returned item inside a return section
// of live's An component (product row + QC badge + back-request block).
type SupplierCustomerReturnItem struct {
	ReturnItemID string                              `json:"returnItemId"`
	ProductTitle string                              `json:"productTitle"`
	VariantName  string                              `json:"variantName,omitempty"`
	Quantity     int                                 `json:"quantity"`
	QCStatus     string                              `json:"qcStatus"`
	ReceivedAt   *time.Time                          `json:"receivedAt,omitempty"`
	BackRequests []SupplierCustomerReturnBackRequest `json:"backRequests"`
}

// SupplierCustomerReturn is one return section of live's An component:
// order header (orderNumber + Sn status badge + returnCode badge), reason,
// destination line, return-shipping bearer/amount boxes, and items.
// Destination fields are nil when the return has no recorded warehouse
// (live renders the destination line only when a name is present).
type SupplierCustomerReturn struct {
	ReturnID               string                       `json:"returnId"`
	OrderID                string                       `json:"orderId"`
	OrderNumber            string                       `json:"orderNumber"`
	Status                 string                       `json:"status"`
	Reason                 string                       `json:"reason"`
	ReturnCode             *string                      `json:"returnCode,omitempty"`
	CreatedAt              time.Time                    `json:"createdAt"`
	ReturnShippingAmount   *int64                       `json:"returnShippingAmount,omitempty"`
	ReturnShippingBearer   *string                      `json:"returnShippingBearer,omitempty"`
	DestinationName        *string                      `json:"destinationName,omitempty"`
	WarehouseName          *string                      `json:"warehouseName,omitempty"`
	DestinationType        *string                      `json:"destinationType,omitempty"`
	WarehouseCity          *string                      `json:"warehouseCity,omitempty"`
	Items                  []SupplierCustomerReturnItem `json:"items"`
}

func (r *SupplierRepository) ListSupplierCustomerReturns(ctx context.Context, supplierID string) ([]SupplierCustomerReturn, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT cr.id, cr.order_id, COALESCE(o.order_number, cr.order_id),
			cr.status, cr.reason, cr.return_code, cr.created_at,
			cr.return_shipping_amount, cr.return_shipping_bearer,
			w.name, w.city_name,
			cr.destination_supplier_warehouse_id, sw.name, sw.city_name
		FROM customer_returns cr
		JOIN return_items ri ON ri.return_id = cr.id
		JOIN order_items oi ON oi.id::text = ri.order_item_id
		JOIN products p ON p.id = oi.product_id
		LEFT JOIN orders o ON o.id = cr.order_id
		LEFT JOIN warehouses w ON w.id = cr.warehouse_id
		LEFT JOIN supplier_warehouses sw ON sw.id = cr.destination_supplier_warehouse_id
		WHERE p.supplier_id = $1
		ORDER BY cr.created_at DESC
	`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierCustomerReturn{}
	ids := []string{}
	for rows.Next() {
		var c SupplierCustomerReturn
		var destName, destCity sql.NullString
		var destSupplierWarehouseID, supplierDestName, supplierDestCity sql.NullString
		if err := rows.Scan(&c.ReturnID, &c.OrderID, &c.OrderNumber, &c.Status,
			&c.Reason, &c.ReturnCode, &c.CreatedAt, &c.ReturnShippingAmount,
			&c.ReturnShippingBearer, &destName, &destCity,
			&destSupplierWarehouseID, &supplierDestName, &supplierDestCity); err != nil {
			return nil, err
		}
		// Destination (owner rule, Oct 28 2026): a recorded supplier
		// warehouse destination (migration 202611050003) wins over the
		// central warehouse columns — the two are mutually exclusive
		// by construction at creation.
		if destSupplierWarehouseID.Valid && destSupplierWarehouseID.String != "" {
			t := "supplier"
			c.DestinationType = &t
			if supplierDestName.Valid {
				c.DestinationName = &supplierDestName.String
				c.WarehouseName = &supplierDestName.String
			}
			if supplierDestCity.Valid {
				c.WarehouseCity = &supplierDestCity.String
			}
		} else if destName.Valid {
			c.DestinationName = &destName.String
			c.WarehouseName = &destName.String
			t := "central"
			c.DestinationType = &t
			if destCity.Valid {
				c.WarehouseCity = &destCity.String
			}
		}
		c.Items = []SupplierCustomerReturnItem{}
		out = append(out, c)
		ids = append(ids, c.ReturnID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return out, nil
	}
	itemRows, err := r.db.QueryContext(ctx, `
		SELECT ri.id, ri.return_id, p.title, COALESCE(pv.variant_name, ''),
			ri.qty, ri.qc_status, ri.received_at
		FROM return_items ri
		JOIN order_items oi ON oi.id::text = ri.order_item_id
		JOIN products p ON p.id = oi.product_id
		LEFT JOIN product_variants pv ON pv.id = ri.variant_id
		WHERE ri.return_id = ANY($1::uuid[]) AND p.supplier_id = $2
		ORDER BY p.title ASC
	`, pq.Array(ids), supplierID)
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()
	byReturn := map[string][]SupplierCustomerReturnItem{}
	itemIDs := []string{}
	for itemRows.Next() {
		var it SupplierCustomerReturnItem
		var returnID string
		if err := itemRows.Scan(&it.ReturnItemID, &returnID, &it.ProductTitle,
			&it.VariantName, &it.Quantity, &it.QCStatus, &it.ReceivedAt); err != nil {
			return nil, err
		}
		it.BackRequests = []SupplierCustomerReturnBackRequest{}
		byReturn[returnID] = append(byReturn[returnID], it)
		itemIDs = append(itemIDs, it.ReturnItemID)
	}
	if err := itemRows.Err(); err != nil {
		return nil, err
	}
	if len(itemIDs) > 0 {
		backRows, err := r.db.QueryContext(ctx, `
			SELECT id, return_item_id, quantity, status, reason, review_reason,
				reviewed_at, shipped_at, completed_at, carrier_code,
				tracking_number, created_at
			FROM supplier_return_back_requests
			WHERE return_item_id = ANY($1::uuid[])
			ORDER BY created_at DESC
		`, pq.Array(itemIDs))
		if err != nil {
			return nil, err
		}
		defer backRows.Close()
		backsByItem := map[string][]SupplierCustomerReturnBackRequest{}
		for backRows.Next() {
			var b SupplierCustomerReturnBackRequest
			if err := backRows.Scan(&b.ID, &b.ReturnItemID, &b.Quantity, &b.Status,
				&b.Reason, &b.ReviewReason, &b.ReviewedAt, &b.ShippedAt,
				&b.CompletedAt, &b.CarrierCode, &b.TrackingNumber, &b.CreatedAt); err != nil {
				return nil, err
			}
			backsByItem[b.ReturnItemID] = append(backsByItem[b.ReturnItemID], b)
		}
		if err := backRows.Err(); err != nil {
			return nil, err
		}
		for rid, items := range byReturn {
			for k := range items {
				if backs, ok := backsByItem[items[k].ReturnItemID]; ok {
					items[k].BackRequests = backs
				}
			}
			byReturn[rid] = items
		}
	}
	for k := range out {
		if items, ok := byReturn[out[k].ReturnID]; ok {
			out[k].Items = items
		}
	}
	return out, nil
}

// SupplierReturnShippingCharge mirrors customer_return_shipping_charges
// (migration 202610250202), the per-item settlement notes live's An groups
// by return_id. Keys are snake_case because live's component reads
// return_id (not returnId) when grouping.
type SupplierReturnShippingCharge struct {
	ID        string `json:"id"`
	ReturnID  string `json:"return_id"`
	Bearer    string `json:"bearer"`
	Amount    int64  `json:"amount"`
	Reason    string `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

func (r *SupplierRepository) ListSupplierReturnShippingCharges(ctx context.Context, supplierID string) ([]SupplierReturnShippingCharge, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, return_id, bearer, amount, reason, created_at
		FROM customer_return_shipping_charges
		WHERE supplier_id = $1
		ORDER BY created_at DESC
	`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierReturnShippingCharge{}
	for rows.Next() {
		var c SupplierReturnShippingCharge
		if err := rows.Scan(&c.ID, &c.ReturnID, &c.Bearer, &c.Amount, &c.Reason, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SupplierReturnClawback mirrors one customer_return_financial_events
// product_cost row (migration 202611050006, GAP 6): a refunded customer
// return reversing this supplier's product cost for one return item.
// Keys follow the shipping-charge reader's snake_case shape.
type SupplierReturnClawback struct {
	ID        string    `json:"id"`
	ReturnID  string    `json:"return_id"`
	Amount    int64     `json:"amount"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

// ListSupplierReturnClawbacks lists the supplier's customer-return
// cost reversals, newest first — the settlement surface that explains
// a drop in Available. Returns an empty list on a database where the
// events table does not exist yet.
func (r *SupplierRepository) ListSupplierReturnClawbacks(ctx context.Context, supplierID string) ([]SupplierReturnClawback, error) {
	var hasTable bool
	if err := r.db.QueryRowContext(ctx, `SELECT to_regclass('public.customer_return_financial_events') IS NOT NULL`).Scan(&hasTable); err != nil {
		return nil, err
	}
	out := []SupplierReturnClawback{}
	if !hasTable {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, return_id::text, amount, reason, created_at
		FROM customer_return_financial_events
		WHERE supplier_id = $1 AND component = 'product_cost'
		ORDER BY created_at DESC
	`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c SupplierReturnClawback
		if err := rows.Scan(&c.ID, &c.ReturnID, &c.Amount, &c.Reason, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

var (
	ErrSupplierReturnBackActive       = errors.New("an active back request already exists for this item")
	ErrSupplierReturnBackNotReceived  = errors.New("item has not been received at the warehouse yet")
	ErrSupplierReturnBackBadQuantity  = errors.New("quantity is out of range for this item")
	ErrSupplierReturnBackBadState     = errors.New("back request is not in a state that allows this action")
)

// CreateSupplierReturnBack inserts a requested back-request for a received
// return item owned by this supplier. Idempotent on idempotency_key: a
// replay returns the original row instead of failing on the UNIQUE key.
func (r *SupplierRepository) CreateSupplierReturnBack(ctx context.Context, supplierID, returnItemID string, quantity int, reason, idemKey string) (*SupplierCustomerReturnBackRequest, error) {
	if quantity <= 0 || strings.TrimSpace(reason) == "" {
		return nil, ErrSupplierReturnBackBadQuantity
	}
	var itemQty int
	var qcStatus string
	var receivedAt sql.NullTime
	var returnID, productID, ownerID string
	err := r.db.QueryRowContext(ctx, `
		SELECT ri.qty, ri.qc_status, ri.received_at, ri.return_id, p.id, p.supplier_id
		FROM return_items ri
		JOIN order_items oi ON oi.id::text = ri.order_item_id
		JOIN products p ON p.id = oi.product_id
		WHERE ri.id = $1
	`, returnItemID).Scan(&itemQty, &qcStatus, &receivedAt, &returnID, &productID, &ownerID)
	if err != nil {
		return nil, err
	}
	if ownerID != supplierID {
		return nil, sql.ErrNoRows
	}
	if !receivedAt.Valid {
		return nil, ErrSupplierReturnBackNotReceived
	}
	var activeCount int
	if err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM supplier_return_back_requests
		WHERE return_item_id = $1 AND status IN ('requested', 'approved', 'shipped_back')
	`, returnItemID).Scan(&activeCount); err != nil {
		return nil, err
	}
	if activeCount > 0 {
		return nil, ErrSupplierReturnBackActive
	}
	var completedQty, otherQty int
	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN status = 'completed' THEN quantity ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status NOT IN ('rejected', 'cancelled', 'completed') THEN quantity ELSE 0 END), 0)
		FROM supplier_return_back_requests WHERE return_item_id = $1
	`, returnItemID).Scan(&completedQty, &otherQty); err != nil {
		return nil, err
	}
	// Live's form max: after a completed request the remaining units
	// (quantity minus non-rejected/non-cancelled) cap a re-request;
	// otherwise the full item quantity caps it.
	maxQty := itemQty
	if completedQty > 0 {
		maxQty = itemQty - completedQty - otherQty
	}
	if quantity > maxQty {
		return nil, ErrSupplierReturnBackBadQuantity
	}
	source := "legacy"
	if qcStatus == "defective" {
		source = "none"
	} else {
		var balanceCount int
		if err := r.db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM inventory_balances
			WHERE product_id = $1 AND location_key = $2 AND quantity > 0
		`, productID, "inbound:"+returnID).Scan(&balanceCount); err == nil && balanceCount > 0 {
			source = "canonical"
		}
	}
	if idemKey == "" {
		idemKey = "rb-" + uuid.NewString()
	}
	id := "srb-" + uuid.NewString()
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO supplier_return_back_requests
			(id, customer_return_id, return_item_id, supplier_id, product_id,
			 quantity, status, reason, inventory_source, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6, 'requested', $7, $8, $9)
	`, id, returnID, returnItemID, supplierID, productID, quantity, reason, source, idemKey)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			var existing SupplierCustomerReturnBackRequest
			qerr := r.db.QueryRowContext(ctx, `
				SELECT id, return_item_id, quantity, status, reason, review_reason,
					reviewed_at, shipped_at, completed_at, carrier_code,
					tracking_number, created_at
				FROM supplier_return_back_requests WHERE idempotency_key = $1
			`, idemKey).Scan(&existing.ID, &existing.ReturnItemID, &existing.Quantity,
				&existing.Status, &existing.Reason, &existing.ReviewReason,
				&existing.ReviewedAt, &existing.ShippedAt, &existing.CompletedAt,
				&existing.CarrierCode, &existing.TrackingNumber, &existing.CreatedAt)
			if qerr != nil {
				return nil, err
			}
			return &existing, nil
		}
		return nil, err
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO supplier_return_back_events
			(id, back_request_id, from_status, to_status, reason, idempotency_key)
		VALUES ($1, $2, NULL, 'requested', $3, $4)
	`, "srbe-"+uuid.NewString(), id, reason, idemKey); err != nil {
		return nil, err
	}
	return r.GetSupplierReturnBack(ctx, supplierID, id)
}

func (r *SupplierRepository) GetSupplierReturnBack(ctx context.Context, supplierID, backID string) (*SupplierCustomerReturnBackRequest, error) {
	var b SupplierCustomerReturnBackRequest
	err := r.db.QueryRowContext(ctx, `
		SELECT id, return_item_id, quantity, status, reason, review_reason,
			reviewed_at, shipped_at, completed_at, carrier_code,
			tracking_number, created_at
		FROM supplier_return_back_requests
		WHERE id = $1 AND supplier_id = $2
	`, backID, supplierID).Scan(&b.ID, &b.ReturnItemID, &b.Quantity, &b.Status,
		&b.Reason, &b.ReviewReason, &b.ReviewedAt, &b.ShippedAt, &b.CompletedAt,
		&b.CarrierCode, &b.TrackingNumber, &b.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// transitionSupplierReturnBack moves a back-request between two states
// with an idempotent event row: a replayed key returns the current row.
func (r *SupplierRepository) transitionSupplierReturnBack(ctx context.Context, supplierID, backID, from, to, reason, idemKey string) (*SupplierCustomerReturnBackRequest, error) {
	cur, err := r.GetSupplierReturnBack(ctx, supplierID, backID)
	if err != nil {
		return nil, err
	}
	if cur.Status != from {
		// A replayed transition (event key already recorded) returns the
		// current row instead of failing on the state guard.
		var eventCount int
		if idemKey != "" {
			if qerr := r.db.QueryRowContext(ctx, `
				SELECT COUNT(*) FROM supplier_return_back_events
				WHERE back_request_id = $1 AND idempotency_key = $2
			`, backID, idemKey).Scan(&eventCount); qerr == nil && eventCount > 0 {
				return cur, nil
			}
		}
		return nil, ErrSupplierReturnBackBadState
	}
	var stampCol string
	switch to {
	case "cancelled":
		stampCol = ""
	case "completed":
		stampCol = "completed_at = NOW(), "
	default:
		return nil, ErrSupplierReturnBackBadState
	}
	if _, err := r.db.ExecContext(ctx, `
		UPDATE supplier_return_back_requests SET status = $1, `+stampCol+`updated_at = NOW()
		WHERE id = $2
	`, to, backID); err != nil {
		return nil, err
	}
	if idemKey == "" {
		idemKey = to + "-back-" + backID
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO supplier_return_back_events
			(id, back_request_id, from_status, to_status, reason, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (idempotency_key) DO NOTHING
	`, "srbe-"+uuid.NewString(), backID, from, to, reason, idemKey); err != nil {
		return nil, err
	}
	return r.GetSupplierReturnBack(ctx, supplierID, backID)
}

func (r *SupplierRepository) CancelSupplierReturnBack(ctx context.Context, supplierID, backID, reason, idemKey string) (*SupplierCustomerReturnBackRequest, error) {
	return r.transitionSupplierReturnBack(ctx, supplierID, backID, "requested", "cancelled", reason, idemKey)
}

func (r *SupplierRepository) CompleteSupplierReturnBack(ctx context.Context, supplierID, backID, idemKey string) (*SupplierCustomerReturnBackRequest, error) {
	return r.transitionSupplierReturnBack(ctx, supplierID, backID, "shipped_back", "completed", "", idemKey)
}

// SupplierStorageFee mirrors warehouse_storage_fee_charges
// (migration 202610280005) for one supplier (D12
// supplier/settlement/storage-fees), read-only.
type SupplierStorageFee struct {
	ID            string    `json:"id"`
	ProductID     string    `json:"productId"`
	ProductTitle  string    `json:"productTitle"`
	WarehouseID   string    `json:"warehouseId"`
	WarehouseName string    `json:"warehouseName"`
	FeeDate       time.Time `json:"feeDate"`
	Quantity      int       `json:"qty"`
	UnitDay       int64     `json:"feeUnitDayToman"`
	Amount        int64     `json:"amountToman"`
	GoodsClass    string    `json:"goodsClass"`
	Estimated     bool      `json:"estimated"`
	CreatedAt     time.Time `json:"createdAt"`
}

func (r *SupplierRepository) ListSupplierStorageFees(ctx context.Context, supplierID string) ([]SupplierStorageFee, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT f.id, f.product_id, COALESCE(p.title, ''), f.warehouse_id, COALESCE(w.name, ''),
		       f.fee_date, f.qty, f.fee_unit_day_toman,
		       f.amount_toman, f.goods_class, f.estimated, f.created_at
		FROM warehouse_storage_fee_charges f
		LEFT JOIN products p ON p.id = f.product_id
		LEFT JOIN warehouses w ON w.id = f.warehouse_id
		WHERE f.supplier_id = $1
		ORDER BY f.fee_date DESC, f.created_at DESC LIMIT 200
	`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierStorageFee{}
	for rows.Next() {
		var f SupplierStorageFee
		if err := rows.Scan(&f.ID, &f.ProductID, &f.ProductTitle, &f.WarehouseID, &f.WarehouseName, &f.FeeDate, &f.Quantity,
			&f.UnitDay, &f.Amount, &f.GoodsClass, &f.Estimated, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SupplierAddon mirrors product_addons owned by one supplier
// (migration 202610230005) for GET /supplier/addons (D12).
type SupplierAddon struct {
	ID             string            `json:"id"`
	Title          string            `json:"title"`
	Description    string            `json:"description"`
	Kind           string            `json:"kind"`
	PriceMode      string            `json:"priceMode"`
	PriceValue     float64           `json:"priceValue"`
	CapToman       int64             `json:"capToman"`
	ApprovalStatus string            `json:"approvalStatus"`
	RejectionReason *string          `json:"rejectionReason,omitempty"`
	IsActive       bool              `json:"isActive"`
	SortOrder      int               `json:"sortOrder"`
	LinkedProductID *string          `json:"linkedProductId,omitempty"`
	Attachments    []AddonAttachment `json:"attachments"`
	CreatedAt      time.Time         `json:"createdAt"`
}

type AddonAttachment struct {
	ScopeType string `json:"scopeType"`
	ProductID string `json:"productId"`
}

func (r *SupplierRepository) ListSupplierAddons(ctx context.Context, supplierID string) ([]SupplierAddon, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, title, COALESCE(description, ''), kind, price_mode, price_value,
		       cap_toman, approval_status, rejection_reason, is_active, sort_order, linked_product_id, created_at
		FROM product_addons WHERE owner_type = 'supplier' AND owner_supplier_id = $1
		ORDER BY sort_order ASC, created_at DESC
	`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SupplierAddon{}
	for rows.Next() {
		var a SupplierAddon
		if err := rows.Scan(&a.ID, &a.Title, &a.Description, &a.Kind, &a.PriceMode, &a.PriceValue,
			&a.CapToman, &a.ApprovalStatus, &a.RejectionReason, &a.IsActive, &a.SortOrder, &a.LinkedProductID, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Attachments = []AddonAttachment{}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	ids := make([]interface{}, 0, len(out))
	for _, a := range out {
		ids = append(ids, a.ID)
	}
	placeholders := make([]string, 0, len(ids))
	for i := range ids {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
	}
	arows, err := r.db.QueryContext(ctx, `SELECT addon_id, product_id FROM product_addon_attachments WHERE scope_type = 'product' AND addon_id IN (`+strings.Join(placeholders, ", ")+`)`, ids...)
	if err != nil {
		return nil, err
	}
	defer arows.Close()
	byAddon := map[string][]AddonAttachment{}
	for arows.Next() {
		var addonID, productID string
		if err := arows.Scan(&addonID, &productID); err != nil {
			return nil, err
		}
		byAddon[addonID] = append(byAddon[addonID], AddonAttachment{ScopeType: "product", ProductID: productID})
	}
	if err := arows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if atts, ok := byAddon[out[i].ID]; ok {
			out[i].Attachments = atts
		}
	}
	return out, nil
}

// FIX 50 — live addons widget (Ql): supplier addon writes. New addons are
// born pending + inactive (migration 202610230005 design). Edits to an
// APPROVED addon are staged in pending_approvals (entity_type
// 'product_addon') and the live row is untouched until a manager approves
// — this is what live's staged toast describes. Edits to pending/rejected
// addons apply in place.
func (r *SupplierRepository) CreateSupplierAddon(ctx context.Context, supplierID string, a *SupplierAddon) (*SupplierAddon, error) {
	a.ID = "addon-" + uuid.NewString()[:12]
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO product_addons (id, owner_type, owner_supplier_id, title, description, kind, price_mode, price_value, cap_toman, approval_status, is_active, sort_order, linked_product_id)
		VALUES ($1, 'supplier', $2, $3, $4, $5, $6, $7, $8, 'pending', FALSE, $9, $10)
	`, a.ID, supplierID, a.Title, a.Description, a.Kind, a.PriceMode, a.PriceValue, a.CapToman, a.SortOrder, a.LinkedProductID)
	if err != nil {
		return nil, err
	}
	created, err := r.GetSupplierAddon(ctx, supplierID, a.ID)
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (r *SupplierRepository) GetSupplierAddon(ctx context.Context, supplierID, addonID string) (*SupplierAddon, error) {
	list, err := r.ListSupplierAddons(ctx, supplierID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == addonID {
			return &list[i], nil
		}
	}
	return nil, nil
}

func (r *SupplierRepository) UpdateSupplierAddonInPlace(ctx context.Context, supplierID string, a *SupplierAddon) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE product_addons SET title = $1, description = $2, kind = $3, price_mode = $4, price_value = $5, cap_toman = $6, sort_order = $7, linked_product_id = $8, updated_at = NOW()
		WHERE id = $9 AND owner_type = 'supplier' AND owner_supplier_id = $10
	`, a.Title, a.Description, a.Kind, a.PriceMode, a.PriceValue, a.CapToman, a.SortOrder, a.LinkedProductID, a.ID, supplierID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *SupplierRepository) StageSupplierAddonUpdate(ctx context.Context, supplierID string, a *SupplierAddon) error {
	data, err := json.Marshal(map[string]interface{}{
		"title": a.Title, "description": a.Description, "kind": a.Kind, "priceMode": a.PriceMode,
		"priceValue": a.PriceValue, "capToman": a.CapToman, "sortOrder": a.SortOrder, "linkedProductId": a.LinkedProductID,
	})
	if err != nil {
		return err
	}
	propID := "prop-addon-" + uuid.NewString()[:12]
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO pending_approvals (id, entity_type, target_id, title, requested_data, requested_by, status)
		VALUES ($1, 'product_addon', $2, $3, $4, $5, 'pending')
	`, propID, a.ID, a.Title, data, supplierID)
	return err
}

func (r *SupplierRepository) SetSupplierAddonActive(ctx context.Context, supplierID, addonID string, active bool) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE product_addons SET is_active = $1, updated_at = NOW()
		WHERE id = $2 AND owner_type = 'supplier' AND owner_supplier_id = $3
	`, active, addonID, supplierID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *SupplierRepository) SetSupplierAddonAttachments(ctx context.Context, supplierID, addonID string, productIDs []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner string
	err = tx.QueryRowContext(ctx, `SELECT owner_supplier_id FROM product_addons WHERE id = $1 AND owner_type = 'supplier'`, addonID).Scan(&owner)
	if err != nil {
		return err
	}
	if owner != supplierID {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM product_addon_attachments WHERE addon_id = $1 AND scope_type = 'product'`, addonID); err != nil {
		return err
	}
	for _, pid := range productIDs {
		var productOwner string
		err := tx.QueryRowContext(ctx, `SELECT supplier_id FROM products WHERE id = $1`, pid).Scan(&productOwner)
		if err != nil {
			return err
		}
		if productOwner != supplierID {
			return fmt.Errorf("product %s is not owned by this supplier", pid)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO product_addon_attachments (addon_id, scope_type, product_id) VALUES ($1, 'product', $2) ON CONFLICT DO NOTHING`, addonID, pid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SupplierAnalyticsSummary is the live supplier analytics shape
// (frontend fields of the live supplier analytics tab): base amounts
// are wholesale costs, not customer prices; orderCount counts paid
// orders only, which is also what drives the empty state. Returns
// aggregation is provisional: live exact aggregation rules are not
// observable from the bundle, so finalized means QC-decided items.
type SupplierAnalyticsReason struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type SupplierAnalyticsReturns struct {
	TotalReturned int                       `json:"totalReturned"`
	QcOk          int                       `json:"qcOk"`
	QcDefective   int                       `json:"qcDefective"`
	TopReasons    []SupplierAnalyticsReason `json:"topReasons"`
}

type SupplierAnalyticsDaily struct {
	Date    string `json:"date"`
	Revenue int64  `json:"revenue"`
	Orders  int    `json:"orders"`
}

type SupplierAnalyticsProduct struct {
	ProductID  string  `json:"productId"`
	Name       string  `json:"name"`
	Quantity   int     `json:"quantity"`
	BaseAmount int64   `json:"baseAmount"`
	ReturnRate float64 `json:"returnRate"`
	Stock      int     `json:"stock"`
	DaysLeft   *int    `json:"daysLeft"`
}

type SupplierAnalyticsSummary struct {
	SupplierID      string                     `json:"supplierId"`
	TotalBaseAmount int64                      `json:"totalBaseAmount"`
	TotalQuantity   int                        `json:"totalQuantity"`
	OrderCount      int                        `json:"orderCount"`
	Returns         SupplierAnalyticsReturns   `json:"returns"`
	Daily           []SupplierAnalyticsDaily   `json:"daily"`
	Products        []SupplierAnalyticsProduct `json:"products"`
}

func (r *SupplierRepository) GetSupplierAnalytics(ctx context.Context, supplierID string) (*SupplierAnalyticsSummary, error) {
	s := &SupplierAnalyticsSummary{SupplierID: supplierID}
	paid := "paid"
	okStatus := "ok"
	defectiveStatus := "defective"
	// Totals over paid orders only.
	err := r.db.QueryRowContext(ctx, `
		SELECT
		  COALESCE(SUM(oi.wholesale_cost * oi.quantity), 0),
		  COALESCE(SUM(oi.quantity), 0),
		  COALESCE(COUNT(DISTINCT oi.order_id), 0)
		FROM order_items oi
		JOIN products p ON p.id = oi.product_id
		JOIN orders o ON o.id = oi.order_id
		WHERE p.supplier_id = $1 AND o.payment_status = $2
	`, supplierID, paid).Scan(&s.TotalBaseAmount, &s.TotalQuantity, &s.OrderCount)
	if err != nil {
		return nil, err
	}
	// Returns: QC-decided items linked to this supplier products.
	qcRows, err := r.db.QueryContext(ctx, `
		SELECT ri.qc_status, COUNT(*)
		FROM return_items ri
		JOIN order_items oi ON oi.id::text = ri.order_item_id
		JOIN products p ON p.id = oi.product_id
		WHERE p.supplier_id = $1 AND ri.qc_status IN ($2, $3)
		GROUP BY ri.qc_status
	`, supplierID, okStatus, defectiveStatus)
	if err != nil {
		return nil, err
	}
	for qcRows.Next() {
		var status string
		var n int
		if err := qcRows.Scan(&status, &n); err != nil {
			qcRows.Close()
			return nil, err
		}
		if status == "ok" {
			s.Returns.QcOk = n
		} else {
			s.Returns.QcDefective = n
		}
	}
	if err := qcRows.Err(); err != nil {
		qcRows.Close()
		return nil, err
	}
	qcRows.Close()
	s.Returns.TotalReturned = s.Returns.QcOk + s.Returns.QcDefective
	// Top return reasons among QC-decided items.
	reasonRows, err := r.db.QueryContext(ctx, `
		SELECT cr.reason, COUNT(*)
		FROM customer_returns cr
		JOIN return_items ri ON ri.return_id = cr.id
		JOIN order_items oi ON oi.id::text = ri.order_item_id
		JOIN products p ON p.id = oi.product_id
		WHERE p.supplier_id = $1 AND ri.qc_status IN ($2, $3) AND cr.reason <> ''
		GROUP BY cr.reason
		ORDER BY COUNT(*) DESC
		LIMIT 5
	`, supplierID, okStatus, defectiveStatus)
	if err != nil {
		return nil, err
	}
	s.Returns.TopReasons = []SupplierAnalyticsReason{}
	for reasonRows.Next() {
		var reason SupplierAnalyticsReason
		if err := reasonRows.Scan(&reason.Reason, &reason.Count); err != nil {
			reasonRows.Close()
			return nil, err
		}
		s.Returns.TopReasons = append(s.Returns.TopReasons, reason)
	}
	if err := reasonRows.Err(); err != nil {
		reasonRows.Close()
		return nil, err
	}
	reasonRows.Close()
	// Daily series, last 30 days including today, zero-filled.
	dailyRows, err := r.db.QueryContext(ctx, `
		SELECT d.day,
		  COALESCE(SUM(oi.wholesale_cost * oi.quantity), 0),
		  COALESCE(COUNT(DISTINCT oi.order_id), 0)
		FROM generate_series(CURRENT_DATE - 29, CURRENT_DATE, '1 day'::interval) AS d(day)
		LEFT JOIN orders o ON o.created_at::date = d.day AND o.payment_status = $2
		LEFT JOIN order_items oi ON oi.order_id = o.id
		LEFT JOIN products p ON p.id = oi.product_id AND p.supplier_id = $1
		GROUP BY d.day
		ORDER BY d.day ASC
	`, supplierID, paid)
	if err != nil {
		return nil, err
	}
	s.Daily = []SupplierAnalyticsDaily{}
	for dailyRows.Next() {
		var day time.Time
		var d SupplierAnalyticsDaily
		if err := dailyRows.Scan(&day, &d.Revenue, &d.Orders); err != nil {
			dailyRows.Close()
			return nil, err
		}
		d.Date = day.Format("2006-01-02")
		s.Daily = append(s.Daily, d)
	}
	if err := dailyRows.Err(); err != nil {
		dailyRows.Close()
		return nil, err
	}
	dailyRows.Close()
	// Per-product table: all supplier products, best sellers first.
	prodRows, err := r.db.QueryContext(ctx, `
		SELECT p.id, p.title, COALESCE(p.stock, 0),
		  COALESCE(sq.qty, 0), COALESCE(sq.base, 0), COALESCE(mq.qty30, 0), COALESCE(rq.ret, 0)
		FROM products p
		LEFT JOIN (
			SELECT oi.product_id, SUM(oi.quantity) AS qty, SUM(oi.wholesale_cost * oi.quantity) AS base
			FROM order_items oi JOIN orders o ON o.id = oi.order_id
			WHERE o.payment_status = $2 GROUP BY oi.product_id
		) sq ON sq.product_id = p.id
		LEFT JOIN (
			SELECT oi.product_id, SUM(oi.quantity) AS qty30
			FROM order_items oi JOIN orders o ON o.id = oi.order_id
			WHERE o.payment_status = $2 AND o.created_at >= CURRENT_DATE - 29 GROUP BY oi.product_id
		) mq ON mq.product_id = p.id
		LEFT JOIN (
			SELECT oi.product_id, SUM(ri.qty) AS ret
			FROM return_items ri JOIN order_items oi ON oi.id::text = ri.order_item_id
			WHERE ri.qc_status IN ($3, $4) GROUP BY oi.product_id
		) rq ON rq.product_id = p.id
		WHERE p.supplier_id = $1
		ORDER BY COALESCE(sq.qty, 0) DESC, p.title ASC
	`, supplierID, paid, okStatus, defectiveStatus)
	if err != nil {
		return nil, err
	}
	s.Products = []SupplierAnalyticsProduct{}
	for prodRows.Next() {
		var pr SupplierAnalyticsProduct
		var qty30 int
		var retQty int
		if err := prodRows.Scan(&pr.ProductID, &pr.Name, &pr.Stock, &pr.Quantity, &pr.BaseAmount, &qty30, &retQty); err != nil {
			prodRows.Close()
			return nil, err
		}
		if pr.Quantity > 0 {
			pr.ReturnRate = float64(retQty) / float64(pr.Quantity)
		}
		if qty30 > 0 {
			d := pr.Stock * 30 / qty30
			pr.DaysLeft = &d
		}
		s.Products = append(s.Products, pr)
	}
	if err := prodRows.Err(); err != nil {
		prodRows.Close()
		return nil, err
	}
	prodRows.Close()
	return s, nil
}

// FIX 48 — live manager-feedback modal (zl) backend: per-field review
// dossier for one supplier proposal, plus resubmission of rejected fields.
// Proposals live in pending_approvals (requested_data JSONB); field
// decisions in proposal_field_reviews; audit rows in proposal_review_history.
func (r *SupplierRepository) GetCatalogReviewDetail(ctx context.Context, supplierID, proposalID string) (map[string]interface{}, []map[string]interface{}, []map[string]interface{}, error) {
	var id, entityType, targetID, title, status string
	var rawData []byte
	var rejectionReason sql.NullString
	var createdAt time.Time
	var reviewedAt *time.Time
	err := r.db.QueryRowContext(ctx, `SELECT id, entity_type, target_id, title, requested_data, status, rejection_reason, created_at, reviewed_at FROM pending_approvals WHERE id = $1 AND requested_by = $2`, proposalID, supplierID).Scan(&id, &entityType, &targetID, &title, &rawData, &status, &rejectionReason, &createdAt, &reviewedAt)
	if err != nil {
		return nil, nil, nil, err
	}
	parsed := map[string]interface{}{}
	_ = json.Unmarshal(rawData, &parsed)
	if parsed == nil {
		parsed = map[string]interface{}{}
	}
	parsed["proposal_id"] = id
	parsed["id"] = id
	parsed["entity_type"] = entityType
	parsed["title"] = title
	parsed["status"] = status
	parsed["created_at"] = createdAt
	if rejectionReason.Valid {
		parsed["rejection_reason"] = rejectionReason.String
	}
	if reviewedAt != nil {
		parsed["reviewed_at"] = *reviewedAt
	}
	rows, err := r.db.QueryContext(ctx, `SELECT field_key, decision, reason FROM proposal_field_reviews WHERE proposal_id = $1 ORDER BY field_key ASC`, proposalID)
	if err != nil {
		return nil, nil, nil, err
	}
	fields := []map[string]interface{}{}
	for rows.Next() {
		var key, decision string
		var reason sql.NullString
		if err := rows.Scan(&key, &decision, &reason); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		f := map[string]interface{}{"field_key": key, "decision": decision}
		if reason.Valid {
			f["reason"] = reason.String
		}
		fields = append(fields, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, nil, err
	}
	hrows, err := r.db.QueryContext(ctx, `SELECT action, field_key, reason, created_at FROM proposal_review_history WHERE proposal_id = $1 ORDER BY created_at ASC`, proposalID)
	if err != nil {
		return nil, nil, nil, err
	}
	defer hrows.Close()
	history := []map[string]interface{}{}
	for hrows.Next() {
		var action string
		var key, reason sql.NullString
		var at time.Time
		if err := hrows.Scan(&action, &key, &reason, &at); err != nil {
			return nil, nil, nil, err
		}
		h := map[string]interface{}{"action": action, "created_at": at}
		if key.Valid {
			h["field_key"] = key.String
		}
		if reason.Valid {
			h["reason"] = reason.String
		}
		history = append(history, h)
	}
	if err := hrows.Err(); err != nil {
		return nil, nil, nil, err
	}
	return parsed, fields, history, nil
}

// ResubmitCatalogReview applies the supplier's corrected rejected fields
// (live zl's data map) to requested_data, reopens those fields as pending,
// records one 'resubmitted' history row, and reopens the proposal.
// Keys that are not currently rejected are ignored.
func (r *SupplierRepository) ResubmitCatalogReview(ctx context.Context, supplierID, proposalID string, data map[string]interface{}) error {
	var rawData []byte
	err := r.db.QueryRowContext(ctx, `SELECT requested_data FROM pending_approvals WHERE id = $1 AND requested_by = $2`, proposalID, supplierID).Scan(&rawData)
	if err != nil {
		return err
	}
	rejRows, err := r.db.QueryContext(ctx, `SELECT field_key FROM proposal_field_reviews WHERE proposal_id = $1 AND decision = 'rejected'`, proposalID)
	if err != nil {
		return err
	}
	rejected := map[string]bool{}
	for rejRows.Next() {
		var k string
		if err := rejRows.Scan(&k); err != nil {
			rejRows.Close()
			return err
		}
		rejected[k] = true
	}
	rejRows.Close()
	if err := rejRows.Err(); err != nil {
		return err
	}
	parsed := map[string]interface{}{}
	_ = json.Unmarshal(rawData, &parsed)
	if parsed == nil {
		parsed = map[string]interface{}{}
	}
	applied := []string{}
	applyStr := func(dataKey, fieldKey, storeKey string) {
		if v, ok := data[dataKey].(string); ok && strings.TrimSpace(v) != "" && rejected[fieldKey] {
			parsed[storeKey] = strings.TrimSpace(v)
			applied = append(applied, fieldKey)
		}
	}
	applyStr("title", "title", "title")
	applyStr("description", "description", "description")
	applyStr("barcode", "barcode", "barcode")
	applyStr("categorySlug", "category", "categorySlug")
	applyStr("warranty", "warranty", "warranty")
	if v, ok := data["supplierBaseCost"].(float64); ok && rejected["price_c"] {
		parsed["supplierBaseCost"] = v
		applied = append(applied, "price_c")
	}
	if v, ok := data["stock"].(float64); ok && rejected["stock"] {
		parsed["stock"] = int64(v)
		applied = append(applied, "stock")
	}
	if v, ok := data["specs"].(map[string]interface{}); ok && rejected["specs"] {
		parsed["specs"] = v
		applied = append(applied, "specs")
	}
	if v, ok := data["imageUrls"].([]interface{}); ok && len(v) > 0 && rejected["images"] {
		parsed["imageUrls"] = v
		if first, ok := v[0].(string); ok {
			parsed["imageUrl"] = first
		}
		applied = append(applied, "images")
	}
	if v, ok := data["variants"].([]interface{}); ok && rejected["variants"] {
		parsed["variants"] = v
		applied = append(applied, "variants")
	}
	if len(applied) == 0 {
		return fmt.Errorf("no rejected fields corrected")
	}
	merged, err := json.Marshal(parsed)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE pending_approvals SET requested_data = $1::jsonb, status = 'pending' WHERE id = $2 AND requested_by = $3`, string(merged), proposalID, supplierID); err != nil {
		return err
	}
	for _, k := range applied {
		if _, err := tx.ExecContext(ctx, `UPDATE proposal_field_reviews SET decision = 'pending', reason = NULL WHERE proposal_id = $1 AND field_key = $2`, proposalID, k); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO proposal_review_history (proposal_id, action) VALUES ($1, 'resubmitted')`, proposalID); err != nil {
		return err
	}
	return tx.Commit()
}

// FIX 49 — live supplier profile tab (Tn): partial update. Only keys the
// caller passes are changed. A changed warehouse city resets confirmation
// (the server decides confirmation; the client only displays the flag).
func (r *SupplierRepository) UpdateSupplierProfile(ctx context.Context, supplierID string, description *string, warehouseCity *string, logoURL *string) (*domain.Supplier, error) {
	var currentCity string
	var currentConfirmed bool
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(warehouse_city, ''), COALESCE(warehouse_city_confirmed, false) FROM suppliers WHERE id = $1`, supplierID).Scan(&currentCity, &currentConfirmed)
	if err != nil {
		return nil, err
	}
	sets := []string{}
	args := []interface{}{}
	if description != nil {
		if len(*description) > 2000 {
			return nil, fmt.Errorf("description too long")
		}
		args = append(args, *description)
		sets = append(sets, fmt.Sprintf("description = $%d", len(args)))
	}
	if warehouseCity != nil {
		if *warehouseCity == "" {
			return nil, fmt.Errorf("warehouse city required")
		}
		args = append(args, *warehouseCity)
		sets = append(sets, fmt.Sprintf("warehouse_city = $%d", len(args)))
		if *warehouseCity != currentCity {
			sets = append(sets, "warehouse_city_confirmed = false")
		}
	}
	if logoURL != nil {
		args = append(args, *logoURL)
		sets = append(sets, fmt.Sprintf("logo_url = $%d", len(args)))
	}
	if len(sets) > 0 {
		args = append(args, supplierID)
		q := fmt.Sprintf("UPDATE suppliers SET %s WHERE id = $%d", strings.Join(sets, ", "), len(args))
		if _, err := r.db.ExecContext(ctx, q, args...); err != nil {
			return nil, err
		}
	}
	_ = currentConfirmed
	return r.GetSupplierByID(ctx, supplierID)
}

// --- Supplier search insight (D12) ----------------------------------
//
// search_queries carries no supplier key: a row is a normalized term, a
// store slug and result counts. The insight is therefore computed over
// PLATFORM-WIDE aggregates and then FOOTPRINT-SCOPED to the caller: a
// term is shown only when it matches the supplier's own catalog
// footprint (their product titles, their brands, or the titles of the
// categories their products sit in), and matched products are
// hard-filtered to the caller's own products. Store slugs, per-store
// splits, platform totals and other suppliers' products are never
// exposed.

const supplierSearchInsightMinSearches = 3
const supplierSearchInsightCandidateLimit = 300
const supplierSearchInsightSectionLimit = 20

type SupplierSearchInsightMatchedProduct struct {
	ProductID string `json:"productId"`
	Title     string `json:"title"`
	ImageURL  string `json:"imageUrl,omitempty"`
}

type SupplierSearchInsightDemandTerm struct {
	Term               string                                `json:"term"`
	Searches           int64                                 `json:"searches"`
	ZeroResultSearches int64                                 `json:"zeroResultSearches"`
	MatchedProducts    []SupplierSearchInsightMatchedProduct `json:"matchedProducts"`
}

type SupplierSearchInsightScopeMatch struct {
	Type string `json:"type"` // "category" | "brand"
	Name string `json:"name"`
}

type SupplierSearchInsightUnmetTerm struct {
	Term               string                            `json:"term"`
	Searches           int64                             `json:"searches"`
	ZeroResultSearches int64                             `json:"zeroResultSearches"`
	ScopeMatches       []SupplierSearchInsightScopeMatch `json:"scopeMatches"`
}

type SupplierSearchInsightTrendItem struct {
	ProductID string   `json:"productId"`
	Title     string   `json:"title"`
	Units     int64    `json:"units"`
	PrevUnits int64    `json:"prevUnits"`
	DeltaPct  *float64 `json:"deltaPct"` // null when prev = 0 and units > 0
}

type SupplierSearchInsight struct {
	WindowDays      int                               `json:"windowDays"`
	TopDemand       []SupplierSearchInsightDemandTerm `json:"topDemand"`
	UnmetInScope    []SupplierSearchInsightUnmetTerm  `json:"unmetInScope"`
	OwnProductTrend []SupplierSearchInsightTrendItem  `json:"ownProductTrend"`
}

// normalizeInsightTerm mirrors the search-log normalisation
// (cmd/server normalizeSearchTerm): trim, collapse whitespace runs and
// map Arabic Yeh/Kaf to Persian Yeh/Kaf. Applied to footprint names so
// they compare against logged terms on equal footing; idempotent on
// already-normalized terms.
func normalizeInsightTerm(s string) string {
	collapsed := strings.Join(strings.Fields(s), " ")
	var b strings.Builder
	for _, r := range collapsed {
		switch r {
		case 'ي':
			b.WriteRune('ی')
		case 'ك':
			b.WriteRune('ک')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

type supplierInsightFootprintEntry struct {
	typ        string // "brand" | "category"
	name       string // display name as stored
	normalized string
}

// matchSupplierInsightFootprint returns the footprint entries a
// normalized term matches: bidirectional substring on normalized forms
// (the term contains the name, or the name contains the term).
func matchSupplierInsightFootprint(term string, footprint []supplierInsightFootprintEntry) []supplierInsightFootprintEntry {
	if term == "" {
		return nil
	}
	var out []supplierInsightFootprintEntry
	for _, e := range footprint {
		if e.normalized == "" {
			continue
		}
		if strings.Contains(term, e.normalized) || strings.Contains(e.normalized, term) {
			out = append(out, e)
		}
	}
	return out
}

var supplierInsightLikeReplacer = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func (r *SupplierRepository) supplierInsightFootprint(ctx context.Context, supplierID string) ([]supplierInsightFootprintEntry, error) {
	var out []supplierInsightFootprintEntry
	seen := map[string]bool{}
	add := func(typ, name string) {
		normalized := normalizeInsightTerm(name)
		key := typ + "\x00" + normalized
		if normalized == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, supplierInsightFootprintEntry{typ: typ, name: name, normalized: normalized})
	}
	brandRows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT brand FROM products
		WHERE supplier_id = $1 AND brand <> ''
	`, supplierID)
	if err != nil {
		return nil, err
	}
	for brandRows.Next() {
		var brand string
		if err := brandRows.Scan(&brand); err != nil {
			brandRows.Close()
			return nil, err
		}
		add("brand", brand)
	}
	if err := brandRows.Err(); err != nil {
		brandRows.Close()
		return nil, err
	}
	brandRows.Close()
	catRows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT c.title FROM products p
		JOIN categories c ON c.slug = p.category_slug
		WHERE p.supplier_id = $1 AND c.title <> ''
	`, supplierID)
	if err != nil {
		return nil, err
	}
	for catRows.Next() {
		var title string
		if err := catRows.Scan(&title); err != nil {
			catRows.Close()
			return nil, err
		}
		add("category", title)
	}
	if err := catRows.Err(); err != nil {
		catRows.Close()
		return nil, err
	}
	catRows.Close()
	return out, nil
}

type supplierInsightCandidate struct {
	term          string
	searches      int64
	zeroSearches  int64
	positiveRows  int64
	totalRows     int64
	catalogRows   int64
	catalogZero   int64
	catalogKnown  bool
}

// GetSupplierSearchInsight computes the supplier demand-insight over a
// trailing window of `days` days (see the footprint-scoping note at the
// top of this section). A supplier with no products gets the same shape
// with every section empty.
func (r *SupplierRepository) GetSupplierSearchInsight(ctx context.Context, supplierID string, days int) (*SupplierSearchInsight, error) {
	if days != 7 && days != 30 && days != 90 {
		days = 30
	}
	out := &SupplierSearchInsight{
		WindowDays:      days,
		TopDemand:       []SupplierSearchInsightDemandTerm{},
		UnmetInScope:    []SupplierSearchInsightUnmetTerm{},
		OwnProductTrend: []SupplierSearchInsightTrendItem{},
	}
	now := time.Now()
	windowStart := now.AddDate(0, 0, -days)
	prevStart := now.AddDate(0, 0, -2*days)

	footprint, err := r.supplierInsightFootprint(ctx, supplierID)
	if err != nil {
		return nil, err
	}

	// Search sections are skipped (left empty) when the search log
	// table is absent, mirroring the seller handler's guard; the trend
	// below is computed regardless.
	var searchTableExists bool
	if err := r.db.QueryRowContext(ctx, `SELECT to_regclass('public.search_queries') IS NOT NULL`).Scan(&searchTableExists); err != nil {
		return nil, err
	}
	if searchTableExists {
		var catalogColExists bool
		if err := r.db.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'public' AND table_name = 'search_queries' AND column_name = 'catalog_result_count')
		`).Scan(&catalogColExists); err != nil {
			return nil, err
		}
		catalogExpr := "0"
		catalogZeroExpr := "0"
		if catalogColExists {
			catalogExpr = "COUNT(catalog_result_count)"
			catalogZeroExpr = "COALESCE(SUM(CASE WHEN catalog_result_count = 0 THEN 1 ELSE 0 END), 0)"
		}
		rows, err := r.db.QueryContext(ctx, `
			SELECT term_normalized,
			  COUNT(*) AS searches,
			  COALESCE(SUM(CASE WHEN result_count = 0 THEN 1 ELSE 0 END), 0) AS zero_searches,
			  COALESCE(SUM(CASE WHEN result_count > 0 THEN 1 ELSE 0 END), 0) AS positive_rows,
			  COUNT(*) AS total_rows,
			  `+catalogExpr+` AS catalog_rows,
			  `+catalogZeroExpr+` AS catalog_zero_rows
			FROM search_queries
			WHERE term_normalized <> '' AND created_at >= $1
			GROUP BY term_normalized
			HAVING COUNT(*) >= $2
			ORDER BY COUNT(*) DESC, term_normalized ASC
			LIMIT $3
		`, windowStart, supplierSearchInsightMinSearches, supplierSearchInsightCandidateLimit)
		if err != nil {
			return nil, err
		}
		var candidates []supplierInsightCandidate
		for rows.Next() {
			var c supplierInsightCandidate
			if err := rows.Scan(&c.term, &c.searches, &c.zeroSearches, &c.positiveRows, &c.totalRows, &c.catalogRows, &c.catalogZero); err != nil {
				rows.Close()
				return nil, err
			}
			c.catalogKnown = catalogColExists
			candidates = append(candidates, c)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()

		// Top demand: terms matching the caller's own products (by
		// title) or their brand/category footprint, in rank order.
		for _, c := range candidates {
			if len(out.TopDemand) >= supplierSearchInsightSectionLimit {
				break
			}
			pattern := "%" + supplierInsightLikeReplacer.Replace(c.term) + "%"
			mrows, err := r.db.QueryContext(ctx, `
				SELECT id, title, COALESCE(image_url, '') FROM products
				WHERE supplier_id = $1 AND is_active AND title ILIKE $2 ESCAPE '\'
				ORDER BY title LIMIT 4
			`, supplierID, pattern)
			if err != nil {
				return nil, err
			}
			matched := []SupplierSearchInsightMatchedProduct{}
			for mrows.Next() {
				var p SupplierSearchInsightMatchedProduct
				if err := mrows.Scan(&p.ProductID, &p.Title, &p.ImageURL); err != nil {
					mrows.Close()
					return nil, err
				}
				matched = append(matched, p)
			}
			if err := mrows.Err(); err != nil {
				mrows.Close()
				return nil, err
			}
			mrows.Close()
			footprintMatches := matchSupplierInsightFootprint(normalizeInsightTerm(c.term), footprint)
			if len(matched) == 0 && len(footprintMatches) == 0 {
				continue
			}
			out.TopDemand = append(out.TopDemand, SupplierSearchInsightDemandTerm{
				Term: c.term, Searches: c.searches, ZeroResultSearches: c.zeroSearches, MatchedProducts: matched,
			})
		}

		// Unmet in scope: never answered with a positive result, and
		// proven absent from the whole catalog by the log (every row's
		// catalog_result_count = 0; a NULL row means "unknown" and
		// disqualifies the term) — then verified once more against the
		// live catalog, because the writer conflates the two counts: a
		// live title match is a discoverability gap, not a supply gap.
		for _, c := range candidates {
			if len(out.UnmetInScope) >= supplierSearchInsightSectionLimit {
				break
			}
			if c.positiveRows != 0 || !c.catalogKnown || c.catalogRows != c.totalRows || c.catalogZero != c.totalRows {
				continue
			}
			footprintMatches := matchSupplierInsightFootprint(normalizeInsightTerm(c.term), footprint)
			if len(footprintMatches) == 0 {
				continue
			}
			pattern := "%" + supplierInsightLikeReplacer.Replace(c.term) + "%"
			var liveMatches int64
			if err := r.db.QueryRowContext(ctx, `
				SELECT COUNT(*) FROM products WHERE is_active AND title ILIKE $1 ESCAPE '\'
			`, pattern).Scan(&liveMatches); err != nil {
				return nil, err
			}
			if liveMatches > 0 {
				continue
			}
			scopeMatches := []SupplierSearchInsightScopeMatch{}
			for i, e := range footprintMatches {
				if i >= 3 {
					break
				}
				scopeMatches = append(scopeMatches, SupplierSearchInsightScopeMatch{Type: e.typ, Name: e.name})
			}
			out.UnmetInScope = append(out.UnmetInScope, SupplierSearchInsightUnmetTerm{
				Term: c.term, Searches: c.searches, ZeroResultSearches: c.zeroSearches, ScopeMatches: scopeMatches,
			})
		}
	}

	// Own product trend: paid-order units in the window vs the previous
	// equal window (same payment rule as GetSupplierAnalytics).
	trendRows, err := r.db.QueryContext(ctx, `
		SELECT p.id, p.title,
		  COALESCE(SUM(CASE WHEN o.created_at >= $2 THEN oi.quantity ELSE 0 END), 0) AS units,
		  COALESCE(SUM(CASE WHEN o.created_at < $2 THEN oi.quantity ELSE 0 END), 0) AS prev_units
		FROM order_items oi
		JOIN orders o ON o.id = oi.order_id
		JOIN products p ON p.id = oi.product_id
		WHERE p.supplier_id = $1 AND o.payment_status = 'paid' AND o.created_at >= $3
		GROUP BY p.id, p.title
		ORDER BY units DESC, p.title ASC
		LIMIT 5
	`, supplierID, windowStart, prevStart)
	if err != nil {
		return nil, err
	}
	defer trendRows.Close()
	for trendRows.Next() {
		var item SupplierSearchInsightTrendItem
		if err := trendRows.Scan(&item.ProductID, &item.Title, &item.Units, &item.PrevUnits); err != nil {
			return nil, err
		}
		if item.PrevUnits > 0 {
			pct := math.Round(float64(item.Units-item.PrevUnits)/float64(item.PrevUnits)*1000) / 10
			item.DeltaPct = &pct
		}
		out.OwnProductTrend = append(out.OwnProductTrend, item)
	}
	if err := trendRows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
