package seeder

// PlatformSettingsBootstrap represents standard production configuration
type PlatformSettingsBootstrap struct {
	ID                            int
	BaseFeeF                      int64
	MinProfitMargin               float64
	MaxProfitMargin               float64
	DefaultMargin                 float64
	AttributionModel              string
	CookieDays                    int
	DeliveryHoldPeriodDays        int
	DeliveryFollowupTriggerDays   int
	ReferralCommissionPercent     float64
	ReferralIsEnabled             bool
	ReferralSubjectToDeliveryLock bool
	SellerContractVersion         int
	SellerContractText            string
	SupplierContractVersion       int
	SupplierContractText          string
}

// CategoryBootstrap represents system category taxonomy
type CategoryBootstrap struct {
	ID       string
	Title    string
	Slug     string
	IconName string
	Gradient string
	ImageURL string
}

// NicheBootstrap represents official 9 niche taxonomy
type NicheBootstrap struct {
	ID           string
	Title        string
	Slug         string
	CategorySlug string
	Badge        string
	Description  string
}

// DefaultBootstrapSettings returns production baseline platform governance
func DefaultBootstrapSettings() PlatformSettingsBootstrap {
	return PlatformSettingsBootstrap{
		ID:                            1,
		BaseFeeF:                      100000,
		MinProfitMargin:               5.00,
		MaxProfitMargin:               35.00,
		DefaultMargin:                 20.00,
		AttributionModel:              "last_touch",
		CookieDays:                    30,
		DeliveryHoldPeriodDays:        20,
		DeliveryFollowupTriggerDays:   10,
		ReferralCommissionPercent:     2.00,
		ReferralIsEnabled:             true,
		ReferralSubjectToDeliveryLock: true,
		SellerContractVersion:         2,
		SellerContractText:            "قرارداد رسمی همکاری تجاری دراپ‌شیپینگ دروپ (سند شماره ۵۰۴)",
		SupplierContractVersion:       2,
		SupplierContractText:          "قرارداد رسمی تأمین و توزیع کالای دروپ (سند شماره ۵۰۵)",
	}
}

// DefaultBootstrapCategories returns official core categories
func DefaultBootstrapCategories() []CategoryBootstrap {
	return []CategoryBootstrap{
		{ID: "cat-digital", Title: "کالای دیجیتال", Slug: "digital", IconName: "Smartphone", Gradient: "from-blue-500 to-indigo-600", ImageURL: "https://web-cdn.snapp.ir/fintech-marketing/ecommerce/categories/digital.png"},
		{ID: "cat-fashion", Title: "مد و پوشاک", Slug: "fashion", IconName: "Shirt", Gradient: "from-pink-500 to-rose-600", ImageURL: "https://web-cdn.snapp.ir/fintech-marketing/ecommerce/categories/fashion.png"},
		{ID: "cat-beauty", Title: "زیبایی و سلامت", Slug: "beauty", IconName: "Sparkles", Gradient: "from-purple-500 to-violet-600", ImageURL: "https://web-cdn.snapp.ir/fintech-marketing/ecommerce/categories/beauty.png"},
		{ID: "cat-appliances", Title: "لوازم خانگی", Slug: "appliances", IconName: "Home", Gradient: "from-amber-500 to-orange-600", ImageURL: "https://web-cdn.snapp.ir/fintech-marketing/ecommerce/categories/appliances.png"},
		{ID: "cat-gold", Title: "طلا و زیورآلات", Slug: "gold", IconName: "Coins", Gradient: "from-yellow-400 to-amber-500", ImageURL: "https://web-cdn.snapp.ir/fintech-marketing/ecommerce/categories/gold.png"},
	}
}

// DefaultBootstrapNiches returns the 9 official platform niche taxonomies
func DefaultBootstrapNiches() []NicheBootstrap {
	return []NicheBootstrap{
		{ID: "niche-smartwatch", Title: "ساعت هوشمند و گجت‌های پوشیدنی", Slug: "smartwatch", CategorySlug: "digital", Badge: "پرفروش‌ترین", Description: "ساعت‌های هوشمند با پشتیبانی اعلان فارسی"},
		{ID: "niche-powerbank", Title: "پاوربانک و تجهیزات شارژ سریع", Slug: "powerbank", CategorySlug: "digital", Badge: "ارسال اکسپرس", Description: "شارژرها و پاوربانک‌های فست‌شارژ استاندارد"},
		{ID: "niche-headphone", Title: "هدفون، هندزفری و اسپیکر بلوتوثی", Slug: "headphone", CategorySlug: "digital", Badge: "تضمین اصالت", Description: "هندزفری‌های بلوتوثی با نویزکنسلینگ"},
		{ID: "niche-gold18", Title: "طلا و زیورآلات ۱۸ عیار استاندارد", Slug: "gold-jewelry", CategorySlug: "gold", Badge: "ارسال بیمه‌شده", Description: "زیورآلات طلای ۱۸ عیار با کد رهگیری پستی"},
		{ID: "niche-leather", Title: "کیف و اکسسوری چرم طبیعی", Slug: "leather-bags", CategorySlug: "fashion", Badge: "دست‌دوز تبریز", Description: "محصولات چرم طبیعی گاوی و تبریز"},
		{ID: "niche-home-appliances", Title: "لوازم برقی و پخت‌وپز خانه", Slug: "home-appliances", CategorySlug: "appliances", Badge: "گارانتی معتبر", Description: "سرخ‌کن بدون روغن، قهوه‌ساز و غذاساز"},
		{ID: "niche-skincare", Title: "مراقبت پوست، مو و زیبایی", Slug: "skincare", CategorySlug: "beauty", Badge: "ضمانت بازگشت", Description: "کرم‌ها و سرم‌های بهداشتی با برچسب اصالت"},
		{ID: "niche-books", Title: "کتاب کنکور، رمان و نوشت‌افزار", Slug: "stationery-books", CategorySlug: "digital", Badge: "ارسال ارزان", Description: "کتاب‌های دانشگاهی و لوازم‌التحریر فانتزی"},
		{ID: "niche-auto", Title: "لوازم فنی، نظم‌دهنده و نگهداری خودرو", Slug: "auto-accessories", CategorySlug: "digital", Badge: "تجهیزات سفر", Description: "جامپ‌استارتر، پمپ باد پرتابل و هولدر"},
	}
}
