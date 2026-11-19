INSERT INTO users(id,mobile,full_name,role)
SELECT md5('cap-customer-'||i)::uuid,'099'||lpad(i::text,8,'0'),'cap-customer-'||i,'customer' FROM generate_series(1,1000)i ON CONFLICT DO NOTHING;
INSERT INTO users(id,mobile,full_name,role)
SELECT md5('cap-seller-'||i)::uuid,'098'||lpad(i::text,8,'0'),'cap-seller-'||i,'seller' FROM generate_series(1,100)i ON CONFLICT DO NOTHING;
INSERT INTO users(id,mobile,full_name,role) SELECT md5('cap-supplier-user-'||i)::uuid,'097'||lpad(i::text,8,'0'),'cap-supplier-user-'||i,'supplier' FROM generate_series(1,100)i ON CONFLICT DO NOTHING;
INSERT INTO user_capabilities(user_id,capability,source_type) SELECT id,role,'capacity_fixture' FROM users WHERE full_name LIKE 'cap-%' ON CONFLICT DO NOTHING;
INSERT INTO suppliers(id,name,owner_user_id,approval_status) SELECT 'cap-supplier-'||i,'Capacity supplier '||i,md5('cap-supplier-user-'||i)::uuid,'approved' FROM generate_series(1,100)i ON CONFLICT DO NOTHING;
INSERT INTO supplier_shipping_methods(owner_user_id,method,status) SELECT md5('cap-supplier-user-'||i)::uuid::text,'direct','enabled' FROM generate_series(1,100)i ON CONFLICT DO NOTHING;
INSERT INTO supplier_warehouses(id,supplier_id,name,province_name,city_name,address) SELECT 'cap-swh-'||i,'cap-supplier-'||i,'Capacity supplier warehouse '||i,'Tehran','Tehran','Synthetic origin' FROM generate_series(1,100)i ON CONFLICT DO NOTHING;
INSERT INTO stores(id,name,slug,owner_name,owner_user_id)
SELECT 'cap-store-'||i,'Capacity store '||i,'cap-store-'||i,'Synthetic seller',md5('cap-seller-'||i)::uuid FROM generate_series(1,100)i ON CONFLICT DO NOTHING;
INSERT INTO categories(id,title,slug,icon_name) VALUES('cap-category','Capacity','cap-category','box') ON CONFLICT DO NOTHING;
INSERT INTO products(id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,stock,length_mm,width_mm,height_mm,weight_grams,measurement_status,fulfillment_method,origin_supplier_warehouse_id)
SELECT 'cap-product-'||i,'Capacity product '||i,'cap-product-'||i,'cap-category','cap-supplier-'||(1+(i%100)),120000,100000,10000,0,100,100,100,100,'verified','supplier_direct','cap-swh-'||(1+(i%100)) FROM generate_series(1,10000)i ON CONFLICT DO NOTHING;
INSERT INTO store_products(store_id,product_id) SELECT 'cap-store-'||(1+(i%100)),'cap-product-'||i FROM generate_series(1,10000)i ON CONFLICT DO NOTHING;
INSERT INTO product_variants(id,product_id,spnc,variant_name,stock,is_default)
SELECT md5('cap-sku-'||i)::uuid,'cap-product-'||(1+((i-1)/5)),'CAP'||lpad(i::text,8,'0'),'cap-sku-'||i,100,(i%5=1) FROM generate_series(1,50000)i ON CONFLICT DO NOTHING;
INSERT INTO inventory_balances(id,product_id,supplier_id,owner_type,location_type,location_key,status,quantity,reconciled,supplier_warehouse_id)
SELECT 'cap-balance-'||i,'cap-product-'||i,'cap-supplier-'||(1+(i%100)),'supplier','supplier','supplier-warehouse:cap-swh-'||(1+(i%100)),'sellable',500,true,'cap-swh-'||(1+(i%100)) FROM generate_series(1,10000)i ON CONFLICT (id) DO UPDATE SET quantity=EXCLUDED.quantity;
INSERT INTO inventory_movements(id,product_id,supplier_id,quantity,to_status,to_location_key,reference_type,reference_id,idempotency_key,metadata)
SELECT 'cap-receipt-'||i,'cap-product-'||i,'cap-supplier-'||(1+(i%100)),500,'sellable','supplier-warehouse:cap-swh-'||(1+(i%100)),'capacity_synthetic_receipt','cap-receipt-'||i,'cap-receipt-'||i,'{"synthetic":true,"source":"capacity fixture external receipt"}'::jsonb FROM generate_series(1,10000)i ON CONFLICT DO NOTHING;
INSERT INTO orders(id,order_number,store_id,store_name,total_amount,payable_amount,customer_full_name,customer_phone,customer_province,customer_city,customer_address,customer_postal_code,status,tracking_code,customer_user_id,created_at)
SELECT 'cap-order-'||i,'CAP-'||i,'cap-store-'||(1+(i%100)),'Capacity store',110000,110000,'Synthetic customer','099'||lpad((1+(i%1000))::text,8,'0'),'Tehran','Tehran','Synthetic address','0000000000','cancelled','CAP-TRACK-'||i,md5('cap-customer-'||(1+(i%1000)))::uuid,now()-(i%365)*interval '1 day' FROM generate_series(1,100000)i ON CONFLICT DO NOTHING;
INSERT INTO order_items(order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price)
SELECT 'cap-order-'||i,'cap-product-'||(1+(i%10000)),1,100000,10000,0,110000 FROM generate_series(1,100000)i ON CONFLICT DO NOTHING;
ANALYZE users; ANALYZE suppliers; ANALYZE stores; ANALYZE products; ANALYZE product_variants; ANALYZE orders; ANALYZE order_items;
