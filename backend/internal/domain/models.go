package domain

import (
	"encoding/json"
	"time"
)

// User represents platform actors
type User struct {
	ID        string    `json:"id" db:"id"`
	Mobile    string    `json:"mobile" db:"mobile"`
	FullName  string    `json:"fullName" db:"full_name"`
	Role      string    `json:"role" db:"role"`
	IsActive  bool      `json:"isActive" db:"is_active"`
	Username  string    `json:"username,omitempty" db:"username"`
	Email     string    `json:"email,omitempty" db:"email"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt time.Time `json:"updatedAt" db:"updated_at"`
}

// UserCapability is an explicit authorization membership for one identity.
// Status is active, pending, or revoked; only active memberships are emitted
// into access tokens.
type UserCapability struct {
	ID         string          `json:"id" db:"id"`
	UserID     string          `json:"userId" db:"user_id"`
	Capability string          `json:"capability" db:"capability"`
	Status     string          `json:"status" db:"status"`
	SourceType string          `json:"sourceType" db:"source_type"`
	SourceID   *string         `json:"sourceId,omitempty" db:"source_id"`
	GrantedBy  *string         `json:"grantedBy,omitempty" db:"granted_by"`
	GrantedAt  time.Time       `json:"grantedAt" db:"granted_at"`
	RevokedAt  *time.Time      `json:"revokedAt,omitempty" db:"revoked_at"`
	Metadata   json.RawMessage `json:"metadata" db:"metadata"`
	CreatedAt  time.Time       `json:"createdAt" db:"created_at"`
	UpdatedAt  time.Time       `json:"updatedAt" db:"updated_at"`
}

// Supplier represents verified vendors offering wholesale catalog items
type Supplier struct {
	ID                     string  `json:"id" db:"id"`
	OwnerUserID            *string `json:"ownerUserId,omitempty" db:"owner_user_id"`
	Name                   string  `json:"name" db:"name"`
	LogoURL                string  `json:"logoUrl" db:"logo_url"`
	Rating                 float64 `json:"rating" db:"rating"`
	TotalProducts          int     `json:"totalProducts" db:"total_products"`
	DeliveryScore          int     `json:"deliveryScore" db:"delivery_score"`
	WarehouseCity          string  `json:"warehouseCity" db:"warehouse_city"`
	Description            string  `json:"description" db:"description"`
	Badge                  string  `json:"badge" db:"badge"`
	SupportsDirectShipping bool    `json:"supportsDirectShipping" db:"supports_direct_shipping"`
	// D53 (live parity): the four fields supplier_repo.go already scans.
	// Exact live types/tags are unverified (MASTER-AUDIT U80); these are
	// the probable types suggested by the audit (and by
	// SupplierMarkupSummary for the nullable markup pair), not claimed
	// live-verified types.
	ApprovalStatus        string   `json:"approvalStatus" db:"approval_status"`
	SignedContractVersion int      `json:"signedContractVersion" db:"signed_contract_version"`
	DefaultMarkupType     *string  `json:"defaultMarkupType,omitempty" db:"default_markup_type"`
	DefaultMarkupValue    *float64 `json:"defaultMarkupValue,omitempty" db:"default_markup_value"`
	// D55: extra live columns, added only as far as APIs need them.
	ApprovedBy             *string    `json:"approvedBy,omitempty" db:"approved_by"`
	ApprovedAt             *time.Time `json:"approvedAt,omitempty" db:"approved_at"`
	RejectionReason        *string    `json:"rejectionReason,omitempty" db:"rejection_reason"`
	TapinShopID            *string    `json:"tapinShopId,omitempty" db:"tapin_shop_id"`
	WarehouseCityConfirmed bool       `json:"warehouseCityConfirmed" db:"warehouse_city_confirmed"`
	WarehouseCitySource    *string    `json:"warehouseCitySource,omitempty" db:"warehouse_city_source"`
	// TapinToken is internal use ONLY. json:"-" means it is never
	// serialized in any API response; no handler in this tree selects
	// or returns it. Tapin crypto details are unverified (U63).
	TapinToken string `json:"-" db:"tapin_token"`
	// Gap 12: suspension bookkeeping (migration 202611050007). Served
	// only on the admin records surface; nil unless the supplier is or
	// was suspended (reactivation clears all three).
	SuspendedAt      *time.Time `json:"suspendedAt,omitempty" db:"suspended_at"`
	SuspendedBy      *string    `json:"suspendedBy,omitempty" db:"suspended_by"`
	SuspensionReason *string    `json:"suspensionReason,omitempty" db:"suspension_reason"`
	CreatedAt        time.Time  `json:"createdAt" db:"created_at"`
}

type SupplierMember struct {
	ID          string   `json:"id"`
	SupplierID  string   `json:"supplierId"`
	UserID      string   `json:"userId"`
	FullName    string   `json:"fullName"`
	Username    string   `json:"username"`
	Mobile      string   `json:"mobile"`
	RoleKey     string   `json:"roleKey"`
	Permissions []string `json:"permissions"`
	IsActive    bool     `json:"isActive"`
}

// Category represents high-level taxonomy
type Category struct {
	ID       string  `json:"id" db:"id"`
	Title    string  `json:"title" db:"title"`
	Slug     string  `json:"slug" db:"slug"`
	IconName string  `json:"iconName" db:"icon_name"`
	Gradient string  `json:"gradient" db:"gradient"`
	ImageURL string  `json:"imageUrl" db:"image_url"`
	ParentID *string `json:"parentId" db:"parent_id"`
	// FIX 4 (team 04 G8): active-product count in this subtree; not a
	// DB column, filled by GetCategoryProductCounts.
	ProductCount int `json:"productCount" db:"-"`
}

// Niche represents a specialized target market segment. The DB is
// the source of truth (14 niches in live, D44); no hardcoded list
// is kept here. The niche-auto -> digital categorySlug mapping
// suspicion is left as-is (TODO, see Team 04 report).
type Niche struct {
	ID           string `json:"id" db:"id"`
	Title        string `json:"title" db:"title"`
	Slug         string `json:"slug" db:"slug"`
	CategorySlug string `json:"categorySlug" db:"category_slug"`
	Badge        string `json:"badge" db:"badge"`
	Description  string `json:"description" db:"description"`
}

// Store represents dropshipper storefront or central marketplace
type Store struct {
	Tagline          string `json:"tagline"`
	Description      string `json:"description"`
	ShortDescription string `json:"shortDescription"`
	// G2 (Batch 4b): the complementary-section title. Storage home is
	// the dedicated stores.addon_section_title column (read in
	// loadPanelConfig's SELECT, written by SaveStoreSettings) — NOT
	// panel_config: the PDP reads the column (GetPDPAddons), so a
	// panel_config-only value would never render. The panel_config
	// merge in SaveStoreSettings still carries a copy; the column wins.
	AddonSectionTitle         string          `json:"addonSectionTitle,omitempty" db:"addon_section_title"`
	ActiveProductIDs          []string        `json:"activeProductIds"`
	SelectedNicheIDs          []string        `json:"selectedNicheIds"`
	ActiveCategories          []string        `json:"activeCategories"`
	BuilderConfig             json.RawMessage `json:"builderConfig,omitempty"`
	CentralDealsFilter        string          `json:"centralDealsFilter,omitempty"`
	CentralDealsProductIDs    []string        `json:"centralDealsProductIds,omitempty"`
	Status                    string          `json:"status"`
	PanelConfig               json.RawMessage `json:"-"`
	SellerDealsWidgetConfig   json.RawMessage `json:"sellerDealsWidgetConfig,omitempty"`
	ID                        string          `json:"id" db:"id"`
	Name                      string          `json:"name" db:"name"`
	Slug                      string          `json:"slug" db:"slug"`
	OwnerType                 string          `json:"ownerType" db:"owner_type"` // 'central' | 'dropshipper'
	OwnerName                 string          `json:"ownerName" db:"owner_name"`
	OwnerUserID               *string         `json:"-" db:"owner_user_id"`
	ProfitMarginPercent       float64         `json:"profitMarginPercent" db:"profit_margin_percent"`
	CustomStoreMarginOverride *float64        `json:"customStoreMarginOverride" db:"custom_store_margin_override"`
	IsMarginLockedByAdmin     bool            `json:"isMarginLockedByAdmin" db:"is_margin_locked_by_admin"`
	ActivePlanID              string          `json:"activePlanId" db:"active_plan_id"`
	ReferralCode              string          `json:"referralCode" db:"referral_code"`
	ReferrerSellerID          *string         `json:"referrerSellerId" db:"referrer_seller_id"`
	SignedContractVersion     int             `json:"signedContractVersion" db:"signed_contract_version"`
	CustomDomain              string          `json:"customDomain" db:"custom_domain"`
	TotalSales                int64           `json:"totalSales" db:"total_sales"`
	// FIX3 (public storefront only): the store's public template and logo.
	// These are presentation fields, read by team05PublicStoreView only;
	// json:"-" keeps every other domain.Store encoding unchanged.
	TemplateID                string             `json:"-" db:"template_id"`
	TemplateConfig            json.RawMessage    `json:"-" db:"template_config"`
	Logo                      string             `json:"-" db:"logo"`
	ProductPriceOverrides     map[string]float64 `json:"productPriceOverrides"`
	ProductPriceOverrideModes map[string]string  `json:"productPriceOverrideModes"` // 'percent' | 'fixed'
	NichePriceOverrides       map[string]float64 `json:"nichePriceOverrides"`
	NichePriceOverrideModes   map[string]string  `json:"nichePriceOverrideModes"` // 'percent' | 'fixed'
}

// SellerDiscountCampaign is a seller-managed discount campaign
// (MASTER-AUDIT D48, backend management only). It mirrors the
// seller_discount_campaigns table (migration 202610230004).
// MANAGEMENT ONLY: campaigns are stored and listed, never
// consulted when pricing an order (price application is
// unproven in live, U04).
type SellerDiscountCampaign struct {
	ID        string    `json:"id" db:"id"`
	StoreID   string    `json:"storeId" db:"store_id"`
	Name      string    `json:"name" db:"name"`
	Scope     string    `json:"scope" db:"scope"` // product | niche | store
	TargetID  string    `json:"targetId" db:"target_id"`
	Mode      string    `json:"mode" db:"mode"` // percent | fixed
	Value     float64   `json:"value" db:"value"`
	StartsAt  time.Time `json:"startsAt" db:"starts_at"`
	EndsAt    time.Time `json:"endsAt" db:"ends_at"`
	IsActive  bool      `json:"isActive" db:"is_active"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt time.Time `json:"updatedAt" db:"updated_at"`
}

// SellerDiscountCampaignPatch carries optional PATCH fields; a
// nil pointer means "leave unchanged".
type SellerDiscountCampaignPatch struct {
	Name     *string    `json:"name"`
	Scope    *string    `json:"scope"`
	TargetID *string    `json:"targetId"`
	Mode     *string    `json:"mode"`
	Value    *float64   `json:"value"`
	StartsAt *time.Time `json:"startsAt"`
	EndsAt   *time.Time `json:"endsAt"`
	IsActive *bool      `json:"isActive"`
}

// StoreSubscriber is one row of the seller subscribers tab
// (MASTER-AUDIT D47), read from newsletter_subscribers. The
// unsubscribe_token column is deliberately absent: it is the
// only credential the public unsubscribe endpoint needs.
type StoreSubscriber struct {
	ID             string     `json:"id" db:"id"`
	StoreID        string     `json:"storeId" db:"store_id"`
	Mobile         string     `json:"mobile" db:"mobile"`
	Email          string     `json:"email" db:"email"`
	Status         string     `json:"status" db:"status"`
	Source         string     `json:"source" db:"source"`
	CreatedAt      time.Time  `json:"createdAt" db:"created_at"`
	UnsubscribedAt *time.Time `json:"unsubscribedAt" db:"unsubscribed_at"`
}

// Product represents catalog inventory item
type Product struct {
	ProductType               string                 `json:"productType" db:"product_type"`
	SchemaVersion             int                    `json:"schemaVersion" db:"schema_version"`
	ProductSchema             *ProductSchema         `json:"productSchema,omitempty" db:"attribute_schema"`
	ID                        string                 `json:"id" db:"id"`
	Title                     string                 `json:"title" db:"title"`
	Slug                      string                 `json:"slug" db:"slug"`
	CategorySlug              string                 `json:"categorySlug" db:"category_slug"`
	SupplierID                string                 `json:"supplierId" db:"supplier_id"`
	OriginalPrice             int64                  `json:"originalPrice" db:"original_price"`
	SupplierBaseCost          int64                  `json:"supplierBaseCost" db:"supplier_base_cost"` // C
	PlatformMargin            int64                  `json:"platformMargin" db:"platform_margin"`      // Default F
	IsOriginal                bool                   `json:"isOriginal" db:"is_original"`
	Stock                     int                    `json:"stock" db:"stock"`
	ImageURL                  string                 `json:"imageUrl" db:"image_url"`
	Images                    []string               `json:"images,omitempty"`
	Specs                     map[string]interface{} `json:"specs" db:"specs"`
	Description               string                 `json:"description" db:"description"`
	WarehouseName             string                 `json:"warehouseName" db:"warehouse_name"`
	PreparationDays           int                    `json:"preparationDays" db:"preparation_days"`
	SupplierWarranty          string                 `json:"supplierWarranty" db:"supplier_warranty"`
	IsFeatured                bool                   `json:"isFeatured" db:"is_featured"`
	IsSpecialDeal             bool                   `json:"isSpecialDeal" db:"is_special_deal"`
	FulfillmentMethod         string                 `json:"fulfillmentMethod" db:"fulfillment_method"` // "warehouse" | "supplier_direct"
	SupplyOption              string                 `json:"supplyOption" db:"supply_option"`           // "3hour_local" | "1day" | "3day"
	OriginSupplierWarehouseId *string                `json:"originSupplierWarehouseId,omitempty" db:"origin_supplier_warehouse_id"`
	WarrantyPropertyID        string                 `json:"warrantyPropertyId,omitempty" db:"warranty_property_id"`
	LengthMM                  *int                   `json:"lengthMm,omitempty" db:"length_mm"`
	WidthMM                   *int                   `json:"widthMm,omitempty" db:"width_mm"`
	HeightMM                  *int                   `json:"heightMm,omitempty" db:"height_mm"`
	WeightGrams               *int                   `json:"weightGrams,omitempty" db:"weight_grams"`
	// Shipping/tax class (P2(b)): the fallback target for variants
	// (variant class ?? this ?? 'standard'). Stored vocabulary only
	// in v1; consumed nowhere yet. DB default 'standard'.
	ShippingClass     string `json:"shippingClass" db:"shipping_class"`
	TaxClass          string `json:"taxClass" db:"tax_class"`
	MeasurementStatus string `json:"measurementStatus" db:"measurement_status"`
	// Brand, Rating, ReviewsCount, CreatedAt: columns confirmed by
	// migration 202610200003 (brand TEXT, rating NUMERIC(3,2),
	// reviews_count INT) and by the GetSupplierProducts SELECT in
	// supplier_repo.go that scans them; created_at is in the initial
	// products schema. JSON names follow this struct's camelCase convention.
	Brand        string    `json:"brand" db:"brand"`
	IsActive     bool      `json:"isActive" db:"is_active"`
	SpnNumber    *int64    `json:"spnNumber,omitempty" db:"spn_number"`
	Rating       float64   `json:"rating" db:"rating"`
	ReviewsCount int       `json:"reviewsCount" db:"reviews_count"`
	CreatedAt    time.Time `json:"createdAt" db:"created_at"`
	NicheIDs     []string  `json:"nicheIds"`
}

// PlatformSettings represents runtime governance
type PlatformSettings struct {
	BaseFeeF                      int64            `json:"baseFeeF"`
	SupplierFeeOverrides          map[string]int64 `json:"supplierFeeOverrides"`
	ProductFeeOverrides           map[string]int64 `json:"productFeeOverrides"`
	MinProfitMargin               float64          `json:"minProfitMargin"`
	MaxProfitMargin               float64          `json:"maxProfitMargin"`
	DefaultMargin                 float64          `json:"defaultMargin"`
	AttributionModel              string           `json:"attributionModel"`
	CookieDays                    int              `json:"cookieDays"`
	DeliveryHoldPeriodDays        int              `json:"deliveryHoldPeriodDays"`
	DeliveryFollowupTriggerDays   int              `json:"deliveryFollowupTriggerDays"`
	ReferralCommissionPercent     float64          `json:"referralCommissionPercent"`
	ReferralIsEnabled             bool             `json:"referralIsEnabled"`
	ReferralSubjectToDeliveryLock bool             `json:"referralSubjectToDeliveryLock"`
	SellerContractVersion         int              `json:"sellerContractVersion"`
	SupplierContractVersion       int              `json:"supplierContractVersion"`
	SupplierReturnPostalCharge    *int64           `json:"supplierReturnPostalCharge,omitempty"`
}

// OrderItem represents a line item in an order with snapshot financials
type OrderItemOriginSplit struct {
	WarehouseID         *string
	SupplierWarehouseID *string
	Label               string
	Quantity            int
	LegIndex            int
}

type OrderItem struct {
	OriginSplits         []OrderItemOriginSplit `json:"-"`
	PricingPolicyVersion string                 `json:"pricingPolicyVersion,omitempty" db:"pricing_policy_version"`
	ID                   string                 `json:"id" db:"id"`
	OrderID              string                 `json:"orderId" db:"order_id"`
	ProductID            string                 `json:"productId" db:"product_id"`
	ProductTitle         string                 `json:"productTitle,omitempty"`
	Quantity             int                    `json:"quantity" db:"quantity"`
	WholesaleCost        int64                  `json:"wholesaleCost" db:"wholesale_cost"`             // C
	PlatformFee          int64                  `json:"platformFee" db:"platform_fee"`                 // F
	SellerMargin         int64                  `json:"sellerMargin" db:"seller_margin"`               // D
	FinalUnitPrice       int64                  `json:"finalUnitPrice" db:"final_unit_price"`          // P = C + F + D
	DiscountAmount       int64                  `json:"discountAmount,omitempty" db:"discount_amount"` // seller discount on this line (margin only)
	// D38 variant snapshot (migration 202610200005). Pointers so a NULL
	// column scans as absent rather than as an empty variant identity.
	VariantID   *string `json:"variantId,omitempty" db:"variant_id"`
	VariantName *string `json:"variantName,omitempty" db:"variant_name"`
	VariantSPNC *string `json:"variantSpnc,omitempty" db:"variant_spnc"`
	// Gap 13: resolved-routing snapshot consumed only by
	// CreateOrderWithShipments, which persists one
	// order_item_origins row per line from these fields inside
	// the order transaction. Internal transport only: Routing is
	// routing.ResolveItemRouting's verdict for this line;
	// OriginLabel mirrors shippingQuoteLine legOrigin's label;
	// WarehouseID / SupplierWarehouseID are the origin's
	// warehouse identities (nil = none recorded); LegIndex is
	// the first-appearance index of the line's origin key
	// (supplier:<id> / warehouse), same grouping as
	// computeShippingQuote.
	Routing             string  `json:"-"`
	OriginLabel         string  `json:"-"`
	WarehouseID         *string `json:"-"`
	SupplierWarehouseID *string `json:"-"`
	SupplierID          string  `json:"-"`
	LegIndex            int     `json:"-"`
}

// Order represents customer purchase contract
type OrderRefundSummary struct {
	Status         string `json:"status"`
	Currency       string `json:"currency"`
	AmountToman    int64  `json:"amountToman"`
	PendingToman   int64  `json:"pendingToman"`
	SucceededToman int64  `json:"succeededToman"`
	FailedToman    int64  `json:"failedToman"`
	UnknownToman   int64  `json:"unknownToman"`
	Destination    string `json:"destination"`
	NeedsReview    bool   `json:"needsReview"`
}

type Order struct {
	CouponID                   string              `json:"-"`
	CouponRevision             int64               `json:"-"`
	CouponDiscountAmount       *int64              `json:"couponDiscountAmount,omitempty" db:"coupon_discount_amount"`
	Refund                     *OrderRefundSummary `json:"refund,omitempty"`
	ID                         string              `json:"id" db:"id"`
	CustomerUserID             *string             `json:"customerUserId,omitempty" db:"customer_user_id"`
	OrderNumber                string              `json:"orderNumber" db:"order_number"`
	StoreID                    string              `json:"storeId" db:"store_id"`
	StoreName                  string              `json:"storeName" db:"store_name"`
	TotalAmount                int64               `json:"totalAmount" db:"total_amount"`
	DiscountAmount             int64               `json:"discountAmount" db:"discount_amount"`
	SellerDiscountAmount       int64               `json:"sellerDiscountAmount,omitempty" db:"seller_discount_amount"`
	DiscountCode               string              `json:"discountCode,omitempty" db:"discount_code"`
	PayableAmount              int64               `json:"payableAmount" db:"payable_amount"`
	PaymentMethod              string              `json:"paymentMethod" db:"payment_method"`
	PaymentStatus              string              `json:"paymentStatus" db:"payment_status"`
	CustomerFullName           string              `json:"customerFullName" db:"customer_full_name"`
	CustomerPhone              string              `json:"customerPhone" db:"customer_phone"`
	CustomerProvince           string              `json:"customerProvince" db:"customer_province"`
	CustomerCity               string              `json:"customerCity" db:"customer_city"`
	CustomerAddress            string              `json:"customerAddress" db:"customer_address"`
	CustomerPostalCode         string              `json:"customerPostalCode" db:"customer_postal_code"`
	Status                     string              `json:"status" db:"status"`
	TrackingCode               string              `json:"trackingCode" db:"tracking_code"`
	ShippingCost               int64               `json:"shippingCost" db:"shipping_cost"`
	ShippingQuoteID            string              `json:"shippingQuoteId,omitempty" db:"shipping_quote_id"`
	DropshipperProfit          int64               `json:"dropshipperProfit" db:"dropshipper_profit"`
	PlatformProfit             int64               `json:"platformProfit" db:"platform_profit"`
	ReferralCommission         int64               `json:"referralCommission" db:"referral_commission"`
	ReferralCommissionReleased bool                `json:"referralCommissionReleased" db:"referral_commission_released"`
	ReferrerSellerID           *string             `json:"referrerSellerId" db:"referrer_seller_id"`
	ProfitReleased             bool                `json:"profitReleased" db:"profit_released"`
	ProfitReleasedAt           *time.Time          `json:"profitReleasedAt" db:"profit_released_at"`
	ShippedAt                  *time.Time          `json:"shippedAt" db:"shipped_at"`
	ConfirmedAt                *time.Time          `json:"confirmedAt" db:"confirmed_at"`
	DisputedAt                 *time.Time          `json:"disputedAt" db:"disputed_at"`
	FulfillmentMethod          string              `json:"fulfillmentMethod" db:"fulfillment_method"` // "warehouse" | "supplier_direct" | "mixed"
	CreatedAt                  time.Time           `json:"createdAt" db:"created_at"`
	UpdatedAt                  time.Time           `json:"updatedAt" db:"updated_at"`
	Items                      []OrderItem         `json:"items"`
	// D38 snapshot columns (migrations 202610200002, 202610230005,
	// 202610250009, 202610280008, 202610310016, 202610190003 and the
	// carrier/packing columns). All already exist in the live schema;
	// this struct only stops the Go layer ignoring them. Pointers so
	// NULL columns scan as absent. wallet_amount + gateway_amount <=
	// payable_amount is a database CHECK, not re-implemented here.
	// NOTE: there is deliberately NO seller discount field here:
	// orders.discount_amount remains OriginalPrice minus final price,
	// and seller_discount_amount / discount_code exist only on the
	// un-deployed dropshipper-discounts branch, not in live.
	WalletAmount        int64      `json:"walletAmount" db:"wallet_amount"`
	GatewayAmount       int64      `json:"gatewayAmount" db:"gateway_amount"`
	PlatformAddonAmount int64      `json:"platformAddonAmount" db:"platform_addon_amount"`
	SupplierAddonAmount int64      `json:"supplierAddonAmount" db:"supplier_addon_amount"`
	InventoryCaptured   bool       `json:"inventoryCaptured" db:"inventory_captured"`
	PackagingLengthMM   *int       `json:"packagingLengthMm,omitempty" db:"packaging_length_mm"`
	PackagingWidthMM    *int       `json:"packagingWidthMm,omitempty" db:"packaging_width_mm"`
	PackagingHeightMM   *int       `json:"packagingHeightMm,omitempty" db:"packaging_height_mm"`
	DisputeReason       *string    `json:"disputeReason,omitempty" db:"dispute_reason"`
	Carrier             *string    `json:"carrier,omitempty" db:"carrier"`
	TrackingNumber      *string    `json:"trackingNumber,omitempty" db:"tracking_number"`
	ShippingLabel       *string    `json:"shippingLabel,omitempty" db:"shipping_label"`
	PackedAt            *time.Time `json:"packedAt,omitempty" db:"packed_at"`
	BoxSize             *string    `json:"boxSize,omitempty" db:"box_size"`
	WeightGrams         *int       `json:"weightGrams,omitempty" db:"weight_grams"`
	IdempotencyKey      *string    `json:"idempotencyKey,omitempty" db:"idempotency_key"`
	ReturnWindowEndsAt  *time.Time `json:"returnWindowEndsAt,omitempty" db:"return_window_ends_at"`
}

// ShippingAddress represents destination location for direct fulfillment
type ShippingAddress struct {
	Province   string `json:"province"`
	City       string `json:"city"`
	PostalCode string `json:"postalCode"`
	Address    string `json:"address"`
}

// SupplierShipment represents vendor-direct fulfillment consignment
type SupplierShipment struct {
	ID              string          `json:"id" db:"id"`
	OrderID         string          `json:"orderId" db:"order_id"`
	OrderNumber     string          `json:"orderNumber,omitempty"`
	SupplierID      string          `json:"supplierId" db:"supplier_id"`
	ShippingAddress ShippingAddress `json:"shippingAddress" db:"shipping_address"`
	CustomerPhone   string          `json:"customerPhone" db:"customer_phone"` // Contact phone for delivery courier
	CustomerName    string          `json:"customerName" db:"customer_name"`   // Recipient name
	Carrier         *string         `json:"carrier,omitempty" db:"carrier"`
	TrackingNumber  *string         `json:"trackingNumber,omitempty" db:"tracking_number"`
	ShippedAt       *time.Time      `json:"shippedAt,omitempty" db:"shipped_at"`
	DeliveredAt     *time.Time      `json:"deliveredAt,omitempty" db:"delivered_at"`
	Status          string          `json:"status" db:"status"` // pending, shipped, in_transit, delivered, failed
	SupplierNotes   *string         `json:"supplierNotes,omitempty" db:"supplier_notes"`
	CreatedAt       time.Time       `json:"createdAt" db:"created_at"`
	UpdatedAt       time.Time       `json:"updatedAt" db:"updated_at"`
}

// SupplierShipmentAuditLog tracks privacy access and status changes
type SupplierShipmentAuditLog struct {
	ID          string    `json:"id" db:"id"`
	ShipmentID  string    `json:"shipmentId" db:"shipment_id"`
	ActorUserID string    `json:"actorUserId" db:"actor_user_id"`
	Action      string    `json:"action" db:"action"`
	Timestamp   time.Time `json:"timestamp" db:"timestamp"`
}

// Dispute represents a return claim or undelivered shipment dispute
// DisputeOutcome separates immutable adjudication from evolving order-level money progress.
type DisputeOutcome struct {
	DisputeID          string             `json:"disputeId"`
	OrderID            string             `json:"orderId"`
	Action             string             `json:"action"`
	ResponsibleParty   string             `json:"responsibleParty"`
	ClaimAmount        int64              `json:"claimAmount"`
	ActorUserID        string             `json:"actorUserId,omitempty"`
	DecisionTime       *time.Time         `json:"decisionTime,omitempty"`
	Replay             bool               `json:"replay"`
	OrderStatus        string             `json:"orderStatus"`
	SellerReleaseState string             `json:"sellerReleaseState"`
	HoldReasons        []string           `json:"holdReasons"`
	RefundScope        string             `json:"refundScope"`
	Refund             OrderRefundSummary `json:"refund"`
}

type Dispute struct {
	Outcome          *DisputeOutcome `json:"outcome,omitempty"`
	ID               string          `json:"id" db:"id"`
	OrderID          string          `json:"orderId" db:"order_id"`
	OrderNumber      string          `json:"orderNumber" db:"order_number"`
	Type             string          `json:"type" db:"type"` // 'return_7day' | 'delivery_undelivered'
	CustomerName     string          `json:"customerName" db:"customer_name"`
	ProductTitle     string          `json:"productTitle" db:"product_title"`
	ClaimAmount      int64           `json:"claimAmount" db:"claim_amount"`
	Reason           string          `json:"reason" db:"reason"`
	StoreID          string          `json:"storeId" db:"store_id"`
	SupplierName     string          `json:"supplierName" db:"supplier_name"`
	Status           string          `json:"status" db:"status"` // 'pending' | 'resolved'
	ResponsibleParty string          `json:"responsibleParty" db:"responsible_party"`
	ResolutionAction string          `json:"resolutionAction" db:"resolution_action"`
	ResolutionDate   *time.Time      `json:"resolutionDate" db:"resolution_date"`
	CreatedAt        time.Time       `json:"createdAt" db:"created_at"`
}

// LedgerEntry represents immutable double-entry bookkeeping journal entry
type LedgerEntry struct {
	ID                    string    `json:"id" db:"id"`
	Date                  time.Time `json:"date" db:"date"`
	TargetAccount         string    `json:"targetAccount" db:"target_account"`
	TargetType            string    `json:"targetType" db:"target_type"` // 'seller' | 'supplier' | 'carrier' | 'platform'
	Type                  string    `json:"type" db:"type"`              // 'debit' | 'credit'
	Amount                int64     `json:"amount" db:"amount"`
	Reason                string    `json:"reason" db:"reason"`
	Operator              string    `json:"operator" db:"operator"`
	RelatedOrderOrDispute string    `json:"relatedOrderOrDispute" db:"related_order_or_dispute"`
	CreatedAt             time.Time `json:"createdAt" db:"created_at"`
}

// ---------------------------------------------------------------------------
// Team 04 live parity additions to domain models (D21/D28/D39/D40/D43/
// D46/D61). Appended by the Team 04 patch.
// ---------------------------------------------------------------------------

// ProductVariant is one row of product_variants as used by the public
// catalog (D28). SupplierBaseCost is the variant's own cost input to
// pricing (NULL = inherit the product cost); it is never serialized
// into the public variant payload by the catalog handlers.
type ProductVariant struct {
	Selection        map[string]string      `json:"selection"`
	InventoryMode    string                 `json:"inventoryMode"`
	IsActive         bool                   `json:"isActive"`
	CombinationKey   string                 `json:"combinationKey,omitempty"`
	MinCartQty       int                    `json:"minCartQty"`
	MaxCartQty       *int                   `json:"maxCartQty"`
	ID               string                 `json:"id"`
	ProductID        string                 `json:"productId"`
	Spnc             string                 `json:"spnc"`
	SpncNumber       *int                   `json:"spncNumber,omitempty"`
	Name             string                 `json:"name"`
	Attributes       map[string]interface{} `json:"attributes"`
	IsDefault        bool                   `json:"isDefault"`
	Stock            int                    `json:"stock"`
	SupplierBaseCost *int64                 `json:"supplierBaseCost,omitempty"`
	// Images are the variant's own photos, ordered; images[0] is the hero.
	// Empty/nil = no variant image; PDP falls back to the product gallery.
	// (P0 cap is 4, enforced in the repository, not in this struct.)
	Images []string `json:"images,omitempty"`
	// Physical attributes. Nil = inherit from the product.
	// Present values must be > 0; dims are all-or-none.
	WeightGrams *int `json:"weightGrams,omitempty"`
	LengthMM    *int `json:"lengthMm,omitempty"`
	WidthMM     *int `json:"widthMm,omitempty"`
	HeightMM    *int `json:"heightMm,omitempty"`
	// Sale price (P1(c)): an absolute final customer price in Toman
	// that REPLACES the resolved final price inside [SaleFrom, SaleTo)
	// (lowest wins vs campaigns, floor R). Nil = no sale. Never exposed
	// alongside cost: the sale price itself is customer-facing data.
	SalePrice *int64     `json:"salePrice,omitempty" db:"sale_price"`
	SaleFrom  *time.Time `json:"saleFrom,omitempty" db:"sale_from"`
	SaleTo    *time.Time `json:"saleTo,omitempty" db:"sale_to"`
	// Shipping/tax class (P2(b)): stored vocabulary only in v1, nil =
	// inherit the product-level class. Consumed nowhere yet.
	ShippingClass *string `json:"shippingClass,omitempty" db:"shipping_class"`
	TaxClass      *string `json:"taxClass,omitempty" db:"tax_class"`
}

// Variant shipping/tax class vocabulary (P2(b), mirrors the DB CHECKs
// in migration 202611130004). 'standard' is the effective default.
const (
	ShippingClassStandard  = "standard"
	ShippingClassFragile   = "fragile"
	ShippingClassOversized = "oversized"
	ShippingClassHeavy     = "heavy"
	ShippingClassColdChain = "cold_chain"
	TaxClassStandard       = "standard"
	TaxClassReduced        = "reduced"
	TaxClassZero           = "zero"
	TaxClassExempt         = "exempt"
)

// ValidShippingClass reports whether s is a known shipping class.
func ValidShippingClass(s string) bool {
	switch s {
	case ShippingClassStandard, ShippingClassFragile, ShippingClassOversized, ShippingClassHeavy, ShippingClassColdChain:
		return true
	}
	return false
}

// ValidTaxClass reports whether s is a known tax class.
func ValidTaxClass(s string) bool {
	switch s {
	case TaxClassStandard, TaxClassReduced, TaxClassZero, TaxClassExempt:
		return true
	}
	return false
}

// EffectiveShippingClass resolves variant ?? product ?? 'standard'.
func EffectiveShippingClass(variant, product string) string {
	if variant != "" {
		return variant
	}
	if product != "" {
		return product
	}
	return ShippingClassStandard
}

// EffectiveTaxClass resolves variant ?? product ?? 'standard'.
func EffectiveTaxClass(variant, product string) string {
	if variant != "" {
		return variant
	}
	if product != "" {
		return product
	}
	return TaxClassStandard
}

// CategoryBrand is one Digikala brand facet row (D40, new API shape).
type CategoryBrand struct {
	ID      int    `json:"id"`
	Code    string `json:"code"`
	TitleFa string `json:"titleFa"`
	TitleEn string `json:"titleEn"`
}

// DkAttributeValue is one Digikala attribute value (D40).
type DkAttributeValue struct {
	ID      int    `json:"id"`
	TitleFa string `json:"titleFa"`
	TitleEn string `json:"titleEn"`
}

// DkAttribute is one Digikala attribute with its per-category values
// (D40). Values are the category's OWN value set only; no inherited
// union is computed (see GetCategoryAttributesBySlug).
type DkAttribute struct {
	ID     int                `json:"id"`
	Title  string             `json:"title"`
	Type   string             `json:"type"`
	Values []DkAttributeValue `json:"values"`
}

// DossierDimensions carries product/package dimensions for the D61
// review dossier, sourced from the products dimension columns.
type DossierDimensions struct {
	LengthMm    *int `json:"lengthMm,omitempty"`
	WidthMm     *int `json:"widthMm,omitempty"`
	HeightMm    *int `json:"heightMm,omitempty"`
	WeightGrams *int `json:"weightGrams,omitempty"`
}

// CatalogReviewDossier is the backend data for the Team 08
// CatalogReviewDossier component (D61). ProductID is empty for a
// pre-approval dossier, where ProposalID carries the proposal instead.
type CatalogReviewDossier struct {
	ProductID         string                 `json:"productId"`
	ProposalID        *string                `json:"proposalId,omitempty"`
	Title             string                 `json:"title"`
	Slug              string                 `json:"slug"`
	Brand             string                 `json:"brand"`
	Category          string                 `json:"category"`
	Status            string                 `json:"status"`
	PackageDimensions DossierDimensions      `json:"packageDimensions"`
	ProductHeightMm   *int                   `json:"productHeightMm,omitempty"`
	WeightGrams       *int                   `json:"weightGrams,omitempty"`
	Warranty          string                 `json:"warranty"`
	Fulfillment       string                 `json:"fulfillmentMethod"`
	ShippingMode      string                 `json:"shippingMode"`
	Specs             map[string]interface{} `json:"specs"`
	Images            []string               `json:"images"`
	VariantCount      int                    `json:"variantCount"`
	RejectionReason   *string                `json:"rejectionReason,omitempty"`
	ApprovalNote      string                 `json:"approvalNote"`
}

// SearchDailyAggregate is one row of the daily search rollup
// aggregation (D43). It is an aggregation result only: persisting it
// is TODO U73 (see RollupSearchQueries).
type SearchDailyAggregate struct {
	TermNormalized     string    `json:"termNormalized"`
	StoreSlug          string    `json:"storeSlug"`
	Day                time.Time `json:"day"`
	Searches           int       `json:"searches"`
	ZeroResultSearches int       `json:"zeroResultSearches"`
}

// ProductUnit identifies a physical unit independently of its lot.
type ProductUnit struct {
	ID            string     `json:"id"`
	Code          string     `json:"code"`
	VariantID     string     `json:"variant_id"`
	ProductID     string     `json:"product_id"`
	Sequence      int        `json:"sequence"`
	Status        string     `json:"status"`
	BalanceID     *string    `json:"balance_id"`
	PrintedAt     *time.Time `json:"printed_at"`
	LastPrintedAt *time.Time `json:"last_printed_at"`
	PrintCount    int        `json:"print_count"`
	PrintedBy     *string    `json:"printed_by"`
	IssuedAt      time.Time  `json:"issued_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// ProductUnitEvent records an issuance, transition, or print event.
type ProductUnitEvent struct {
	ID            string          `json:"id"`
	UnitID        string          `json:"unit_id"`
	Code          string          `json:"code"`
	EventType     string          `json:"event_type"`
	FromStatus    *string         `json:"from_status"`
	ToStatus      *string         `json:"to_status"`
	ReferenceType string          `json:"reference_type"`
	ReferenceID   *string         `json:"reference_id"`
	ActorUserID   *string         `json:"actor_user_id"`
	Metadata      json.RawMessage `json:"metadata"`
	CreatedAt     time.Time       `json:"created_at"`
}

// OrderItemUnit records which unit an order captured and when it was restored.
type OrderItemUnit struct {
	OrderItemID string     `json:"order_item_id"`
	UnitID      string     `json:"unit_id"`
	Code        string     `json:"code"`
	OrderID     string     `json:"order_id"`
	CapturedAt  time.Time  `json:"captured_at"`
	RestoredAt  *time.Time `json:"restored_at"`
}
