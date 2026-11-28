package repository

// Availability mirrors the stock and quantity rules used by the public
// purchase flow. A legacy product carrying no variants uses product stock.
// A variant must be visible in this storefront and meet its minimum order
// quantity. A placeholder for the parent product never counts as stock.
const storefrontStockPredicate = `(
		(
			COALESCE(to_jsonb(p)->>'product_type', 'legacy') <> 'variable'
			AND COALESCE((to_jsonb(p)->>'schema_version')::integer, 0) <> 2
			AND p.stock > 0
			AND NOT EXISTS (SELECT 1 FROM product_variants any_variant WHERE any_variant.product_id = p.id)
		)
		OR EXISTS (
			SELECT 1
			FROM product_variants v
			LEFT JOIN store_variant_visibility svv
				ON svv.store_id = scope_store.id AND svv.product_id = v.product_id AND svv.variant_id = v.id
			CROSS JOIN LATERAL (
				SELECT GREATEST(COALESCE((to_jsonb(v)->>'min_cart_qty')::integer, 1), 1) AS minimum,
					COALESCE(to_jsonb(p)->>'product_type', 'legacy') AS kind,
					COALESCE((to_jsonb(p)->>'schema_version')::integer, 0) AS schema_version
			) limits
			WHERE v.product_id = p.id
				AND svv.is_visible IS DISTINCT FROM FALSE
				AND COALESCE((to_jsonb(v)->>'is_active')::boolean, TRUE)
				AND (CASE
					WHEN COALESCE(to_jsonb(v)->>'inventory_mode', 'legacy') = 'tracked'
						OR limits.kind = 'variable' OR limits.schema_version = 2 OR v.stock > 0 THEN v.stock
					ELSE p.stock
				END) >= limits.minimum
				AND COALESCE((to_jsonb(v)->>'max_cart_qty')::integer, limits.minimum) >= limits.minimum
				AND NOT (
					(
						limits.kind = 'variable'
						OR (limits.kind <> 'simple' AND EXISTS (
							SELECT 1 FROM product_variants sibling
							LEFT JOIN store_variant_visibility sibling_visibility
								ON sibling_visibility.store_id = scope_store.id AND sibling_visibility.product_id = sibling.product_id
								AND sibling_visibility.variant_id = sibling.id
							WHERE sibling.product_id = p.id AND sibling.id <> v.id
								AND sibling_visibility.is_visible IS DISTINCT FROM FALSE
						))
					)
					AND (
						regexp_replace(translate(COALESCE(v.variant_name, ''), chr(8204) || chr(8205), ''), '[[:space:]]', '', 'g') = 'پیشفرض'
						OR (limits.kind <> 'variable' AND btrim(COALESCE(v.variant_name, '')) = btrim(p.title))
					)
					AND COALESCE(to_jsonb(v)->'selection', '{}'::jsonb) = '{}'::jsonb
					AND NOT EXISTS (
						SELECT 1 FROM jsonb_each_text(
							CASE WHEN jsonb_typeof(v.attributes) = 'object' THEN v.attributes ELSE '{}'::jsonb END
						) attribute WHERE btrim(attribute.value) <> ''
					)
				)
		)
	)`
