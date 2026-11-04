package seeder

// GetDevUsers returns strictly isolated test fixture users (0900 reserved test mobile numbers)
func GetDevUsers() []UserDevFixture {
	return []UserDevFixture{
		{ID: "00000000-0000-0000-0000-000000000001", Mobile: "09000000001", FullName: "مدیر ارشد پلتفرم (تستی)", Role: "admin", Username: "admin", PasswordEnv: "ADMIN_PASSWORD"},
		{ID: "00000000-0000-0000-0000-000000000002", Mobile: "09000000002", FullName: "کارشناس مالی و پایا (تستی)", Role: "finance_specialist", Username: "finance", PasswordEnv: "FINANCE_PASSWORD"},
		{ID: "00000000-0000-0000-0000-000000000003", Mobile: "09000000003", FullName: "سرپرست لجستیک (تستی)", Role: "logistics_specialist", Username: "logistics", PasswordEnv: "LOGISTICS_PASSWORD"},
		{ID: "00000000-0000-0000-0000-000000000007", Mobile: "09000000007", FullName: "اپراتور انبار مرکزی (تستی)", Role: "warehouse_staff", Username: "warehouse", PasswordEnv: "WAREHOUSE_PASSWORD"},
		{ID: "00000000-0000-0000-0000-000000000008", Mobile: "09000000008", FullName: "کارشناس پشتیبانی (تستی)", Role: "support_specialist", Username: "support", PasswordEnv: "SUPPORT_PASSWORD"},
		{ID: "00000000-0000-0000-0000-000000000009", Mobile: "09000000009", FullName: "مدیر تأمین تکنولایف (تستی)", Role: "supplier", Username: "technolife", PasswordEnv: "SUPPLIER_PASSWORD"},
		{ID: "00000000-0000-0000-0000-000000000004", Mobile: "09121112233", FullName: "بهاره رضوانی (سلر بی‌بی استایل)", Role: "seller"},
		{ID: "00000000-0000-0000-0000-000000000005", Mobile: "09120000000", FullName: "کاربر جدید ثبت‌نامی (تستی)", Role: "seller"},
		{ID: "00000000-0000-0000-0000-000000000006", Mobile: "09123456789", FullName: "حمیدرضا سلطانی (خریدار نمونه)", Role: "customer"},
	}
}

// GetDevSuppliers returns mock vendors
func GetDevSuppliers() []SupplierDevFixture {
	return []SupplierDevFixture{
		{ID: "sup-khanoumi", Name: "خانومی", LogoURL: "https://web-cdn.snapp.ir/fintech/merchant-growth/img/khanoumi-large.png", Rating: 4.90, TotalProducts: 420, DeliveryScore: 99, WarehouseCity: "تهران", Description: "مرجع تخصصی آرایشی، بهداشتی و مراقبت پوست با ضمانت اصالت کالا", Badge: "٪۳ بازگشت وجه"},
		{ID: "sup-technolife", OwnerUserID: "00000000-0000-0000-0000-000000000009", Name: "تکنولایف", LogoURL: "https://web-cdn.snapp.ir/fintech/merchant-growth/img/technolife-large.png", Rating: 4.90, TotalProducts: 850, DeliveryScore: 99, WarehouseCity: "تهران", Description: "تأمین‌کننده معتبر گوشی، لپ‌تاپ، لوازم جانبی و گجت‌های هوشمند", Badge: "ارسال فوری"},
		{ID: "sup-filimo", Name: "فیلیمو", LogoURL: "https://web-cdn.snapp.ir/fintech/merchant-growth/img/filimo-large.png", Rating: 4.80, TotalProducts: 15, DeliveryScore: 98, WarehouseCity: "تهران", Description: "بزرگترین پلتفرم تماشای آنلاین فیلم و سریال در ایران", Badge: "تخفیف اشتراک"},
	}
}

// GetDevStores returns mock dropshipper stores with referral tree
func GetDevStores() []StoreDevFixture {
	return []StoreDevFixture{
		{ID: "store-central", Name: "فروشگاه رسمی مرکزی سلرشو", Slug: "central", OwnerType: "central", OwnerName: "مدیریت مرکزی سلرشو", ProfitMarginPercent: 0.00, ActivePlanID: "vip", ReferralCode: "DRUP-ROOT", ReferrerSellerID: "", TotalSales: 184500000},
		{ID: "store-bibi", Name: "فروشگاه بی‌بی استایل (Bibi Style)", Slug: "bibi", OwnerType: "dropshipper", OwnerName: "بهاره رضوانی", OwnerUserID: "00000000-0000-0000-0000-000000000004", ProfitMarginPercent: 12.00, ActivePlanID: "trial", ReferralCode: "DRUP-BIBI", ReferrerSellerID: "", TotalSales: 44100000},
		{ID: "store-sepehr", Name: "موبایل و دیجیتال سپهر", Slug: "sepehr-digital", OwnerType: "dropshipper", OwnerName: "سپهر زمانی", ProfitMarginPercent: 15.00, ActivePlanID: "standard", ReferralCode: "DRUP-SEPH", ReferrerSellerID: "store-bibi", TotalSales: 52000000},
		{ID: "store-parsian", Name: "گالری پوشاک و چرم پارسیان", Slug: "parsian-style", OwnerType: "dropshipper", OwnerName: "نیما پارسا", ProfitMarginPercent: 14.00, ActivePlanID: "pro", ReferralCode: "DRUP-PARS", ReferrerSellerID: "store-sepehr", TotalSales: 38000000},
	}
}
