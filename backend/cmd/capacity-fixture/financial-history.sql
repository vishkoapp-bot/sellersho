-- Synthetic historical snapshots, only in the disposable guarded CAP database.
-- This is load shape, not proof of production history or a migration repair.
-- CAP-01 (2026-11-20): the UPDATE below flips profit_released false->true,
-- which the FIN005 writer fence (migration 202611160002, Nov 16) only allows
-- when the session carries sellersho.fin005_writer='v1' — the same
-- transaction-local set_config the app's own release path uses
-- (internal/repository/dispute_resolution.go). Without it the fixture
-- fails on current HEAD (and would fail identically under Docker).
SELECT set_config('sellersho.fin005_writer','v1',true);
CREATE TEMP TABLE cap_history AS SELECT i, 'cap-order-'||i id,
  ((i-1)/100)%10 bucket, 1+(i%10000) product_number,
  now()-(30+i%330)*interval '1 day' occurred
FROM generate_series(1,100000)i;
CREATE UNIQUE INDEX ON cap_history(id);
UPDATE order_items oi SET quantity=1+(h.i%3),seller_margin=13200,final_unit_price=123200,
 variant_id=md5('cap-sku-'||((h.product_number-1)*5+1))::uuid
FROM cap_history h WHERE oi.order_id=h.id;
INSERT INTO order_items(order_id,product_id,quantity,wholesale_cost,platform_fee,seller_margin,final_unit_price,variant_id)
SELECT id,'cap-product-'||(1+((product_number+99)%10000)),1,100000,10000,13200,123200,
 md5('cap-sku-'||(((product_number+99)%10000)*5+1))::uuid
FROM cap_history WHERE i%5<2;
UPDATE orders o SET store_id='cap-store-'||(1+(h.product_number%100)),
 status=CASE WHEN bucket<6 THEN 'delivered_confirmed' WHEN bucket=6 THEN 'shipped' WHEN bucket=7 THEN 'processing' WHEN bucket=8 THEN 'cancelled' ELSE 'pending' END,
 payment_status=CASE WHEN bucket<8 THEN 'paid' WHEN bucket=8 THEN 'cancelled' ELSE 'pending' END,
 created_at=h.occurred,updated_at=h.occurred+interval '10 days',
 shipped_at=CASE WHEN bucket<7 THEN h.occurred+interval '1 day' END,
 confirmed_at=CASE WHEN bucket<6 THEN h.occurred+interval '3 days' END,
 return_window_ends_at=CASE WHEN bucket<6 THEN h.occurred+interval '10 days' END,
 profit_released=bucket<6 AND h.i>100,
 profit_released_at=CASE WHEN bucket<6 AND h.i>100 THEN h.occurred+interval '10 days' END,
 inventory_captured=bucket<8
FROM cap_history h WHERE o.id=h.id;
UPDATE orders o SET total_amount=s.total,payable_amount=s.total,dropshipper_profit=s.seller,platform_profit=s.platform
FROM (SELECT order_id,sum(quantity*final_unit_price) total,sum(quantity*seller_margin) seller,sum(quantity*platform_fee) platform
 FROM order_items WHERE order_id LIKE 'cap-order-%' GROUP BY order_id) s WHERE o.id=s.order_id;
INSERT INTO order_item_origins(id,order_id,order_item_id,leg_index,origin_type,supplier_id,origin_label,quantity,supplier_warehouse_id,created_at)
SELECT 'cap-origin-'||oi.id,oi.order_id,oi.id,0,'supplier',p.supplier_id,'Synthetic immutable supplier origin',oi.quantity,p.origin_supplier_warehouse_id,o.created_at
FROM order_items oi JOIN orders o ON o.id=oi.order_id JOIN products p ON p.id=oi.product_id WHERE o.id LIKE 'cap-order-%';
INSERT INTO payment_transactions(id,order_id,order_number,gateway_token,rrn,amount,status,created_at)
SELECT 'cap-payment-'||h.i,o.id,o.order_number,'synthetic-cap-token-'||h.i,'CAP-RRN-'||h.i,o.payable_amount,'paid',o.created_at
FROM orders o JOIN cap_history h ON h.id=o.id WHERE h.bucket<8;
-- Historical source receipts were consumed before the current 5m-unit replenishment.
-- Captures are the sold projection, not another physical stock bucket.
INSERT INTO inventory_movements(id,product_id,supplier_id,quantity,to_status,to_location_key,reference_type,reference_id,idempotency_key,metadata,created_at)
SELECT 'cap-hist-receipt-'||oi.id,oi.product_id,src.supplier_id,oi.quantity,'sellable','supplier-warehouse:'||src.supplier_warehouse_id,
 'capacity_synthetic_receipt',oi.id::text,'cap-hist-receipt-'||oi.id,'{"synthetic":true,"historical":true}'::jsonb,o.created_at-interval '1 day'
FROM order_items oi JOIN orders o ON o.id=oi.order_id JOIN order_item_origins src ON src.order_item_id=oi.id WHERE o.id LIKE 'cap-order-%' AND o.inventory_captured;
INSERT INTO inventory_movements(id,product_id,supplier_id,variant_id,quantity,from_status,to_status,from_location_key,to_location_key,reference_type,reference_id,idempotency_key,metadata,created_at)
SELECT 'cap-hist-capture-'||oi.id,oi.product_id,src.supplier_id,oi.variant_id,oi.quantity,'sellable','sold','supplier-warehouse:'||src.supplier_warehouse_id,'customer',
 'order_capture',oi.id::text,'cap-hist-capture-'||oi.id,'{"synthetic":true,"historical":true}'::jsonb,o.created_at
FROM order_items oi JOIN orders o ON o.id=oi.order_id JOIN order_item_origins src ON src.order_item_id=oi.id WHERE o.id LIKE 'cap-order-%' AND o.inventory_captured;
INSERT INTO order_inventory_captures(order_item_id,order_id,product_id,variant_id,quantity,variant_quantity,inventory_mode,allocations,created_at)
SELECT oi.id::text,oi.order_id,oi.product_id,oi.variant_id,oi.quantity,oi.quantity,'legacy',
 jsonb_build_array(jsonb_build_object('originId',src.id,'balanceId',ib.id,'quantity',oi.quantity)),o.created_at
FROM order_items oi JOIN orders o ON o.id=oi.order_id JOIN order_item_origins src ON src.order_item_id=oi.id
JOIN inventory_balances ib ON ib.product_id=oi.product_id AND ib.status='sellable' AND ib.supplier_warehouse_id=src.supplier_warehouse_id
WHERE o.id LIKE 'cap-order-%' AND o.inventory_captured;
-- Paired synthetic postings for the bulk historical cohort only. The first100
-- orders remain unreleased and are processed by the real release helper.
INSERT INTO ledger_entries(id,date,target_account,target_type,type,amount,reason,operator,related_order_or_dispute,created_at)
SELECT 'cap-hist-credit-'||h.i,o.profit_released_at,o.store_id,'seller','credit',o.dropshipper_profit,'Synthetic historical profit release','capacity-fixture',o.id,o.profit_released_at
FROM orders o JOIN cap_history h ON h.id=o.id WHERE o.profit_released;
INSERT INTO ledger_entries(id,date,target_account,target_type,type,amount,reason,operator,related_order_or_dispute,created_at)
SELECT 'cap-hist-debit-'||h.i,o.profit_released_at,'platform','platform','debit',o.dropshipper_profit,'Synthetic historical profit release','capacity-fixture',o.id,o.profit_released_at
FROM orders o JOIN cap_history h ON h.id=o.id WHERE o.profit_released;
ANALYZE orders; ANALYZE order_items; ANALYZE order_item_origins; ANALYZE payment_transactions; ANALYZE order_inventory_captures;
DROP TABLE cap_history;
