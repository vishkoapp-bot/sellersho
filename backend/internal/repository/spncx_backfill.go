package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

var (
	ErrBackfillProductNotFound = errors.New("backfill product not found")
	ErrBackfillExcluded        = errors.New("backfill product excluded")
	ErrBackfillPreflightFailed = errors.New("backfill preflight failed")
	ErrBackfillExcessUndecided = errors.New("backfill excess decision required")
)

const backfillBasis = "products.stock"

type BackfillVariantInput struct {
	VariantID     string
	SPNC          string
	Stock         int
	IsDefault     bool
	InventoryMode string
	AlreadyMinted int
}

type BackfillVariantAlloc struct {
	VariantID     string `json:"variant_id"`
	SPNC          string `json:"spnc"`
	VariantStock  int    `json:"variant_stock"`
	Alloc         int    `json:"alloc"`
	AlreadyMinted int    `json:"already_minted"`
	IsDefault     bool   `json:"is_default"`
}

// AllocateBackfillVariants implements recommendation (b) + §5.3: products.stock is
// the source of truth (OQ-1). Allocation policy changes belong in this function.
func AllocateBackfillVariants(variants []BackfillVariantInput, p int) []BackfillVariantAlloc {
	ordered := append([]BackfillVariantInput(nil), variants...)
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.IsDefault != b.IsDefault {
			return !a.IsDefault
		}
		if a.Stock != b.Stock {
			return a.Stock > b.Stock
		}
		return a.VariantID < b.VariantID
	})
	alloc := make([]BackfillVariantAlloc, 0, len(ordered))
	remaining := max(0, p)
	for _, v := range ordered {
		n := min(max(0, v.Stock), remaining)
		alloc = append(alloc, BackfillVariantAlloc{
			VariantID: v.VariantID, SPNC: v.SPNC, VariantStock: v.Stock,
			Alloc: n, AlreadyMinted: v.AlreadyMinted, IsDefault: v.IsDefault,
		})
		remaining -= n
	}
	return alloc
}

type BackfillLotCreation struct {
	VariantID   string `json:"variant_id"`
	Quantity    int    `json:"quantity"`
	LocationKey string `json:"location_key"`
}

type BackfillLotReduction struct {
	BalanceID     string `json:"balance_id"`
	QuantityDelta int    `json:"quantity_delta"`
}

type BackfillVariantStockCorrection struct {
	VariantID string `json:"variant_id"`
	From      int    `json:"from"`
	To        int    `json:"to"`
}

type BackfillNewVariant struct {
	VariantID string `json:"variant_id"`
	Stock     int    `json:"stock"`
}

type BackfillVariantOrders struct {
	VariantID string `json:"variant_id"`
	SPNC      string `json:"spnc"`
	Orders    int    `json:"orders"`
}

type BackfillOrderHistory struct {
	DefaultEverOrdered bool                    `json:"default_ever_ordered"`
	NamedEverOrdered   bool                    `json:"named_ever_ordered"`
	Orders             int                     `json:"orders"`
	NamedVariantOrders []BackfillVariantOrders `json:"named_variant_orders"`
}

type BackfillProductPlan struct {
	ProductID               string                           `json:"product_id"`
	Title                   string                           `json:"title"`
	P                       int                              `json:"P"`
	Alloc                   []BackfillVariantAlloc           `json:"alloc"`
	LotsToCreate            []BackfillLotCreation            `json:"lots_to_create"`
	LotsToReduce            []BackfillLotReduction           `json:"lots_to_reduce"`
	TopUp                   int                              `json:"top_up"`
	Excess                  int                              `json:"excess"`
	ExcessDecision          *string                          `json:"excess_decision"`
	VariantStockCorrections []BackfillVariantStockCorrection `json:"variant_stock_corrections"`
	NewVariants             []BackfillNewVariant             `json:"new_variants"`
	OrderHistory            BackfillOrderHistory             `json:"order_history"`
	TrimTarget              *string                          `json:"trim_target"`
	Excluded                bool                             `json:"excluded"`
	ExcludedReason          *string                          `json:"excluded_reason"`
	FlippedVariants         []string                         `json:"flipped_variants"`

	UnitsTarget int    `json:"-"`
	Group       string `json:"-"`
}

type BackfillGroupTotals struct {
	DoubledNonDefault int `json:"doubled_non_default"`
	Equal             int `json:"equal"`
	Other             int `json:"other"`
	VariantLess       int `json:"variant_less"`
}

type BackfillAssertion struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Details string `json:"details"`
}

type BackfillPreCutoverOrders struct {
	Count            int `json:"count"`
	Items            int `json:"items"`
	OpenReservations int `json:"open_reservations"`
}

type BackfillPlan struct {
	DryRun               bool                     `json:"dry_run"`
	PlanHash             string                   `json:"plan_hash"`
	Basis                string                   `json:"basis"`
	TotalUnits           int                      `json:"total_units"`
	ProductCount         int                      `json:"product_count"`
	GroupTotals          BackfillGroupTotals      `json:"group_totals"`
	Products             []BackfillProductPlan    `json:"products"`
	GlobalAssertions     []BackfillAssertion      `json:"global_assertions"`
	DemoTestProducts     []string                 `json:"demo_test_products"`
	InactiveProducts     []string                 `json:"inactive_products"`
	PreCutoverOrders     BackfillPreCutoverOrders `json:"pre_cutover_orders"`
	DiffVsPreviousDryRun any                      `json:"diff_vs_previous_dry_run"`
}

func (p *BackfillPlan) AllPassed() bool {
	for _, assertion := range p.GlobalAssertions {
		if !assertion.Passed {
			return false
		}
	}
	return true
}

type BackfillExpectations struct {
	ZeroAllocVariants int
	GroupDoubled      int
	GroupEqual        int
	GroupOther        int
	GroupVariantLess  int
	TotalUnits        int
	ProductCount      int
}

func ProdBackfillExpectations() BackfillExpectations {
	return BackfillExpectations{
		ZeroAllocVariants: 40, GroupDoubled: 2324, GroupEqual: 260, GroupOther: 407,
		GroupVariantLess: 1025, TotalUnits: 4016, ProductCount: 59,
	}
}

// ComputePlanHash hashes the canonical run scope. Stock figures and generated
// variant IDs are deliberately omitted so the live plan can be compared separately.
func ComputePlanHash(productIDs []string, excludedIDs []string) string {
	products := append([]string{}, productIDs...)
	excluded := append([]string{}, excludedIDs...)
	sort.Strings(products)
	sort.Strings(excluded)
	raw, _ := json.Marshal(struct {
		ProductIDs []string `json:"product_ids"`
		Basis      string   `json:"basis"`
		Excluded   []string `json:"excluded"`
	}{products, backfillBasis, excluded})
	return fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
}

type backfillProduct struct {
	id, title, supplier, productType, originWarehouse string
	stock, schemaVersion                              int
	active                                            bool
}

type backfillVariant struct {
	BackfillVariantInput
	active bool
}

type backfillLocation struct {
	owner, kind, key             string
	warehouse, supplierWarehouse string
}

type backfillSnapshotBalance struct {
	ID          string  `json:"id"`
	Quantity    int     `json:"quantity"`
	Status      string  `json:"status"`
	VariantID   *string `json:"variant_id"`
	LocationKey string  `json:"location_key"`
}

type backfillBalance struct {
	backfillSnapshotBalance
	location    backfillLocation
	unitTracked bool
	reconciled  bool
}

type backfillState struct {
	status, planHash, newVariantID string
	excludedReason, excessDecision *string
}

func readBackfillProductTx(ctx context.Context, tx *sql.Tx, id string, lock bool) (backfillProduct, error) {
	var p backfillProduct
	query := `SELECT id,title,COALESCE(supplier_id,''),stock,schema_version,product_type,
		COALESCE(origin_supplier_warehouse_id,''),is_active FROM products WHERE id=$1`
	if lock {
		query += " FOR UPDATE"
	}
	err := tx.QueryRowContext(ctx, query, id).Scan(&p.id, &p.title, &p.supplier, &p.stock, &p.schemaVersion, &p.productType, &p.originWarehouse, &p.active)
	if errors.Is(err, sql.ErrNoRows) {
		err = fmt.Errorf("%w: %s", ErrBackfillProductNotFound, id)
	}
	return p, err
}

func readBackfillVariantsTx(ctx context.Context, tx *sql.Tx, productID string, lock bool) ([]backfillVariant, error) {
	query := `SELECT id,COALESCE(spnc,''),stock,is_default,inventory_mode,is_active FROM product_variants WHERE product_id=$1 ORDER BY id`
	if lock {
		query += " FOR UPDATE"
	}
	rows, err := tx.QueryContext(ctx, query, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var variants []backfillVariant
	for rows.Next() {
		var v backfillVariant
		if err := rows.Scan(&v.VariantID, &v.SPNC, &v.Stock, &v.IsDefault, &v.InventoryMode, &v.active); err != nil {
			return nil, err
		}
		variants = append(variants, v)
	}
	return variants, rows.Err()
}

func readBackfillStateTx(ctx context.Context, tx *sql.Tx, productID string, lock bool) (backfillState, error) {
	var state backfillState
	query := `SELECT status,excluded_reason,excess_decision,COALESCE(plan_hash,''),
		COALESCE(plan->'new_variants'->0->>'variant_id','')
		FROM spncx_backfill_state WHERE product_id=$1`
	if lock {
		query += " FOR UPDATE"
	}
	err := tx.QueryRowContext(ctx, query, productID).Scan(&state.status, &state.excludedReason, &state.excessDecision, &state.planHash, &state.newVariantID)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return state, err
}

func readBackfillBalancesTx(ctx context.Context, tx *sql.Tx, productID string, lock bool) ([]backfillBalance, error) {
	query := `SELECT id,quantity,status,variant_id,location_key,owner_type,location_type,
		COALESCE(warehouse_id,''),COALESCE(supplier_warehouse_id,''),unit_tracked,COALESCE(reconciled,FALSE)
		FROM inventory_balances WHERE product_id=$1 ORDER BY id`
	if lock {
		query += " FOR UPDATE"
	}
	rows, err := tx.QueryContext(ctx, query, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var balances []backfillBalance
	for rows.Next() {
		var b backfillBalance
		if err := rows.Scan(&b.ID, &b.Quantity, &b.Status, &b.VariantID, &b.LocationKey,
			&b.location.owner, &b.location.kind, &b.location.warehouse, &b.location.supplierWarehouse, &b.unitTracked, &b.reconciled); err != nil {
			return nil, err
		}
		b.location.key = b.LocationKey
		balances = append(balances, b)
	}
	return balances, rows.Err()
}

type backfillProductComputation struct {
	plan        BackfillProductPlan
	primary     backfillLocation
	unallocated []backfillBalance
	sellable    map[string][]backfillBalance
	failures    map[string][]string
	judgements  []string
	absorb      int
}

func (c *backfillProductComputation) fail(name, details string) {
	c.failures[name] = append(c.failures[name], details)
}

func (p *BackfillProductPlan) exclude(reason string) {
	p.Excluded = true
	p.ExcludedReason = &reason
}

func computeBackfillProductTx(ctx context.Context, tx *sql.Tx, p backfillProduct, variants []backfillVariant, balances []backfillBalance) (backfillProductComputation, error) {
	c := backfillProductComputation{
		plan: BackfillProductPlan{
			ProductID: p.id, Title: p.title, P: p.stock,
			Alloc: []BackfillVariantAlloc{}, LotsToCreate: []BackfillLotCreation{},
			LotsToReduce: []BackfillLotReduction{}, VariantStockCorrections: []BackfillVariantStockCorrection{},
			NewVariants: []BackfillNewVariant{}, FlippedVariants: []string{},
			OrderHistory: BackfillOrderHistory{NamedVariantOrders: []BackfillVariantOrders{}},
		},
		sellable: make(map[string][]backfillBalance),
		failures: make(map[string][]string),
	}
	var u, v0 int
	for _, b := range balances {
		if b.Status != "sellable" {
			continue
		}
		if b.VariantID == nil {
			c.unallocated = append(c.unallocated, b)
			u += b.Quantity
		} else {
			c.sellable[*b.VariantID] = append(c.sellable[*b.VariantID], b)
			v0 += b.Quantity
		}
	}
	sort.Slice(c.unallocated, func(i, j int) bool {
		a, b := c.unallocated[i], c.unallocated[j]
		if a.Quantity != b.Quantity {
			return a.Quantity > b.Quantity
		}
		return a.ID < b.ID
	})
	c.primary = backfillLocation{owner: "supplier", kind: "supplier", key: "supplier-warehouse:" + p.supplier}
	if len(c.unallocated) != 0 {
		c.primary = c.unallocated[0].location
	} else if p.supplier != "" && p.originWarehouse != "" {
		location, err := supplierDeclaredLocationTx(ctx, tx, p.id, p.supplier)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return c, err
		}
		c.primary.supplierWarehouse = strings.TrimPrefix(location, "supplier-warehouse:")
	}

	if len(variants) == 0 {
		c.plan.Group = "variant_less"
		switch {
		case p.schemaVersion >= 2 || p.productType == "variable":
			c.plan.exclude("variant_less_v2_or_variable")
		case len(c.unallocated) == 0 && c.primary.supplierWarehouse == "":
			c.plan.exclude("no_backfill_location")
		default:
			id := uuid.NewString()
			c.plan.NewVariants = append(c.plan.NewVariants, BackfillNewVariant{id, p.stock})
			c.plan.Alloc = append(c.plan.Alloc, BackfillVariantAlloc{VariantID: id, VariantStock: p.stock, Alloc: p.stock, IsDefault: true})
		}
	} else if err := c.planVariantsTx(ctx, tx, variants); err != nil {
		return c, err
	}
	c.planLedger(u, v0)
	return c, nil
}

func (c *backfillProductComputation) planVariantsTx(ctx context.Context, tx *sql.Tx, variants []backfillVariant) error {
	p := &c.plan
	orders := make(map[string]int, len(variants))
	input := make([]BackfillVariantInput, 0, len(variants))
	var stock, namedStock int
	var mirroredDefault bool
	for i := range variants {
		v := &variants[i]
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT
			(SELECT COUNT(*) FROM product_units WHERE variant_id=$1),
			(SELECT COUNT(*) FROM order_items WHERE variant_id=$1)`, v.VariantID).Scan(&v.AlreadyMinted, &n); err != nil {
			return err
		}
		input = append(input, v.BackfillVariantInput)
		orders[v.VariantID] = n
		p.OrderHistory.Orders += n
		stock += v.Stock
		if v.IsDefault {
			p.OrderHistory.DefaultEverOrdered = p.OrderHistory.DefaultEverOrdered || n > 0
			mirroredDefault = mirroredDefault || v.Stock == p.P
		} else {
			p.OrderHistory.NamedEverOrdered = p.OrderHistory.NamedEverOrdered || n > 0
			p.OrderHistory.NamedVariantOrders = append(p.OrderHistory.NamedVariantOrders, BackfillVariantOrders{v.VariantID, v.SPNC, n})
			namedStock += v.Stock
		}
	}
	switch {
	case mirroredDefault && namedStock == p.P && p.P > 0:
		p.Group = "doubled"
	case stock == p.P:
		p.Group = "equal"
	case stock > p.P:
		p.Group = "other"
	default:
		c.fail("variant_stock_covers_product", fmt.Sprintf("%s: variant stock %d < P %d", p.ProductID, stock, p.P))
	}
	p.Alloc = AllocateBackfillVariants(input, p.P)
	byID := make(map[string]BackfillVariantAlloc, len(p.Alloc))
	for _, a := range p.Alloc {
		byID[a.VariantID] = a
		// Only an "other" product (Σ variant stock > P) has a trim target: the
		// variant allocated last under the largest-first rule. A doubled
		// product's zero-alloc default is not a trim.
		if p.Group == "other" && a.Alloc < a.VariantStock {
			target := a.VariantID
			p.TrimTarget = &target
		}
	}
	spncs := make(map[string]string, len(variants))
	for _, v := range variants {
		a := byID[v.VariantID]
		if strings.TrimSpace(v.SPNC) == "" {
			c.fail("spnc_present_unique", v.VariantID+": missing SPNC")
		} else if first, ok := spncs[v.SPNC]; ok {
			c.fail("spnc_present_unique", fmt.Sprintf("%s: duplicate SPNC on %s and %s", v.SPNC, first, v.VariantID))
		}
		spncs[v.SPNC] = v.VariantID
		if !v.active && a.Alloc > 0 {
			// The product projection excludes sellable lots of inactive variants.
			c.fail("active_variants_cover_projection", fmt.Sprintf("%s: inactive variant with allocation %d", v.VariantID, a.Alloc))
		}
		if v.InventoryMode == "tracked" {
			var quantity int
			for _, b := range c.sellable[v.VariantID] {
				quantity += b.Quantity
			}
			if quantity != a.Alloc || v.Stock != a.Alloc {
				c.fail("tracked_lots_equal_alloc", fmt.Sprintf("%s: sellable %d, stock %d, alloc %d", v.VariantID, quantity, v.Stock, a.Alloc))
			}
			if p.Group == "doubled" && v.IsDefault {
				c.fail("no_tracked_doubled_default", p.ProductID+": "+v.VariantID)
			}
		} else if v.InventoryMode == "legacy" {
			p.FlippedVariants = append(p.FlippedVariants, v.VariantID)
			if v.Stock != a.Alloc {
				p.VariantStockCorrections = append(p.VariantStockCorrections, BackfillVariantStockCorrection{v.VariantID, v.Stock, a.Alloc})
			}
		}
	}
	c.checkOrderHistory(stock > p.P, orders)
	return nil
}

func (c *backfillProductComputation) checkOrderHistory(hasTrim bool, orders map[string]int) {
	p := &c.plan
	h := p.OrderHistory
	if p.Group == "doubled" {
		if h.DefaultEverOrdered || (h.Orders > 0 && !h.NamedEverOrdered) {
			p.exclude("v0a_order_history_flag")
		} else if h.Orders == 0 {
			c.judgements = append(c.judgements, p.ProductID+": V0a judgement call (no order history)")
		}
	}
	if !hasTrim || p.TrimTarget == nil {
		return
	}
	target := *p.TrimTarget
	var keptOrders int
	for _, a := range p.Alloc {
		if a.VariantID != target && a.Alloc > 0 {
			keptOrders = max(keptOrders, orders[a.VariantID])
		}
	}
	trimOrders := orders[target]
	if trimOrders > keptOrders && !p.Excluded {
		p.exclude("v0b_order_history_flag")
	}
	if (trimOrders > 0 && trimOrders == keptOrders) || h.Orders == 0 {
		c.judgements = append(c.judgements, fmt.Sprintf("%s: V0b judgement call (trim orders %d, most kept orders %d)", p.ProductID, trimOrders, keptOrders))
	}
}

func (c *backfillProductComputation) planLedger(u, v0 int) {
	p := &c.plan
	p.TopUp = max(0, p.P-u-v0)
	absorbed := p.P - p.TopUp
	c.absorb = min(u, absorbed)
	p.Excess = u - c.absorb
	for _, a := range p.Alloc {
		n := max(0, a.Alloc-a.AlreadyMinted)
		if n > 0 {
			p.LotsToCreate = append(p.LotsToCreate, BackfillLotCreation{a.VariantID, n, c.primary.key})
			p.UnitsTarget += n
		}
	}
	for _, lot := range c.unallocated {
		if lot.Quantity > 0 {
			p.LotsToReduce = append(p.LotsToReduce, BackfillLotReduction{lot.ID, -lot.Quantity})
		}
	}
}

func ComputeBackfillPlan(ctx context.Context, db *sql.DB, productIDs []string, exp BackfillExpectations) (*BackfillPlan, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	ids, err := backfillProductIDsTx(ctx, tx, productIDs)
	if err != nil {
		return nil, err
	}
	plan := &BackfillPlan{
		DryRun: true, Basis: backfillBasis, ProductCount: len(ids),
		Products: []BackfillProductPlan{}, DemoTestProducts: []string{}, InactiveProducts: []string{},
	}
	evidence := backfillPlanEvidence{failures: make(map[string][]string), spncs: make(map[string]string)}
	for _, id := range ids {
		p, err := readBackfillProductTx(ctx, tx, id, false)
		if err != nil {
			return nil, err
		}
		state, err := readBackfillStateTx(ctx, tx, id, false)
		if err != nil {
			return nil, err
		}
		if state.status == "completed" {
			c, err := readCompletedBackfillPlanTx(ctx, tx, id)
			if err != nil {
				return nil, err
			}
			evidence.addProduct(plan, p, c)
			continue
		}
		variants, err := readBackfillVariantsTx(ctx, tx, id, false)
		if err != nil {
			return nil, err
		}
		balances, err := readBackfillBalancesTx(ctx, tx, id, false)
		if err != nil {
			return nil, err
		}
		c, err := computeBackfillProductTx(ctx, tx, p, variants, balances)
		if err != nil {
			return nil, err
		}
		if state.status == "excluded" {
			c.plan.Excluded, c.plan.ExcludedReason = true, state.excludedReason
		}
		evidence.addProduct(plan, p, c)
	}
	if err := readBackfillOrderTotalsTx(ctx, tx, &plan.PreCutoverOrders); err != nil {
		return nil, err
	}
	plan.PlanHash = ComputePlanHash(ids, evidence.excluded)
	evidence.assert(plan, exp, len(productIDs) == 0)
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return plan, nil
}

func backfillProductIDsTx(ctx context.Context, tx *sql.Tx, requested []string) ([]string, error) {
	if len(requested) > 0 {
		ids := append([]string(nil), requested...)
		sort.Strings(ids)
		unique := ids[:1]
		for _, id := range ids[1:] {
			if id != unique[len(unique)-1] {
				unique = append(unique, id)
			}
		}
		return unique, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM products ORDER BY id COLLATE "C"`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func readBackfillOrderTotalsTx(ctx context.Context, tx *sql.Tx, orders *BackfillPreCutoverOrders) error {
	var hasReservations bool
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM orders),
		(SELECT COUNT(*) FROM order_items),to_regclass('inventory_reservations') IS NOT NULL`).
		Scan(&orders.Count, &orders.Items, &hasReservations); err != nil {
		return err
	}
	if hasReservations {
		return tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory_reservations WHERE status='active'`).Scan(&orders.OpenReservations)
	}
	return nil
}

type backfillPlanEvidence struct {
	failures                     map[string][]string
	spncs                        map[string]string
	excluded, excess, judgements []string
	equal, other, v0Covered      []string
	zero                         int
}

func (e *backfillPlanEvidence) addProduct(plan *BackfillPlan, p backfillProduct, c backfillProductComputation) {
	pp := c.plan
	plan.Products = append(plan.Products, pp)
	plan.TotalUnits += pp.P
	// There is no demo/test flag in the spec. Use the requested ID/title heuristic.
	if strings.HasPrefix(p.id, "demo-") || strings.Contains(p.id, "test") || strings.Contains(p.title, "تست") {
		plan.DemoTestProducts = append(plan.DemoTestProducts, p.id)
	}
	if !p.active {
		plan.InactiveProducts = append(plan.InactiveProducts, p.id)
	}
	if pp.Excluded {
		e.excluded = append(e.excluded, p.id)
	}
	if pp.Excess > 0 {
		e.excess = append(e.excess, fmt.Sprintf("%s (%d)", p.id, pp.Excess))
	}
	e.judgements = append(e.judgements, c.judgements...)
	for name, failures := range c.failures {
		e.failures[name] = append(e.failures[name], failures...)
	}
	for _, a := range pp.Alloc {
		if a.Alloc == 0 {
			e.zero++
		}
		if a.SPNC != "" {
			if first, ok := e.spncs[a.SPNC]; ok {
				e.failures["spnc_present_unique"] = append(e.failures["spnc_present_unique"], fmt.Sprintf("%s: duplicate SPNC on %s and %s", a.SPNC, first, a.VariantID))
			} else {
				e.spncs[a.SPNC] = a.VariantID
			}
		}
		if pp.Group == "doubled" && !a.IsDefault {
			plan.GroupTotals.DoubledNonDefault += a.Alloc
		}
	}
	switch pp.Group {
	case "equal":
		plan.GroupTotals.Equal += pp.P
		e.equal = append(e.equal, fmt.Sprintf("%s (%s): %d", pp.ProductID, pp.Title, pp.P))
	case "other":
		plan.GroupTotals.Other += pp.P
		e.other = append(e.other, pp.ProductID)
		if pp.TrimTarget != nil {
			e.v0Covered = append(e.v0Covered, pp.ProductID)
		}
	case "variant_less":
		plan.GroupTotals.VariantLess += pp.P
	}
}

func (e *backfillPlanEvidence) assert(plan *BackfillPlan, exp BackfillExpectations, full bool) {
	add := func(name string, passed bool, details string) {
		plan.GlobalAssertions = append(plan.GlobalAssertions, BackfillAssertion{name, passed, details})
	}
	for _, name := range []string{"variant_stock_covers_product", "tracked_lots_equal_alloc", "no_tracked_doubled_default", "spnc_present_unique", "active_variants_cover_projection"} {
		details := e.failures[name]
		add(name, len(details) == 0, strings.Join(details, "; "))
	}
	g := plan.GroupTotals
	want := BackfillGroupTotals{exp.GroupDoubled, exp.GroupEqual, exp.GroupOther, exp.GroupVariantLess}
	groupDetails := fmt.Sprintf("doubled_non_default=%d, equal=%d, other=%d, variant_less=%d; equal products: %s",
		g.DoubledNonDefault, g.Equal, g.Other, g.VariantLess, strings.Join(e.equal, ", "))
	if full {
		add("group_totals", g == want, fmt.Sprintf("%s; expected %+v", groupDetails, want))
		add("zero_alloc_count", e.zero == exp.ZeroAllocVariants, fmt.Sprintf("got %d, expected %d", e.zero, exp.ZeroAllocVariants))
		add("total_units", plan.TotalUnits == exp.TotalUnits, fmt.Sprintf("got %d, expected %d", plan.TotalUnits, exp.TotalUnits))
		add("product_count", plan.ProductCount == exp.ProductCount, fmt.Sprintf("got %d, expected %d", plan.ProductCount, exp.ProductCount))
	} else {
		add("group_totals", g.DoubledNonDefault+g.Equal+g.Other+g.VariantLess == plan.TotalUnits,
			fmt.Sprintf("%s; subset total %d; catalogue expectations skipped", groupDetails, plan.TotalUnits))
		add("zero_alloc_count", true, fmt.Sprintf("subset run: %d", e.zero))
		add("total_units", true, "subset run")
	}
	add("v0_coverage", len(e.v0Covered) == len(e.other), fmt.Sprintf("V0b covered [%s] of [%s]; %s",
		strings.Join(e.v0Covered, ", "), strings.Join(e.other, ", "), strings.Join(e.judgements, "; ")))
	o := plan.PreCutoverOrders
	add("pre_cutover_orders", true, fmt.Sprintf("orders=%d, items=%d, open reservations=%d", o.Count, o.Items, o.OpenReservations))
	add("excess_decisions", true, "Products requiring a decision before backfill: "+strings.Join(e.excess, ", "))
}

type BackfillProductResult struct {
	ProductID       string
	Status          string
	UnitsMinted     int
	UnitsTarget     int
	FlippedVariants []string
	NewVariantIDs   []string
	Excess          int
	PrintedCodes    []string
}

func BackfillProduct(ctx context.Context, db *sql.DB, productID, actor, planHash string) (*BackfillProductResult, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	result := &BackfillProductResult{
		ProductID: productID, Status: "failed", FlippedVariants: []string{},
		NewVariantIDs: []string{}, PrintedCodes: []string{},
	}
	attempt, reason, err := backfillProductTx(ctx, tx, productID, actor, planHash)
	if err == nil {
		if attempt.Status == "skipped_completed" {
			return attempt, nil
		}
		if err = tx.Commit(); err == nil {
			return attempt, nil
		}
	}
	_ = tx.Rollback()
	switch {
	case errors.Is(err, ErrBackfillProductNotFound), errors.Is(err, ErrBackfillExcessUndecided):
		return result, err
	case errors.Is(err, ErrBackfillExcluded):
		result.Status = "excluded"
		if reason == nil {
			return result, err // A durable veto already exists.
		}
	}

	// This statement runs in its own transaction after the product rollback.
	// Guard completed state in case another attempt succeeded in the meantime.
	stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if stateErr := recordBackfillFailure(stateCtx, db, productID, result.Status, reason, err); stateErr != nil {
		err = errors.Join(err, fmt.Errorf("record backfill %s: %w", result.Status, stateErr))
	}
	return result, err
}

func recordBackfillFailure(ctx context.Context, db *sql.DB, id, status string, reason *string, cause error) error {
	var message *string
	if status == "failed" {
		detail := cause.Error()
		message = &detail
	}
	_, err := db.ExecContext(ctx, `INSERT INTO spncx_backfill_state AS s
		(product_id,status,units_target,error,excluded_reason,excluded_at,started_at)
		SELECT id,$2::text,0,$3,$4,CASE WHEN $2='excluded' THEN NOW() END,NOW() FROM products WHERE id=$1
		ON CONFLICT (product_id) DO UPDATE SET status=EXCLUDED.status,error=EXCLUDED.error,
			excluded_reason=EXCLUDED.excluded_reason,excluded_at=EXCLUDED.excluded_at,
			started_at=COALESCE(s.started_at,EXCLUDED.started_at)
		WHERE s.status IS DISTINCT FROM 'completed' AND s.status IS DISTINCT FROM 'excluded'`, id, status, message, reason)
	return err
}

func backfillProductTx(ctx context.Context, tx *sql.Tx, id, actor, hash string) (*BackfillProductResult, *string, error) {
	p, err := readBackfillProductTx(ctx, tx, id, true)
	if err != nil {
		return nil, nil, err
	}
	variants, err := readBackfillVariantsTx(ctx, tx, id, true)
	if err != nil {
		return nil, nil, err
	}
	for _, v := range variants {
		if err := lockExistingUnitCounterTx(ctx, tx, v.VariantID); err != nil {
			return nil, nil, err
		}
	}
	state, err := readBackfillStateTx(ctx, tx, id, true)
	if err != nil {
		return nil, nil, err
	}
	switch state.status {
	case "completed":
		return &BackfillProductResult{ProductID: id, Status: "skipped_completed"}, nil, nil
	case "excluded":
		return nil, nil, fmt.Errorf("%w: %s", ErrBackfillExcluded, id)
	}
	balances, err := readBackfillBalancesTx(ctx, tx, id, true)
	if err != nil {
		return nil, nil, err
	}
	c, err := computeBackfillProductTx(ctx, tx, p, variants, balances)
	if err != nil {
		return nil, nil, err
	}
	plan := &c.plan
	if plan.Excluded {
		return nil, plan.ExcludedReason, fmt.Errorf("%w: %s: %s", ErrBackfillExcluded, id, *plan.ExcludedReason)
	}
	if len(c.failures) > 0 {
		var details []string
		for name, failures := range c.failures {
			details = append(details, name+": "+strings.Join(failures, ", "))
		}
		sort.Strings(details)
		return nil, nil, fmt.Errorf("%w: %s", ErrBackfillPreflightFailed, strings.Join(details, "; "))
	}
	if plan.Excess > 0 && state.excessDecision == nil {
		return nil, nil, fmt.Errorf("%w: %s (%d)", ErrBackfillExcessUndecided, id, plan.Excess)
	}
	plan.ExcessDecision = state.excessDecision
	// A persisted plan for the approved scope already names the new variant.
	if len(plan.NewVariants) > 0 && hash != "" && state.planHash == hash && state.newVariantID != "" {
		approvedID, err := uuid.Parse(state.newVariantID)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid approved backfill variant ID: %w", err)
		}
		plan.NewVariants[0].VariantID = approvedID.String()
		plan.Alloc[0].VariantID = approvedID.String()
		if len(plan.LotsToCreate) > 0 {
			plan.LotsToCreate[0].VariantID = approvedID.String()
		}
	}
	if err := writeBackfillSnapshotTx(ctx, tx, p, variants, balances); err != nil {
		return nil, nil, fmt.Errorf("backfill snapshot: %w", err)
	}
	if len(plan.NewVariants) > 0 {
		variants, err = createBackfillVariantTx(ctx, tx, p, plan)
		if err != nil {
			return nil, nil, fmt.Errorf("create backfill variant: %w", err)
		}
	}
	minted, err := c.mintAndFlipTx(ctx, tx, p, variants, actor)
	if err != nil {
		return nil, nil, err
	}
	if err := c.reconcileBackfillLedgerTx(ctx, tx, p, actor, balances); err != nil {
		return nil, nil, fmt.Errorf("backfill ledger: %w", err)
	}
	if err := verifyBackfillProductTx(ctx, tx, *plan); err != nil {
		return nil, nil, err
	}
	if err := completeBackfillProductTx(ctx, tx, *plan, minted, hash); err != nil {
		return nil, nil, fmt.Errorf("complete backfill: %w", err)
	}
	result := &BackfillProductResult{
		ProductID: id, Status: "completed", UnitsMinted: minted, UnitsTarget: plan.UnitsTarget,
		FlippedVariants: plan.FlippedVariants, NewVariantIDs: []string{}, Excess: plan.Excess, PrintedCodes: []string{},
	}
	for _, v := range plan.NewVariants {
		result.NewVariantIDs = append(result.NewVariantIDs, v.VariantID)
	}
	return result, nil, nil
}

func writeBackfillSnapshotTx(ctx context.Context, tx *sql.Tx, p backfillProduct, variants []backfillVariant, balances []backfillBalance) error {
	snapshot := struct {
		ProductStock  int                       `json:"product_stock"`
		VariantStocks map[string]int            `json:"variant_stocks"`
		Balances      []backfillSnapshotBalance `json:"balances"`
	}{p.stock, make(map[string]int, len(variants)), make([]backfillSnapshotBalance, 0, len(balances))}
	for _, v := range variants {
		snapshot.VariantStocks[v.VariantID] = v.Stock
	}
	for _, b := range balances {
		snapshot.Balances = append(snapshot.Balances, b.backfillSnapshotBalance)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO spncx_backfill_snapshot(product_id,snapshot) VALUES($1,$2::jsonb)
		ON CONFLICT(product_id) DO UPDATE SET snapshot=EXCLUDED.snapshot,created_at=NOW()`, p.id, string(raw))
	return err
}

func createBackfillVariantTx(ctx context.Context, tx *sql.Tx, p backfillProduct, plan *BackfillProductPlan) ([]backfillVariant, error) {
	v := plan.NewVariants[0]
	spnc, err := uniqueSPNCTx(ctx, tx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO product_variants
		(id,product_id,spnc,inventory_mode,is_default,stock,variant_name,attributes)
		VALUES($1,$2,$3,'tracked',TRUE,$4,$5,'{}')`, v.VariantID, p.id, spnc, v.Stock, p.title); err != nil {
		return nil, err
	}
	plan.Alloc[0].SPNC = spnc
	return readBackfillVariantsTx(ctx, tx, p.id, true)
}

func (c *backfillProductComputation) mintAndFlipTx(ctx context.Context, tx *sql.Tx, p backfillProduct, variants []backfillVariant, actor string) (int, error) {
	alloc := make(map[string]BackfillVariantAlloc, len(c.plan.Alloc))
	for _, a := range c.plan.Alloc {
		alloc[a.VariantID] = a
	}
	c.plan.FlippedVariants = c.plan.FlippedVariants[:0]
	var minted int
	for _, v := range variants {
		a := alloc[v.VariantID]
		n, err := c.mintVariantTx(ctx, tx, p, v.BackfillVariantInput, a, actor)
		if err != nil {
			return 0, fmt.Errorf("backfill variant %s mint: %w", v.VariantID, err)
		}
		minted += n
		flipped, err := flipBackfillVariantTx(ctx, tx, v.BackfillVariantInput, a.Alloc)
		if err != nil {
			return 0, fmt.Errorf("backfill variant %s flip: %w", v.VariantID, err)
		}
		if flipped {
			c.plan.FlippedVariants = append(c.plan.FlippedVariants, v.VariantID)
		}
		// Existing lots with full unit coverage may predate the unit_tracked flag.
		for _, lot := range c.sellable[v.VariantID] {
			if err := refreshUnitTrackedLotTx(ctx, tx, lot.ID); err != nil {
				return 0, err
			}
		}
	}
	return minted, nil
}

func (c *backfillProductComputation) mintVariantTx(ctx context.Context, tx *sql.Tx, p backfillProduct, v BackfillVariantInput, a BackfillVariantAlloc, actor string) (int, error) {
	n := max(0, a.Alloc-a.AlreadyMinted)
	if n == 0 {
		return 0, nil
	}
	var lotID string
	if lots := c.sellable[v.VariantID]; len(lots) > 0 {
		// These rows were read under lock, after the counter locks.
		lotID = lots[0].ID
		if v.InventoryMode == "tracked" {
			var quantity int
			for _, lot := range lots {
				quantity += lot.Quantity
			}
			if quantity != a.Alloc {
				return 0, fmt.Errorf("%w: tracked sellable quantity %d != alloc %d", ErrBackfillPreflightFailed, quantity, a.Alloc)
			}
		}
	} else {
		if len(c.unallocated) == 0 && p.supplier == "" {
			return 0, errors.New("backfill lot requires a supplier or an unallocated location")
		}
		// mintUnitsTx credits the destination itself. Start at zero so the
		// inserted lot and the mint together create exactly n sellable units.
		var err error
		lotID, err = insertBackfillLotTx(ctx, tx, p, c.primary, v.VariantID, "sellable", 0, true)
		if err != nil {
			return 0, err
		}
	}
	units, err := mintUnitsTx(ctx, tx, v.VariantID, p.id, lotID, n, "in_stock", "spncx_backfill", p.id, actor)
	return len(units), err
}

func flipBackfillVariantTx(ctx context.Context, tx *sql.Tx, v BackfillVariantInput, alloc int) (bool, error) {
	res, err := tx.ExecContext(ctx, `UPDATE product_variants SET inventory_mode='tracked' WHERE id=$1 AND inventory_mode='legacy'`, v.VariantID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 1 {
		if v.Stock != alloc {
			// §5.5 step 7 is the final legacy stock correction.
			_, err = tx.ExecContext(ctx, `UPDATE product_variants SET stock=$2 WHERE id=$1`, v.VariantID, alloc)
		}
		return true, err
	}
	if v.InventoryMode != "tracked" {
		return false, fmt.Errorf("could not flip variant in mode %q", v.InventoryMode)
	}
	return false, nil
}

func insertBackfillLotTx(ctx context.Context, tx *sql.Tx, p backfillProduct, location backfillLocation, variantID, status string, quantity int, tracked bool) (string, error) {
	id := "INV-" + uuid.NewString()
	_, err := tx.ExecContext(ctx, `INSERT INTO inventory_balances
		(id,product_id,supplier_id,variant_id,owner_type,location_type,location_key,warehouse_id,supplier_warehouse_id,status,quantity,unit_tracked,reconciled)
		VALUES($1,$2,NULLIF($3,''),NULLIF($4,'')::uuid,$5,$6,$7,NULLIF($8,''),NULLIF($9,''),$10,$11,$12,$12)`,
		id, p.id, p.supplier, variantID, location.owner, location.kind, location.key, location.warehouse, location.supplierWarehouse, status, quantity, tracked)
	return id, err
}

func (c *backfillProductComputation) reconcileBackfillLedgerTx(ctx context.Context, tx *sql.Tx, p backfillProduct, actor string, balances []backfillBalance) error {
	remaining := c.absorb
	var excessLots []string
	for _, lot := range c.unallocated {
		amount := min(lot.Quantity, remaining)
		remaining -= amount
		if amount > 0 {
			if err := reduceBackfillLotTx(ctx, tx, lot.ID, amount); err != nil {
				return err
			}
			if err := insertBackfillMovementTx(ctx, tx, p, actor, "absorb:"+lot.ID, amount,
				"sellable", "sellable", lot.LocationKey, lot.LocationKey,
				// inventory_movements.quantity carries CHECK (quantity > 0) on the
				// migrated schema, so the row records the absorbed magnitude and
				// the signed direction lives in metadata (mirroring the excess
				// movement's from_quantity_delta below).
				map[string]any{"reason": "ledger_absorb", "balance_id": lot.ID, "quantity_delta": -amount}); err != nil {
				return err
			}
		}
		if lot.Quantity > amount {
			excessLots = append(excessLots, lot.ID)
		}
	}
	if c.plan.TopUp > 0 {
		if err := insertBackfillMovementTx(ctx, tx, p, actor, "topup", c.plan.TopUp, "external", "sellable",
			"external", c.primary.key, map[string]any{"reason": "ledger_top_up"}); err != nil {
			return err
		}
	}
	if c.plan.Excess == 0 {
		return nil
	}

	remaining = c.absorb
	for _, lot := range c.unallocated {
		absorbed := min(lot.Quantity, remaining)
		remaining -= absorbed
		if leftover := lot.Quantity - absorbed; leftover > 0 {
			if err := reduceBackfillLotTx(ctx, tx, lot.ID, leftover); err != nil {
				return err
			}
		}
	}
	return c.holdBackfillExcessTx(ctx, tx, p, actor, balances, excessLots)
}

func reduceBackfillLotTx(ctx context.Context, tx *sql.Tx, id string, quantity int) error {
	res, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=quantity-$2,updated_at=NOW()
		WHERE id=$1 AND quantity >= $2`, id, quantity)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("unallocated lot %s cannot cover reduction %d", id, quantity)
	}
	return nil
}

func (c *backfillProductComputation) holdBackfillExcessTx(ctx context.Context, tx *sql.Tx, p backfillProduct, actor string, balances []backfillBalance, fromIDs []string) error {
	location := c.primary
	location.key = "spncx-excess:" + p.id
	excess := c.plan.Excess
	var holdingID string
	for _, b := range balances {
		if b.LocationKey != location.key {
			continue
		}
		if holdingID != "" || b.Quantity != excess || b.Status != "unreconciled" || b.VariantID != nil || b.unitTracked || b.reconciled {
			return fmt.Errorf("holding lot %s does not match expected excess %d", b.ID, excess)
		}
		holdingID = b.ID
	}
	if holdingID == "" {
		var err error
		holdingID, err = insertBackfillLotTx(ctx, tx, p, location, "", "unreconciled", excess, false)
		if err != nil {
			return err
		}
	}
	return insertBackfillMovementTx(ctx, tx, p, actor, "excess", excess, "sellable", "unreconciled", c.primary.key, location.key, map[string]any{
		"reason": "ledger_excess_held", "from_lot_ids": fromIDs, "from_quantity_delta": -excess,
		"to_lot_id": holdingID, "to_quantity_delta": excess,
	})
}

func insertBackfillMovementTx(ctx context.Context, tx *sql.Tx, p backfillProduct, actor, step string, quantity int, from, to, fromLocation, toLocation string, metadata map[string]any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO inventory_movements
		(id,product_id,supplier_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,actor_user_id,metadata)
		VALUES($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8,'spncx_backfill',$2,$9,NULLIF($10,''),$11::jsonb)`,
		"IM-"+uuid.NewString(), p.id, p.supplier, quantity, from, to, fromLocation, toLocation,
		"spncx-backfill:"+p.id+":"+step, actor, string(raw))
	return err
}

func verifyBackfillProductTx(ctx context.Context, tx *sql.Tx, p BackfillProductPlan) error {
	var stock, sellable, unallocated int
	if err := tx.QueryRowContext(ctx, `SELECT p.stock,
		COALESCE(SUM(b.quantity) FILTER (WHERE b.variant_id IS NOT NULL),0),
		COALESCE(SUM(b.quantity) FILTER (WHERE b.variant_id IS NULL),0)
		FROM products p LEFT JOIN inventory_balances b ON b.product_id=p.id AND b.status='sellable'
		WHERE p.id=$1 GROUP BY p.id,p.stock`, p.ProductID).Scan(&stock, &sellable, &unallocated); err != nil {
		return err
	}
	if stock != p.P || sellable != p.P || unallocated != 0 {
		return fmt.Errorf("backfill %s projection: P=%d, stock=%d, variant sellable=%d, unallocated sellable=%d", p.ProductID, p.P, stock, sellable, unallocated)
	}
	for _, a := range p.Alloc {
		var variantStock, variantSellable, inStock int
		var tracked, covered bool
		if err := tx.QueryRowContext(ctx, `SELECT v.stock,v.inventory_mode='tracked',
			(SELECT COALESCE(SUM(quantity),0) FROM inventory_balances WHERE variant_id=v.id AND status='sellable'),
			(SELECT COUNT(*) FROM product_units WHERE variant_id=v.id AND status='in_stock'),
			NOT EXISTS (SELECT 1 FROM inventory_balances b WHERE b.variant_id=v.id AND b.status='sellable'
				AND (NOT b.unit_tracked OR b.quantity<>(SELECT COUNT(*) FROM product_units u WHERE u.balance_id=b.id AND u.status='in_stock')))
			FROM product_variants v WHERE v.id=$1`, a.VariantID).
			Scan(&variantStock, &tracked, &variantSellable, &inStock, &covered); err != nil {
			return err
		}
		if !tracked || !covered || variantStock != a.Alloc || variantSellable != a.Alloc || inStock != a.Alloc {
			return fmt.Errorf("backfill variant %s coverage: alloc=%d, stock=%d, sellable=%d, in_stock=%d, tracked=%t, lots covered=%t",
				a.VariantID, a.Alloc, variantStock, variantSellable, inStock, tracked, covered)
		}
	}
	return nil
}

func completeBackfillProductTx(ctx context.Context, tx *sql.Tx, p BackfillProductPlan, minted int, hash string) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO spncx_backfill_state AS s
		(product_id,status,units_minted,units_target,plan,plan_hash,excess_amount,started_at,completed_at)
		VALUES($1,'completed',$2,$3,$4::jsonb,$5,$6,NOW(),NOW())
		ON CONFLICT (product_id) DO UPDATE SET status='completed',units_minted=EXCLUDED.units_minted,
			units_target=EXCLUDED.units_target,plan=EXCLUDED.plan,plan_hash=EXCLUDED.plan_hash,
			excess_amount=EXCLUDED.excess_amount,error=NULL,started_at=COALESCE(s.started_at,EXCLUDED.started_at),
			completed_at=EXCLUDED.completed_at
		WHERE s.status NOT IN ('completed','excluded')
			AND (EXCLUDED.excess_amount=0 OR s.excess_decision IS NOT NULL)`,
		p.ProductID, minted, p.UnitsTarget, string(raw), hash, p.Excess)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("backfill state for %s changed during the run", p.ProductID)
	}
	return nil
}

// Completed products still contribute their original allocation to preflight.
// Reclassifying their corrected stock would make full-catalogue resumes fail
// the original group expectations.
func readCompletedBackfillPlanTx(ctx context.Context, tx *sql.Tx, id string) (backfillProductComputation, error) {
	var c backfillProductComputation
	var raw []byte
	var unitsTarget int
	if err := tx.QueryRowContext(ctx, `SELECT plan,units_target FROM spncx_backfill_state
		WHERE product_id=$1 AND status='completed'`, id).Scan(&raw, &unitsTarget); err != nil {
		return c, err
	}
	p := &c.plan
	if err := json.Unmarshal(raw, p); err != nil {
		return c, fmt.Errorf("read completed backfill plan for %s: %w", id, err)
	}
	if p.ProductID != id {
		return c, fmt.Errorf("completed backfill plan for %s has product ID %q", id, p.ProductID)
	}
	p.UnitsTarget = unitsTarget
	var stock, namedStock int
	var mirroredDefault bool
	for _, a := range p.Alloc {
		stock += a.VariantStock
		if a.IsDefault {
			mirroredDefault = mirroredDefault || a.VariantStock == p.P
		} else {
			namedStock += a.VariantStock
		}
	}
	switch {
	case len(p.NewVariants) > 0:
		p.Group = "variant_less"
	case mirroredDefault && namedStock == p.P && p.P > 0:
		p.Group = "doubled"
	case stock == p.P:
		p.Group = "equal"
	default:
		p.Group = "other"
	}
	return c, nil
}

type BackfillRunOptions struct {
	ProductIDs   []string
	BatchSize    int
	Actor        string
	PlanHash     string
	Expectations BackfillExpectations
	Reinclude    map[string]string
}

type BackfillRunResult struct {
	Plan             *BackfillPlan
	Products         []BackfillProductResult
	Completed        int
	SkippedCompleted int
	Failed           int
	ExcludedSkipped  int
	TotalMinted      int
}

const backfillAdvisoryLockID = 918273

var ErrBackfillAlreadyRunning = errors.New("backfill already running")

func RunBackfill(ctx context.Context, db *sql.DB, opts BackfillRunOptions) (_ *BackfillRunResult, runErr error) {
	plan, err := ComputeBackfillPlan(ctx, db, opts.ProductIDs, opts.Expectations)
	if err != nil {
		return nil, err
	}
	result := &BackfillRunResult{Plan: plan, Products: make([]BackfillProductResult, 0, len(plan.Products))}
	if !plan.AllPassed() {
		return result, ErrBackfillPreflightFailed
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return result, err
	}
	defer conn.Close()
	var locked bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, backfillAdvisoryLockID).Scan(&locked); err != nil {
		// The server may have acquired the lock before the query was canceled.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return result, err
	}
	if !locked {
		return result, ErrBackfillAlreadyRunning
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var unlocked bool
		err := conn.QueryRowContext(unlockCtx, `SELECT pg_advisory_unlock($1)`, backfillAdvisoryLockID).Scan(&unlocked)
		if err != nil || !unlocked {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			if err == nil {
				err = errors.New("backfill advisory lock was not held")
			}
			runErr = errors.Join(runErr, fmt.Errorf("release backfill lock: %w", err))
		}
	}()
	if len(plan.Products) > 0 && db.Stats().MaxOpenConnections == 1 {
		return result, errors.New("backfill requires a pool with at least two connections")
	}

	if opts.BatchSize <= 0 {
		opts.BatchSize = 10
	}
	if opts.PlanHash == "" {
		opts.PlanHash = plan.PlanHash
	}
	for start := 0; start < len(plan.Products); {
		end := start + min(opts.BatchSize, len(plan.Products)-start)
		for _, p := range plan.Products[start:end] {
			product := runBackfillProduct(ctx, db, p.ProductID, opts)
			result.Products = append(result.Products, product)
			switch product.Status {
			case "completed":
				result.Completed++
				result.TotalMinted += product.UnitsMinted
			case "skipped_completed":
				result.SkippedCompleted++
			case "excluded_skipped":
				result.ExcludedSkipped++
			case "failed":
				result.Failed++
			}
		}
		start = end
	}
	return result, ctx.Err()
}

func runBackfillProduct(ctx context.Context, db *sql.DB, id string, opts BackfillRunOptions) BackfillProductResult {
	result := BackfillProductResult{ProductID: id, Status: "failed"}
	if _, reinclude := opts.Reinclude[id]; reinclude {
		// Migration 202611230002 has no re-include audit column. The caller
		// (B6's admin endpoint) must audit-log the reason before clearing the veto.
		if _, err := db.ExecContext(ctx, `UPDATE spncx_backfill_state SET status='pending',
			excluded_reason=NULL,excluded_by=NULL,excluded_at=NULL
			WHERE product_id=$1 AND status='excluded'`, id); err != nil {
			return result
		}
	} else {
		var excluded bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM spncx_backfill_state WHERE product_id=$1 AND status='excluded')`,
			id).Scan(&excluded); err != nil {
			return result
		}
		if excluded {
			result.Status = "excluded_skipped"
			return result
		}
	}
	attempt, err := BackfillProduct(ctx, db, id, opts.Actor, opts.PlanHash)
	if err != nil {
		return result
	}
	return *attempt
}

type BackfillRollbackResult struct {
	ProductID             string
	RolledBack            bool
	PrintedCodesToDestroy []string
	RestoredSnapshot      bool
}

var (
	ErrBackfillRollbackRefused           = errors.New("backfill rollback refused")
	ErrBackfillRollbackNeedsConfirmation = errors.New("backfill rollback needs printed-code confirmation")
	ErrBackfillNotCompleted              = errors.New("backfill product not completed")
)

type backfillRollbackSnapshot struct {
	ProductStock  *int                      `json:"product_stock"`
	VariantStocks map[string]int            `json:"variant_stocks"`
	Balances      []backfillSnapshotBalance `json:"balances"`
}

func RollbackBackfill(ctx context.Context, db *sql.DB, productID, actor string, confirmPrintedDestruction bool) (*BackfillRollbackResult, error) {
	result := &BackfillRollbackResult{ProductID: productID, PrintedCodesToDestroy: []string{}}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()

	if err := rollbackBackfillTx(ctx, tx, result, confirmPrintedDestruction); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	result.RolledBack, result.RestoredSnapshot = true, true
	return result, nil
}

func rollbackBackfillTx(ctx context.Context, tx *sql.Tx, result *BackfillRollbackResult, confirmPrintedDestruction bool) error {
	id := result.ProductID
	_, err := readBackfillProductTx(ctx, tx, id, true)
	if errors.Is(err, ErrBackfillProductNotFound) {
		return ErrBackfillNotCompleted
	}
	if err != nil {
		return err
	}
	variants, err := readBackfillVariantsTx(ctx, tx, id, true)
	if err != nil {
		return err
	}
	for _, v := range variants {
		if err := lockExistingUnitCounterTx(ctx, tx, v.VariantID); err != nil {
			return err
		}
	}
	var rawPlan []byte
	if err := tx.QueryRowContext(ctx, `SELECT plan FROM spncx_backfill_state
		WHERE product_id=$1 AND status='completed' FOR UPDATE`, id).Scan(&rawPlan); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrBackfillNotCompleted
		}
		return err
	}
	if spncxStrict {
		return fmt.Errorf("%w: strict mode is enabled", ErrBackfillRollbackRefused)
	}
	var rawSnapshot []byte
	if err := tx.QueryRowContext(ctx, `SELECT snapshot FROM spncx_backfill_snapshot
		WHERE product_id=$1 FOR UPDATE`, id).Scan(&rawSnapshot); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: snapshot missing", ErrBackfillRollbackRefused)
		}
		return err
	}
	var snapshot backfillRollbackSnapshot
	if err := json.Unmarshal(rawSnapshot, &snapshot); err != nil {
		return fmt.Errorf("%w: invalid snapshot: %w", ErrBackfillRollbackRefused, err)
	}
	if snapshot.ProductStock == nil || snapshot.VariantStocks == nil || snapshot.Balances == nil {
		return fmt.Errorf("%w: incomplete snapshot", ErrBackfillRollbackRefused)
	}
	var plan BackfillProductPlan
	if err := json.Unmarshal(rawPlan, &plan); err != nil {
		return fmt.Errorf("%w: invalid stored plan: %w", ErrBackfillRollbackRefused, err)
	}
	if plan.ProductID != id {
		return fmt.Errorf("%w: stored plan does not match product %s", ErrBackfillRollbackRefused, id)
	}
	if _, err := readBackfillBalancesTx(ctx, tx, id, true); err != nil {
		return err
	}
	unitIDs, err := lockBackfillMintedUnitsTx(ctx, tx, id)
	if err != nil {
		return err
	}
	printed, err := checkBackfillRollbackUnitsTx(ctx, tx, unitIDs)
	if err != nil {
		return err
	}
	result.PrintedCodesToDestroy = printed
	if len(printed) > 0 && !confirmPrintedDestruction {
		return ErrBackfillRollbackNeedsConfirmation
	}
	var holdingHasUnits bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM inventory_balances b JOIN product_units u ON u.balance_id=b.id
		WHERE b.product_id=$1 AND b.location_key='spncx-excess:'||$1)`, id).Scan(&holdingHasUnits); err != nil {
		return err
	}
	if holdingHasUnits {
		return fmt.Errorf("%w: excess holding lot has units", ErrBackfillRollbackRefused)
	}

	snapshotIDs := make([]string, 0, len(snapshot.Balances))
	for _, b := range snapshot.Balances {
		snapshotIDs = append(snapshotIDs, b.ID)
	}
	newVariantIDs := make([]string, 0, len(plan.NewVariants))
	for _, v := range plan.NewVariants {
		newVariantIDs = append(newVariantIDs, v.VariantID)
	}
	if err := removeBackfillUnitsTx(ctx, tx, id, unitIDs, printed, newVariantIDs, snapshotIDs); err != nil {
		return err
	}
	if err := restoreBackfillBalancesTx(ctx, tx, id, snapshot, snapshotIDs); err != nil {
		return err
	}
	// The restored ledger no longer contains these steps. Free their
	// idempotency keys and release mint movements' created-variant FKs.
	if _, err := tx.ExecContext(ctx, `DELETE FROM inventory_movements
		WHERE product_id=$1 AND reference_type='spncx_backfill' AND reference_id=$1
		AND (starts_with(idempotency_key,'spncx:spncx_backfill:'||$1||':'||variant_id||':')
			OR idempotency_key IN ('spncx-backfill:'||$1||':topup','spncx-backfill:'||$1||':excess')
			OR idempotency_key IN (
				SELECT 'spncx-backfill:'||$1||':absorb:'||lot_id FROM unnest($2::text[]) AS lot_id))`, id, pq.Array(snapshotIDs)); err != nil {
		return err
	}
	if err := restoreBackfillVariantsTx(ctx, tx, id, snapshot.VariantStocks, plan.FlippedVariants, newVariantIDs); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM spncx_backfill_state WHERE product_id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM spncx_backfill_snapshot WHERE product_id=$1`, id); err != nil {
		return err
	}
	var stock int
	if err := tx.QueryRowContext(ctx, `SELECT stock FROM products WHERE id=$1`, id).Scan(&stock); err != nil {
		return err
	}
	if stock != *snapshot.ProductStock {
		return fmt.Errorf("%w: restored product stock %d differs from snapshot %d", ErrBackfillRollbackRefused, stock, *snapshot.ProductStock)
	}
	return nil
}

func lockBackfillMintedUnitsTx(ctx context.Context, tx *sql.Tx, id string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT u.id FROM product_units u
		WHERE u.product_id=$1 AND EXISTS (
			SELECT 1 FROM product_unit_events e WHERE e.unit_id=u.id AND e.event_type='minted'
			AND e.reference_type='spncx_backfill' AND e.reference_id=$1)
		ORDER BY u.code FOR UPDATE OF u`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var unitID string
		if err := rows.Scan(&unitID); err != nil {
			return nil, err
		}
		ids = append(ids, unitID)
	}
	return ids, rows.Err()
}

func checkBackfillRollbackUnitsTx(ctx context.Context, tx *sql.Tx, unitIDs []string) ([]string, error) {
	// Read in a new statement after the row locks are held so an event from
	// a printer that committed while we waited is visible.
	var touched bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM order_item_units WHERE unit_id=ANY($1::uuid[])
		UNION ALL
		SELECT 1 FROM product_unit_events WHERE unit_id=ANY($1::uuid[]) AND event_type NOT IN ('minted','printed'))`,
		pq.Array(unitIDs)).Scan(&touched); err != nil {
		return nil, err
	}
	if touched {
		return nil, fmt.Errorf("%w: a backfill-minted unit has order or movement history", ErrBackfillRollbackRefused)
	}
	printed := []string{}
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(array_agg(u.code ORDER BY u.code), '{}')
		FROM product_units u WHERE u.id=ANY($1::uuid[]) AND EXISTS (
			SELECT 1 FROM product_unit_events e WHERE e.unit_id=u.id AND e.event_type='printed')`,
		pq.Array(unitIDs)).Scan(pq.Array(&printed))
	return printed, err
}

func removeBackfillUnitsTx(ctx context.Context, tx *sql.Tx, id string, unitIDs, printed, newVariantIDs, snapshotIDs []string) error {
	// Preserve other units' claims on a mixed print job and remove destroyed
	// labels from replay. Created variants must also release the print-job FK.
	if _, err := tx.ExecContext(ctx, `UPDATE spncx_print_jobs j
		SET unit_codes=j.unit_codes - $2::text[],
			payload=jsonb_set(j.payload,'{units}',COALESCE((
				SELECT jsonb_agg(label ORDER BY ordinal)
				FROM jsonb_array_elements(j.payload->'units') WITH ORDINALITY AS labels(label,ordinal)
				WHERE NOT (label->>'code'=ANY($2::text[]))), '[]'::jsonb)),
			variant_id=CASE WHEN j.variant_id=ANY($3::uuid[]) THEN NULL ELSE j.variant_id END
		WHERE j.product_id=$1 AND (j.unit_codes ?| $2::text[] OR j.variant_id=ANY($3::uuid[]))`,
		id, pq.Array(printed), pq.Array(newVariantIDs)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM spncx_print_job_units WHERE unit_id=ANY($1::uuid[])`, pq.Array(unitIDs)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM product_unit_events WHERE unit_id=ANY($1::uuid[])`, pq.Array(unitIDs)); err != nil {
		return err
	}
	// Delete the created lots that held the removed units instead of
	// decrementing them: a decrement fires the stock projection while the lot
	// rows still exist, so rolling back a top-up-only product (no snapshot
	// balances, e.g. a variant-less product) would re-project products.stock
	// to 0 mid-rollback and the final stock guard could never pass. Deleting
	// the lot rows leaves the projection untouched once no balance rows remain
	// (the trigger's EXISTS guard), and the snapshot restore below fixes the
	// projection for products that keep snapshot lots. Snapshot lots are
	// excluded here; their quantities are restored by value below.
	//
	// This MUST be two statements, not one WITH ... DELETE: all parts of a
	// single statement see the same snapshot, so a NOT EXISTS re-read of
	// product_units inside the lot DELETE would still see the units the CTE
	// just deleted and no lot would ever be removed (observed: rollback left
	// the created lots at full quantity and the stock guard read 2x).
	rows, err := tx.QueryContext(ctx, `DELETE FROM product_units
		WHERE id=ANY($1::uuid[]) RETURNING balance_id`, pq.Array(unitIDs))
	if err != nil {
		return err
	}
	lotIDs := make([]string, 0, len(unitIDs))
	for rows.Next() {
		var balanceID sql.NullString
		if err := rows.Scan(&balanceID); err != nil {
			rows.Close()
			return err
		}
		if balanceID.Valid && balanceID.String != "" {
			lotIDs = append(lotIDs, balanceID.String)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(lotIDs) == 0 {
		return nil
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM inventory_balances b
		WHERE b.id=ANY($1::text[]) AND b.id<>ALL($2::text[])
		AND NOT EXISTS (SELECT 1 FROM product_units u WHERE u.balance_id=b.id)`,
		pq.Array(lotIDs), pq.Array(snapshotIDs))
	return err
}

func restoreBackfillBalancesTx(ctx context.Context, tx *sql.Tx, id string, snapshot backfillRollbackSnapshot, snapshotIDs []string) error {
	// B5a can reuse a holding lot. An existing snapshot row must be restored.
	if _, err := tx.ExecContext(ctx, `DELETE FROM inventory_balances
		WHERE product_id=$1 AND location_key='spncx-excess:'||$1 AND id<>ALL($2::text[])`, id, pq.Array(snapshotIDs)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM inventory_balances b
		WHERE b.product_id=$1 AND b.variant_id IS NOT NULL AND b.status='sellable'
		AND b.id<>ALL($2::text[]) AND b.quantity=0
		AND NOT EXISTS (SELECT 1 FROM product_units u WHERE u.balance_id=b.id)`,
		id, pq.Array(snapshotIDs)); err != nil {
		return err
	}
	for _, b := range snapshot.Balances {
		res, err := tx.ExecContext(ctx, `UPDATE inventory_balances SET quantity=$3,status=$4,location_key=$5
			WHERE id=$1 AND product_id=$2 AND variant_id IS NOT DISTINCT FROM $6::uuid`,
			b.ID, id, b.Quantity, b.Status, b.LocationKey, b.VariantID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("%w: snapshot balance %s is missing or reassigned", ErrBackfillRollbackRefused, b.ID)
		}
	}
	// A restored lot must still reconcile with its surviving units. For
	// example, a sale of a pre-existing unit can leave product stock equal
	// to the snapshot while restoring its lot would create a phantom unit.
	var uncovered bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM inventory_balances b WHERE b.product_id=$1 AND b.id=ANY($2::text[])
		AND b.unit_tracked AND b.quantity<>(SELECT COUNT(*) FROM product_units u WHERE u.balance_id=b.id))`,
		id, pq.Array(snapshotIDs)).Scan(&uncovered); err != nil {
		return err
	}
	if uncovered {
		return fmt.Errorf("%w: restored balances do not match surviving units", ErrBackfillRollbackRefused)
	}
	return nil
}

func restoreBackfillVariantsTx(ctx context.Context, tx *sql.Tx, id string, stocks map[string]int, flipped, created []string) error {
	// §4.2 #28 allows direct variant-stock writes for snapshot restoration.
	for variant, stock := range stocks {
		res, err := tx.ExecContext(ctx, `UPDATE product_variants SET stock=$3 WHERE id=$1 AND product_id=$2`,
			variant, id, stock)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("%w: snapshot variant %s is missing", ErrBackfillRollbackRefused, variant)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE product_variants SET inventory_mode='legacy'
		WHERE product_id=$1 AND id=ANY($2::uuid[]) AND inventory_mode='tracked'`,
		id, pq.Array(append([]string{}, flipped...))); err != nil {
		return err
	}
	// Surviving variants keep their counters so re-backfill never reuses sequences (I4).
	// A deleted variant's SPNC dies with it. Remove its counter before the variant FK.
	if _, err := tx.ExecContext(ctx, `DELETE FROM product_variant_unit_counters c USING product_variants v
		WHERE v.product_id=$1 AND c.variant_id=v.id AND v.id=ANY($2::uuid[])`, id, pq.Array(created)); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM product_variants WHERE product_id=$1 AND id=ANY($2::uuid[])`, id, pq.Array(created))
	return err
}

func RollbackBackfillAll(ctx context.Context, db *sql.DB, productIDs []string, actor string, confirmPrintedDestruction bool) ([]BackfillRollbackResult, error) {
	ids := append([]string(nil), productIDs...)
	if len(ids) == 0 {
		rows, err := db.QueryContext(ctx, `SELECT product_id FROM spncx_backfill_state WHERE status='completed'`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	results := make([]BackfillRollbackResult, 0, len(ids))
	var failures []error
	for _, id := range ids {
		result, err := RollbackBackfill(ctx, db, id, actor, confirmPrintedDestruction)
		results = append(results, *result)
		if err != nil {
			failures = append(failures, fmt.Errorf("rollback %s: %w", id, err))
		}
	}
	return results, errors.Join(failures...)
}

// The §5.8 queries return the offending rows. The engine reports only counts
// and scopes each query to the product checked in this read-only transaction.
var backfillProductChecks = []struct {
	id, name, query string
	newVariantsOnly bool
}{
	{"V1", "variant_sellable_units", `SELECT v.id, v.spnc, v.stock AS variant_stock,
       COUNT(u.id) FILTER (WHERE u.status = 'in_stock') AS unit_count
FROM product_variants v
LEFT JOIN product_units u ON u.variant_id = v.id
WHERE v.product_id = $1
GROUP BY v.id, v.spnc, v.stock
HAVING v.stock <> COUNT(u.id) FILTER (WHERE u.status = 'in_stock')`, false},
	{"V1b", "non_sellable_units_vs_lots", `SELECT b.id, b.status AS lot_status, b.quantity,
       COUNT(u.id) AS unit_count
FROM inventory_balances b
LEFT JOIN product_units u ON u.balance_id = b.id
WHERE b.unit_tracked = TRUE AND b.status <> 'sellable'
  AND b.product_id = $1
GROUP BY b.id, b.status, b.quantity
HAVING b.quantity <> COUNT(u.id)`, false},
	{"V1c", "unit_status_vs_lot_status", `SELECT u.id, u.code, u.status AS unit_status, b.status AS lot_status, b.location_key
FROM product_units u
JOIN inventory_balances b ON b.id = u.balance_id
WHERE u.product_id = $1
  AND u.status <> CASE
    WHEN b.status = 'sellable' THEN 'in_stock'
    WHEN b.status IN ('sales_reserved','return_requested','inbound_reserved','in_transit') THEN 'reserved'
    WHEN b.status = 'quarantine' AND b.location_key LIKE 'return:%' THEN 'returned'
    WHEN b.status = 'quarantine' THEN 'quarantined'
    WHEN b.status IN ('qc_accepted','unreconciled') THEN 'defined'
    WHEN b.status = 'damaged' THEN 'damaged'
    WHEN b.status = 'sold' THEN 'sold'
    ELSE '###NO-MAPPING###'
  END`, false},
	{"V2", "lot_invariant", `SELECT b.id, b.product_id, b.variant_id, b.quantity,
       COUNT(u.id) AS unit_count
FROM inventory_balances b
LEFT JOIN product_units u ON u.balance_id = b.id
WHERE b.unit_tracked = TRUE
  AND b.product_id = $1
GROUP BY b.id, b.product_id, b.variant_id, b.quantity
HAVING b.quantity <> COUNT(u.id)`, false},
	{"V3", "product_sellable_equality", `WITH ledger AS (
  SELECT b.product_id, COALESCE(SUM(b.quantity), 0) AS ledger_sellable
  FROM inventory_balances b
  LEFT JOIN product_variants pv ON pv.id = b.variant_id
  WHERE b.product_id = $1 AND b.status = 'sellable' AND (pv.id IS NULL OR pv.is_active)
  GROUP BY b.product_id
),
units AS (
  SELECT u.product_id, COUNT(*) AS units_in_stock
  FROM product_units u
  WHERE u.product_id = $1 AND u.status = 'in_stock'
  GROUP BY u.product_id
)
SELECT p.id, p.stock AS product_stock,
       COALESCE(l.ledger_sellable, 0) AS ledger_sellable,
       COALESCE(u.units_in_stock, 0) AS units_in_stock
FROM products p
LEFT JOIN ledger l ON l.product_id = p.id
LEFT JOIN units u ON u.product_id = p.id
WHERE p.id = $1
  AND (p.stock <> COALESCE(l.ledger_sellable, 0)
   OR p.stock <> COALESCE(u.units_in_stock, 0))`, false},
	{"V6", "unallocated_sellable", `SELECT product_id, SUM(quantity) FROM inventory_balances
WHERE variant_id IS NULL AND status='sellable' AND unit_tracked=FALSE
  AND product_id = $1
GROUP BY product_id
HAVING SUM(quantity) <> 0`, false},
	{"V7", "product_stock_unchanged", `SELECT s.product_id, p.stock AS product_stock,
       (s.snapshot->>'product_stock')::int AS snapshot_stock
FROM spncx_backfill_snapshot s
JOIN products p ON p.id = s.product_id
WHERE s.product_id = $1 AND p.stock <> (s.snapshot->>'product_stock')::int`, false},
	{"V8", "doubled_defaults_zero", `SELECT v.id, v.spnc, v.stock AS variant_stock,
       COUNT(u.id) FILTER (WHERE u.status = 'in_stock') AS units_in_stock
FROM spncx_backfill_state s
JOIN product_variants v ON v.product_id = s.product_id AND v.is_default
JOIN LATERAL jsonb_array_elements(s.plan->'alloc') a
  ON (a->>'variant_id')::uuid = v.id
LEFT JOIN product_units u ON u.variant_id = v.id
WHERE s.product_id = $1 AND (a->>'alloc')::int = 0
GROUP BY v.id, v.spnc, v.stock
HAVING v.stock <> 0 OR COUNT(u.id) FILTER (WHERE u.status = 'in_stock') <> 0`, false},
	{"V9", "new_variants", `SELECT v.id, v.product_id, v.stock AS variant_stock,
       (nv->>'stock')::int AS expected,
       (SELECT COUNT(*) FROM product_units u
        WHERE u.variant_id = v.id AND u.status = 'in_stock') AS units_in_stock
FROM spncx_backfill_state s,
     jsonb_array_elements(s.plan->'new_variants') AS nv
JOIN product_variants v ON v.id = (nv->>'variant_id')::uuid
WHERE s.product_id = $1
  AND (v.stock <> (nv->>'stock')::int
       OR (SELECT COUNT(*) FROM product_units u
           WHERE u.variant_id = v.id AND u.status = 'in_stock') <> (nv->>'stock')::int)`, true},
	{"V11", "variants_flipped_tracked", `SELECT v.id, v.spnc, v.inventory_mode
FROM product_variants v
WHERE v.product_id = $1
  AND v.inventory_mode <> 'tracked'`, false},
	{"V12", "variant_less_resolution", `SELECT p.id, p.title, p.schema_version, p.product_type,
       COUNT(v.id) AS variant_count,
       COUNT(v.id) FILTER (WHERE v.inventory_mode = 'tracked' AND v.is_default) AS resolvable_variants
FROM products p
LEFT JOIN product_variants v ON v.product_id = p.id
WHERE p.id = $1
GROUP BY p.id, p.title, p.schema_version, p.product_type
HAVING COUNT(v.id) <> 1
    OR COUNT(v.id) FILTER (WHERE v.inventory_mode = 'tracked' AND v.is_default) <> 1
    OR p.schema_version >= 2 OR p.product_type = 'variable'`, true},
}

type BackfillCheckResult struct {
	ID, Name string
	Passed   bool
	RowCount int
}

type BackfillVerification struct {
	ProductID string
	Checks    []BackfillCheckResult
}

func VerifyBackfillProduct(ctx context.Context, db *sql.DB, productID string) (*BackfillVerification, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var hasNewVariants bool
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(jsonb_array_length(NULLIF(plan->'new_variants','null'::jsonb)),0)>0
		FROM spncx_backfill_state WHERE product_id=$1 AND status='completed'`, productID).Scan(&hasNewVariants); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrBackfillNotCompleted
		}
		return nil, err
	}
	result := &BackfillVerification{ProductID: productID, Checks: make([]BackfillCheckResult, 0, len(backfillProductChecks))}
	for _, check := range backfillProductChecks {
		r := BackfillCheckResult{ID: check.id, Name: check.name}
		if check.newVariantsOnly && !hasNewVariants {
			r.Name = "new_variants (n/a)"
		} else {
			err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM ("+check.query+") AS violations", productID).Scan(&r.RowCount)
			if err != nil {
				return nil, fmt.Errorf("verify backfill %s %s: %w", productID, check.id, err)
			}
		}
		r.Passed = r.RowCount == 0
		result.Checks = append(result.Checks, r)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
