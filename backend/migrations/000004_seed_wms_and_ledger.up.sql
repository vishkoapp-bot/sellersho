-- Drup Platform - Seed WMS, Bins, Manifests and Disputes Migration (000004)

-- 1. Sample Warehouse Bins
INSERT INTO warehouse_bins (bin_code, hall_name, aisle, shelf, capacity_percent, product_id, quantity) VALUES
('B-12-03', 'سالن B (کالای دیجیتال)', 'راهرو ۱۲', 'طبقه ۳', 80, 'prod-1', 45),
('B-12-04', 'سالن B (کالای دیجیتال)', 'راهرو ۱۲', 'طبقه ۴', 60, 'prod-2', 80),
('A-04-01', 'سالن A (پوشاک و چرم)', 'راهرو ۰۴', 'طبقه ۱', 45, 'prod-5', 120)
ON CONFLICT (bin_code) DO NOTHING;

-- 2. Sample Inbound Shipment
INSERT INTO inbound_shipments (id, supplier_id, supplier_name, items_count, cartons_count, carrier, truck_plate, status) VALUES
('INB-9941', 'sup-technolife', 'تکنولایف', 155, 12, 'ناوگان باربری پیشتاز', 'ایران ۱۱ - ۷۸۹ ج ۳۴', 'received')
ON CONFLICT (id) DO NOTHING;

-- 3. Sample Shipping Manifests
INSERT INTO shipping_manifests (id, carrier, driver_name, packages_count, status) VALUES
('MAN-501', 'شرکت ملی پست جمهوری اسلامی ایران (پیشتاز)', 'حمید سجادی (کد مأمور: ۴۴۱)', 18, 'handed_over'),
('MAN-502', 'کوریر اکسپرس تیپاکس', 'پژمان علیزاده', 6, 'pending_pickup')
ON CONFLICT (id) DO NOTHING;

-- 4. Sample Ledger Entries
INSERT INTO ledger_entries (id, target_account, target_type, type, amount, reason, operator, related_order_or_dispute) VALUES
('ADJ-301', 'شرکت بازرگانی فناوری نوین (تأمین‌کننده)', 'supplier', 'debit', 350000, 'کسر هزینه ناشی از مغایرت در کنترل کیفیت (QC)', 'کارشناس تطبیق حسابداری', 'DISP-701'),
('ADJ-302', 'فروشگاه رایان دیجیتال (دراپ‌شیپر)', 'seller', 'credit', 125000, 'استرداد مانده بستانکاری بابت اختلاف نرخ کارمزد F پلتفرم', 'مدیر ارشد مالی', 'ORD-702774')
ON CONFLICT (id) DO NOTHING;
