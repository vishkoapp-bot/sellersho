-- FIN-012 read-only inventory. Run against a controlled reconciliation copy.
-- Reports identifiers and numerical values only. Never applies current pricing
-- to paid history, rewrites percentages, or modifies money. FIN-014 owns the
-- review of a real historical report before production writes are enabled.
WITH inputs(source,record_id,field,value_text,input_kind,max_value) AS (
  SELECT 'platform_settings',s.id::text,j.key,j.value #>> '{}','percent',
         CASE WHEN j.key IN ('central_profit_pct','early_withdrawal_fee_pct','referral_commission_percent') THEN 100::numeric ELSE 1000::numeric END
    FROM platform_settings s CROSS JOIN LATERAL jsonb_each(to_jsonb(s)) j
   WHERE j.key IN ('platform_pct','central_profit_pct','increase_cap_pct','max_profit_margin','min_profit_margin','default_margin','early_withdrawal_fee_pct','referral_commission_percent')
  UNION ALL SELECT 'platform_pct_products',product_id,'pct',pct::text,'percent',1000 FROM platform_pct_products
  UNION ALL SELECT 'platform_pct_categories',category_slug,'pct',pct::text,'percent',1000 FROM platform_pct_categories
  UNION ALL SELECT 'product_margin_ceilings',product_id,'max_margin_pct',max_margin_pct::text,'percent',1000 FROM product_margin_ceilings
  UNION ALL SELECT 'store_product_overrides',store_id||'/'||product_id,'override_value',override_value::text,CASE WHEN mode='percent' THEN 'percent' ELSE 'legacy_fixed_inert' END,1000 FROM store_product_overrides
  UNION ALL SELECT 'store_niche_overrides',store_id||'/'||niche_id,'override_value',override_value::text,CASE WHEN mode='percent' THEN 'percent' ELSE 'legacy_fixed_inert' END,1000 FROM store_niche_overrides
  UNION ALL SELECT 'store_variant_overrides',store_id||'/'||variant_id::text,'override_value',override_value::text,CASE WHEN mode='percent' THEN 'percent' ELSE 'legacy_fixed_inert' END,1000 FROM store_variant_overrides
  UNION ALL SELECT 'store_margin_locks',store_id||'/'||scope||'/'||scope_id,'lock_value',lock_value::text,'percent',1000 FROM store_margin_locks WHERE lock_mode='admin_set'
  UNION ALL SELECT 'stores',id,'custom_store_margin_override',custom_store_margin_override::text,'percent',1000 FROM stores WHERE is_margin_locked_by_admin
  UNION ALL SELECT 'seller_discount_campaigns',id::text,'value',value::text,CASE WHEN mode='percent' THEN 'percent' ELSE 'money' END,CASE WHEN mode='percent' THEN 100::numeric ELSE 9007199254740991::numeric END FROM seller_discount_campaigns
  UNION ALL SELECT 'seller_discount_codes',id,'value',value::text,CASE WHEN mode='percent' THEN 'percent' ELSE 'money' END,CASE WHEN mode='percent' THEN 100::numeric ELSE 9007199254740991::numeric END FROM seller_discount_codes
  UNION ALL SELECT 'product_addons',id::text,'price_value',price_value::text,CASE WHEN price_mode='percent' THEN 'percent' ELSE 'money' END,CASE WHEN price_mode='percent' THEN 1000::numeric ELSE 9007199254740991::numeric END FROM product_addons
  UNION ALL SELECT 'stores.widget_discounts',s.id||'/'||j.key,'discounts',j.value #>> '{}','percent',100
    FROM stores s CROSS JOIN LATERAL jsonb_each(CASE WHEN jsonb_typeof(s.seller_deals_widget_config->'discounts')='object' THEN s.seller_deals_widget_config->'discounts' ELSE '{}'::jsonb END) j
), classified AS (
  SELECT source,record_id,field,value_text,
         CASE WHEN input_kind='legacy_fixed_inert' THEN 'legacy_fixed_inert'
              WHEN value_text !~ '^[+-]?[0-9]+([.][0-9]+)?([eE][+-]?[0-9]+)?$' THEN 'nonfinite_or_nonnumeric'
              WHEN value_text::numeric<0 OR value_text::numeric>max_value THEN 'out_of_range'
              WHEN input_kind='percent' AND value_text::numeric*100<>trunc(value_text::numeric*100) THEN 'unsupported_percent_precision'
              WHEN input_kind='money' AND value_text::numeric<>trunc(value_text::numeric) THEN 'fractional_toman'
         END AS reason
    FROM inputs WHERE value_text IS NOT NULL
), historical AS (
  SELECT 'historical_order_items'::text AS source,id::text AS record_id,
         'platform_fee'::text AS field,platform_fee::text AS value_text,'negative_platform_fee'::text AS reason
    FROM order_items WHERE platform_fee<0
  UNION ALL SELECT 'historical_order_items',id::text,'seller_margin',seller_margin::text,'negative_seller_margin'
    FROM order_items WHERE seller_margin<0
  UNION ALL SELECT 'historical_order_items',id::text,'final_unit_price',final_unit_price::text,'snapshot_components_do_not_sum'
    FROM order_items WHERE wholesale_cost::numeric+platform_fee::numeric+seller_margin::numeric<>final_unit_price::numeric
)
SELECT source,record_id,field,value_text,reason FROM classified WHERE reason IS NOT NULL
UNION ALL SELECT source,record_id,field,value_text,reason FROM historical
ORDER BY source,record_id,field,reason;
