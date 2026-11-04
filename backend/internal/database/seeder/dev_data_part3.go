package seeder

// GetDevBins returns mock warehouse bin locations
func GetDevBins() []WarehouseBinDevFixture {
	return []WarehouseBinDevFixture{
		{BinCode: "B-12-03", HallName: "سالن B (کالای دیجیتال)", Aisle: "راهرو ۱۲", Shelf: "طبقه ۳", CapacityPercent: 80, ProductID: "prod-1", Quantity: 45},
		{BinCode: "B-12-04", HallName: "سالن B (کالای دیجیتال)", Aisle: "راهرو ۱۲", Shelf: "طبقه ۴", CapacityPercent: 60, ProductID: "prod-2", Quantity: 80},
		{BinCode: "A-04-01", HallName: "سالن A (پوشاک و چرم)", Aisle: "راهرو ۰۴", Shelf: "طبقه ۱", CapacityPercent: 45, ProductID: "prod-5", Quantity: 120},
	}
}

// GetDevInbounds returns sample inbound truck loads
func GetDevInbounds() []InboundDevFixture {
	return []InboundDevFixture{
		{ID: "INB-9941", SupplierID: "sup-technolife", SupplierName: "تکنولایف", ItemsCount: 155, CartonsCount: 12, Carrier: "ناوگان باربری پیشتاز", TruckPlate: "ایران ۱۱ - ۷۸۹ ج ۳۴", Status: "received"},
	}
}

// GetDevManifests returns sample carrier shipping manifests
func GetDevManifests() []ManifestDevFixture {
	return []ManifestDevFixture{
		{ID: "MAN-501", Carrier: "شرکت ملی پست جمهوری اسلامی ایران (پیشتاز)", DriverName: "حمید سجادی (کد مأمور: ۴۴۱)", PackagesCount: 18, Status: "handed_over"},
		{ID: "MAN-502", Carrier: "کوریر اکسپرس تیپاکس", DriverName: "پژمان علیزاده", PackagesCount: 6, Status: "pending_pickup"},
	}
}

// GetDevLedger returns sample immutable accounting ledger entries
func GetDevLedger() []LedgerDevFixture {
	return []LedgerDevFixture{
		{ID: "ADJ-301", TargetAccount: "شرکت بازرگانی فناوری نوین (تأمین‌کننده)", TargetType: "supplier", Type: "debit", Amount: 350000, Reason: "کسر هزینه ناشی از مغایرت در کنترل کیفیت (QC)", Operator: "کارشناس تطبیق حسابداری", RelatedOrderOrDispute: "DISP-701"},
		{ID: "ADJ-302", TargetAccount: "فروشگاه رایان دیجیتال (دراپ‌شیپر)", TargetType: "seller", Type: "credit", Amount: 125000, Reason: "استرداد مانده بستانکاری بابت اختلاف نرخ کارمزد F پلتفرم", Operator: "مدیر ارشد مالی", RelatedOrderOrDispute: "ORD-702774"},
	}
}
