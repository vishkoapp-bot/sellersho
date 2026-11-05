package main

import (
	"context"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/repository"
)

// Supplier-field gate for the public product list/detail (FIX 14,
// auth-keyed). The store-scoped responses carry the supplier cost
// book (baseCostB, platformFeeF, supplierId, supplierName and, on
// detail, supplierWarranty) because the seller's own panel derives
// its pricing UI from them -- but they must never be served to
// anonymous or unrelated callers: customers see only the final
// price. The fields are therefore keyed to the caller's relationship
// with the viewed store / product:
//
//   - the owner of the viewed store (seller session),
//   - the supplier who owns the product (owner or team session,
//     resolved exactly like RequireSupplierAccess resolves it), or
//   - a platform admin.
//
// Everyone else -- anonymous callers, customers, other sellers --
// gets the central shape: the pointer fields stay nil (absent) and
// platformFeeF / supplierWarranty are zeroed.
//
// Identity comes from the JWT claims OptionalAuthMiddleware places
// in the request context on the public product routes (or
// AuthMiddleware on guarded routes); no token is parsed here.

// supplierFieldsViewer is the caller's resolved identity for the
// gate: who they are, whether they are a platform admin, and the
// supplier they act for ("" when they are not an active supplier).
type supplierFieldsViewer struct {
	userID     string
	isAdmin    bool
	supplierID string
}

// resolveSupplierFieldsViewer builds the viewer for one request. A
// request without valid claims yields the zero viewer, which sees
// nothing. Supplier identity resolution is best-effort: any lookup
// failure leaves supplierID empty, which can only hide fields,
// never show them.
func resolveSupplierFieldsViewer(ctx context.Context, supplierRepo *repository.SupplierRepository) supplierFieldsViewer {
	claims, ok := ctx.Value(userClaimsKey).(*auth.UserClaims)
	if !ok || claims == nil || claims.UserID == "" {
		return supplierFieldsViewer{}
	}
	viewer := supplierFieldsViewer{
		userID:  claims.UserID,
		isAdmin: claims.HasCapability("admin"),
	}
	if supplierRepo != nil {
		if supplierID, _, active, err := supplierRepo.GetSupplierAccess(ctx, claims.UserID); err == nil && active {
			viewer.supplierID = supplierID
		}
	}
	return viewer
}

// canSeeSupplierFields is the single gate consulted at both FIX 14
// decision points (list and detail), so the two surfaces cannot
// drift. store is the explicitly requested store (the ?storeSlug=
// view); the caller-side view precondition (an existing non-central
// store) stays with the handlers, exactly where FIX 14 put it.
func (v supplierFieldsViewer) canSeeSupplierFields(store *domain.Store, productSupplierID string) bool {
	if v.userID == "" {
		return false
	}
	if v.isAdmin {
		return true
	}
	if store != nil && store.OwnerUserID != nil && *store.OwnerUserID == v.userID {
		return true
	}
	return v.supplierID != "" && productSupplierID != "" && v.supplierID == productSupplierID
}
