package seeder

// UserDevFixture represents non-production demo user accounts
type UserDevFixture struct {
	ID          string
	Mobile      string
	FullName    string
	Role        string
	Username    string
	PasswordEnv string
}

// SupplierDevFixture represents mock suppliers
type SupplierDevFixture struct {
	ID            string
	OwnerUserID   string
	Name          string
	LogoURL       string
	Rating        float64
	TotalProducts int
	DeliveryScore int
	WarehouseCity string
	Description   string
	Badge         string
}

// StoreDevFixture represents mock stores
type StoreDevFixture struct {
	ID                  string
	Name                string
	Slug                string
	OwnerType           string
	OwnerName           string
	OwnerUserID         string
	ProfitMarginPercent float64
	ActivePlanID        string
	ReferralCode        string
	ReferrerSellerID    string
	TotalSales          int64
}

// ProductDevFixture represents mock catalog items
type ProductDevFixture struct {
	ID               string
	Title            string
	Slug             string
	CategorySlug     string
	SupplierID       string
	OriginalPrice    int64
	SupplierBaseCost int64
	PlatformMargin   int64
	IsOriginal       bool
	Stock            int
	ImageURL         string
	Description      string
	NicheIDs         []string
}

// WarehouseBinDevFixture represents shelf locations in WMS
type WarehouseBinDevFixture struct {
	BinCode         string
	HallName        string
	Aisle           string
	Shelf           string
	CapacityPercent int
	ProductID       string
	Quantity        int
}

// InboundDevFixture represents incoming supplier trucks
type InboundDevFixture struct {
	ID           string
	SupplierID   string
	SupplierName string
	ItemsCount   int
	CartonsCount int
	Carrier      string
	TruckPlate   string
	Status       string
}

// ManifestDevFixture represents postal carrier dispatch batches
type ManifestDevFixture struct {
	ID            string
	Carrier       string
	DriverName    string
	PackagesCount int
	Status        string
}

// LedgerDevFixture represents sample double-entry bookkeeping records
type LedgerDevFixture struct {
	ID                    string
	TargetAccount         string
	TargetType            string
	Type                  string
	Amount                int64
	Reason                string
	Operator              string
	RelatedOrderOrDispute string
}
