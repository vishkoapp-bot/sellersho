package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Merge-phase backend: seller analytics and search insight reshaped
// to the live response shapes the rebuilt seller tabs read (Team 2).
//
// Analytics (live En tab): totalRevenue, orderCount, avgOrderValue,
// sellerProfit, views, conversionRate, periodComparison (current vs
// previous 30 days), statusBreakdown, daily (30 zero-filled days),
// topProducts, productRows. Headline totals are ALL-TIME paid orders
// (payment_status='paid', status != 'cancelled'); the 30-day windows
// live in periodComparison/daily/views/conversionRate. "Paid" is the
// tab's own rule: real stats appear once the first paid order exists.
// productRows covers the store's assortment (store_products joined to
// active products); per-product views have no source table, so they
// are 0 and the tab renders its no-views state. daysLeft is stock /
// 30-day sales velocity, null when nothing sold in 30 days. Return
// counts cover received/refunded/completed returns only (requested
// or rejected rows are not actual returns). Top products are all-time
// paid-order quantities. Every figure is a real aggregation; nothing
// is faked. Live aggregation details beyond the tab's field contract
// remain unverified.
//
// Search insight (live _n tab): the BARE shape {topSearches,
// unmetInStore, unmetCatalog} with no wrapper. topSearches = the
// store's most-searched terms; unmetInStore = terms that NEVER
// returned a store result, with catalogMatches from a product-title
// search for the quick-add UI; unmetCatalog = the unmet terms whose
// every recorded row also has catalog_result_count = 0 (legacy NULL
// rows are excluded — unknown is not reported as "not in catalog").
// The store_id query param filters to one owned store (404 otherwise),
// mirroring the analytics handler.

// t01MergeInClause builds a ($1, $2, ...) placeholder list for ids.
func t01MergeInClause(ids []string) (string, []interface{}) {
	placeholders := ""
	args := []interface{}{}
	for i, id := range ids {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += "$" + strconv.Itoa(i+1)
		args = append(args, id)
	}
	return placeholders, args
}

// t01MergeSellerScope resolves the caller's store scope: all owned
// stores, or the single store named by storeFilter. It returns
// (ids, slugs, true); ok is false when the filter names a store the
// caller does not own.
func t01MergeSellerScope(ctx context.Context, db *sql.DB, userID, storeFilter string) (ids, slugs []string, ok bool) {
	ids = t01SellerStoreIDs(ctx, db, userID)
	if storeFilter != "" {
		owned := false
		for _, id := range ids {
			if id == storeFilter {
				owned = true
			}
		}
		if !owned {
			return nil, nil, false
		}
		ids = []string{storeFilter}
	}
	slugs = []string{}
	if len(ids) == 0 {
		return ids, slugs, true
	}
	ph, args := t01MergeInClause(ids)
	rows, err := db.QueryContext(ctx, `SELECT slug FROM stores WHERE id IN (`+ph+`) ORDER BY id`, args...)
	if err != nil {
		return ids, slugs, true
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil {
			slugs = append(slugs, s)
		}
	}
	rows.Close()
	return ids, slugs, true
}

// handleT01SellerAnalytics serves GET /api/v1/seller/analytics.
func handleT01SellerAnalytics(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	storeIDs, slugs, ok := t01MergeSellerScope(ctx, db, userID, strings.TrimSpace(r.URL.Query().Get("storeId")))
	if !ok {
		http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
		return
	}

	// Paid orders only; paid-then-cancelled rows (refund_pending)
	// are not sales.
	const paid = `o.payment_status = 'paid' AND o.status != 'cancelled'`
	today := t01TehranDay(time.Now())
	start, end := today.AddDate(0, 0, -29), today.AddDate(0, 0, 1)
	previousStart := start.AddDate(0, 0, -30)
	loc := today.Location().String()
	window := func(column, cast string, offset int) string {
		return fmt.Sprintf(" AND %s >= $%d::%s AND %s < $%d::%s", column, offset+1, cast, column, offset+2, cast)
	}
	type periodVals struct {
		revenue int64
		orders  int64
		profit  int64
	}
	avg := func(v periodVals) int64 {
		if v.orders == 0 {
			return 0
		}
		return v.revenue / v.orders
	}
	period := func(bounds ...time.Time) (periodVals, error) {
		var v periodVals
		if len(storeIDs) == 0 {
			return v, nil
		}
		ph, args := t01MergeInClause(storeIDs)
		q := `SELECT COUNT(*), COALESCE(SUM(payable_amount), 0), COALESCE(SUM(dropshipper_profit), 0)
			FROM orders o WHERE o.store_id IN (` + ph + `) AND ` + paid
		if len(bounds) > 0 {
			q += window("o.created_at", "timestamptz", len(args))
			args = append(args, bounds[0], bounds[1])
		}
		err := db.QueryRowContext(ctx, q, args...).Scan(&v.orders, &v.revenue, &v.profit)
		return v, err
	}
	total, err := period()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	current, err := period(start, end)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	previous, err := period(previousStart, start)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	var views30 int64
	if t01TableExists(ctx, db, "store_view_daily") && len(slugs) > 0 {
		ph, args := t01MergeInClause(slugs)
		_ = db.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(views), 0) FROM store_view_daily WHERE store_slug IN (`+ph+`)`+window("day", "date", len(args)),
			append(args, start.Format(time.DateOnly), end.Format(time.DateOnly))...).Scan(&views30)
	}
	conversion := 0.0
	if views30 > 0 {
		conversion = float64(current.orders) / float64(views30)
	}

	statusRows := []map[string]interface{}{}
	if len(storeIDs) > 0 {
		ph, args := t01MergeInClause(storeIDs)
		rows, err := db.QueryContext(ctx,
			`SELECT status, COUNT(*) FROM orders o WHERE o.store_id IN (`+ph+`) AND `+paid+` GROUP BY status ORDER BY COUNT(*) DESC, status ASC`, args...)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		for rows.Next() {
			var st string
			var n int64
			if err := rows.Scan(&st, &n); err != nil {
				rows.Close()
				writeInternalError(w, err)
				return
			}
			statusRows = append(statusRows, map[string]interface{}{"status": st, "count": n})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			writeInternalError(w, err)
			return
		}
		rows.Close()
	}

	daily := []map[string]interface{}{}
	if len(storeIDs) > 0 {
		ph, args := t01MergeInClause(storeIDs)
		firstDay := fmt.Sprintf("$%d", len(args)+1)
		lastDay := fmt.Sprintf("$%d", len(args)+2)
		zone := fmt.Sprintf("$%d", len(args)+3)
		rows, err := db.QueryContext(ctx,
			`SELECT TO_CHAR(d::date, 'YYYY-MM-DD') AS day, COALESCE(SUM(o.payable_amount), 0), COUNT(o.id)
			FROM generate_series(`+firstDay+`::date::timestamp, `+lastDay+`::date::timestamp, '1 day'::interval) d
			LEFT JOIN orders o ON o.created_at >= (d AT TIME ZONE `+zone+`::text)
				AND o.created_at < ((d + '1 day'::interval) AT TIME ZONE `+zone+`::text)
				AND o.store_id IN (`+ph+`) AND `+paid+`
			GROUP BY d::date ORDER BY d::date`, append(args, start.Format(time.DateOnly), today.Format(time.DateOnly), loc)...)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		for rows.Next() {
			var day string
			var revenue, n int64
			if err := rows.Scan(&day, &revenue, &n); err != nil {
				rows.Close()
				writeInternalError(w, err)
				return
			}
			daily = append(daily, map[string]interface{}{"date": day, "revenue": revenue, "orders": n})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			writeInternalError(w, err)
			return
		}
		rows.Close()
	}

	topProducts := []map[string]interface{}{}
	if len(storeIDs) > 0 {
		ph, args := t01MergeInClause(storeIDs)
		rows, err := db.QueryContext(ctx,
			`SELECT oi.product_id, p.title, COALESCE(SUM(oi.quantity), 0), COALESCE(SUM(oi.final_unit_price * oi.quantity), 0)
			FROM order_items oi JOIN orders o ON o.id = oi.order_id JOIN products p ON p.id = oi.product_id
			WHERE o.store_id IN (`+ph+`) AND `+paid+`
			GROUP BY oi.product_id, p.title ORDER BY SUM(oi.quantity) DESC LIMIT 10`, args...)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		for rows.Next() {
			var pid, title string
			var qty, revenue int64
			if err := rows.Scan(&pid, &title, &qty, &revenue); err != nil {
				rows.Close()
				writeInternalError(w, err)
				return
			}
			topProducts = append(topProducts, map[string]interface{}{"productId": pid, "name": title, "quantity": qty, "revenue": revenue})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			writeInternalError(w, err)
			return
		}
		rows.Close()
	}

	productRows := []map[string]interface{}{}
	if len(storeIDs) > 0 {
		ph, args := t01MergeInClause(storeIDs)
		rows, err := db.QueryContext(ctx,
			`SELECT p.id, p.title, p.stock FROM store_products sp JOIN products p ON p.id = sp.product_id
			WHERE sp.store_id IN (`+ph+`) AND p.is_active GROUP BY p.id, p.title, p.stock ORDER BY p.title`, args...)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		prodOrder := []string{}
		prodTitle := map[string]string{}
		prodStock := map[string]int64{}
		for rows.Next() {
			var pid, title string
			var stock int64
			if err := rows.Scan(&pid, &title, &stock); err != nil {
				rows.Close()
				writeInternalError(w, err)
				return
			}
			prodOrder = append(prodOrder, pid)
			prodTitle[pid] = title
			prodStock[pid] = stock
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			writeInternalError(w, err)
			return
		}
		rows.Close()
		if len(prodOrder) > 0 {
			type prodAgg struct{ qty, revenue, profit int64 }
			paidAgg := map[string]prodAgg{}
			rows, err := db.QueryContext(ctx,
				`SELECT oi.product_id, COALESCE(SUM(oi.quantity), 0), COALESCE(SUM(oi.final_unit_price * oi.quantity), 0), COALESCE(SUM(oi.seller_margin * oi.quantity), 0)
				FROM order_items oi JOIN orders o ON o.id = oi.order_id
				WHERE o.store_id IN (`+ph+`) AND `+paid+`
				GROUP BY oi.product_id`, args...)
			if err != nil {
				writeInternalError(w, err)
				return
			}
			for rows.Next() {
				var pid string
				var a prodAgg
				if err := rows.Scan(&pid, &a.qty, &a.revenue, &a.profit); err != nil {
					rows.Close()
					writeInternalError(w, err)
					return
				}
				paidAgg[pid] = a
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				writeInternalError(w, err)
				return
			}
			rows.Close()
			qty30 := map[string]int64{}
			rows, err = db.QueryContext(ctx,
				`SELECT oi.product_id, COALESCE(SUM(oi.quantity), 0)
				FROM order_items oi JOIN orders o ON o.id = oi.order_id
				WHERE o.store_id IN (`+ph+`) AND `+paid+window("o.created_at", "timestamptz", len(args))+`
				GROUP BY oi.product_id`, append(args, start, end)...)
			if err != nil {
				writeInternalError(w, err)
				return
			}
			for rows.Next() {
				var pid string
				var n int64
				if err := rows.Scan(&pid, &n); err != nil {
					rows.Close()
					writeInternalError(w, err)
					return
				}
				qty30[pid] = n
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				writeInternalError(w, err)
				return
			}
			rows.Close()
			returned := map[string]int64{}
			rows, err = db.QueryContext(ctx,
				`SELECT oi.product_id, COALESCE(SUM(ri.qty), 0)
				FROM return_items ri JOIN customer_returns cr ON cr.id = ri.return_id
				JOIN order_items oi ON oi.id::text = ri.order_item_id
				JOIN orders o ON o.id = oi.order_id
				WHERE o.store_id IN (`+ph+`) AND cr.status IN ('received', 'refunded', 'completed')
				GROUP BY oi.product_id`, args...)
			if err != nil {
				writeInternalError(w, err)
				return
			}
			for rows.Next() {
				var pid string
				var n int64
				if err := rows.Scan(&pid, &n); err != nil {
					rows.Close()
					writeInternalError(w, err)
					return
				}
				returned[pid] = n
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				writeInternalError(w, err)
				return
			}
			rows.Close()
			revCount := map[string]int64{}
			revAvg := map[string]float64{}
			prodPh, prodArgs := t01MergeInClause(prodOrder)
			// ISO-D Worker 4: reviews are product-global in product_reviews
			// (no store_id column); scope them to this seller's orders via
			// product_reviews.order_id -> orders.store_id, or a product
			// shared with other stores would inflate this row's count and
			// rating with other stores' buyers' reviews. Store placeholders
			// continue after the product placeholders ($1..$k).
			revStorePh := ""
			revArgs := append([]interface{}{}, prodArgs...)
			for i, id := range storeIDs {
				if i > 0 {
					revStorePh += ", "
				}
				revStorePh += "$" + strconv.Itoa(len(prodArgs)+i+1)
				revArgs = append(revArgs, id)
			}
			rows, err = db.QueryContext(ctx,
				`SELECT pr.product_id, COUNT(*), COALESCE(AVG(pr.rating), 0) FROM product_reviews pr
				JOIN orders o ON o.id = pr.order_id
				WHERE pr.status = 'published' AND pr.product_id IN (`+prodPh+`) AND o.store_id IN (`+revStorePh+`) GROUP BY pr.product_id`, revArgs...)
			if err != nil {
				writeInternalError(w, err)
				return
			}
			for rows.Next() {
				var pid string
				var n int64
				var avgRating float64
				if err := rows.Scan(&pid, &n, &avgRating); err != nil {
					rows.Close()
					writeInternalError(w, err)
					return
				}
				revCount[pid] = n
				revAvg[pid] = avgRating
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				writeInternalError(w, err)
				return
			}
			rows.Close()
			for _, pid := range prodOrder {
				a := paidAgg[pid]
				ret := returned[pid]
				returnRate := 0.0
				if a.qty > 0 {
					returnRate = float64(ret) / float64(a.qty)
				}
				var daysLeft interface{}
				if q := qty30[pid]; q > 0 {
					daysLeft = prodStock[pid] * 30 / q
				}
				productRows = append(productRows, map[string]interface{}{
					"productId": pid, "name": prodTitle[pid],
					"quantity": a.qty, "revenue": a.revenue, "profit": a.profit,
					"returnRate": returnRate, "returnedQty": ret,
					"views": 0, "conversionRate": 0,
					"reviewsCount": revCount[pid], "rating": revAvg[pid],
					"stock": prodStock[pid], "daysLeft": daysLeft,
				})
			}
		}
	}

	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"totalRevenue":   total.revenue,
		"orderCount":     total.orders,
		"avgOrderValue":  avg(total),
		"sellerProfit":   total.profit,
		"views":          views30,
		"conversionRate": conversion,
		"periodComparison": map[string]interface{}{
			"current":  map[string]interface{}{"revenue": current.revenue, "orders": current.orders, "avgOrderValue": avg(current), "profit": current.profit},
			"previous": map[string]interface{}{"revenue": previous.revenue, "orders": previous.orders, "avgOrderValue": avg(previous), "profit": previous.profit},
		},
		"statusBreakdown": statusRows,
		"daily":           daily,
		"topProducts":     topProducts,
		"productRows":     productRows,
	})
}

// handleT01SellerSearchInsight serves GET /api/v1/seller/search-insight.
func handleT01SellerSearchInsight(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	if !t01MethodOnly(w, r, http.MethodGet) || t01DBNil(w, db) {
		return
	}
	userID := t01ClaimsUserID(r)
	if userID == "" {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	_, slugs, ok := t01MergeSellerScope(ctx, db, userID, strings.TrimSpace(r.URL.Query().Get("store_id")))
	if !ok {
		http.Error(w, `{"error":"Store not found"}`, http.StatusNotFound)
		return
	}
	bareEmpty := func() {
		t01WriteJSON(w, http.StatusOK, map[string]interface{}{
			"topSearches": []interface{}{}, "unmetInStore": []interface{}{}, "unmetCatalog": []interface{}{},
		})
	}
	if !t01TableExists(ctx, db, "search_queries") || len(slugs) == 0 {
		bareEmpty()
		return
	}
	ph, args := t01MergeInClause(slugs)
	type termAgg struct {
		term string
		n    int64
		z    int64
	}
	scanTerms := func(q string) ([]termAgg, error) {
		rows, err := db.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []termAgg{}
		for rows.Next() {
			var t termAgg
			if err := rows.Scan(&t.term, &t.n, &t.z); err != nil {
				return nil, err
			}
			out = append(out, t)
		}
		return out, rows.Err()
	}
	top, err := scanTerms(
		`SELECT term_normalized, COUNT(*),
			COALESCE(SUM(CASE WHEN result_count = 0 THEN 1 ELSE 0 END), 0)
		FROM search_queries WHERE store_slug IN (` + ph + `) AND term_normalized != ''
		GROUP BY term_normalized ORDER BY COUNT(*) DESC LIMIT 50`)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	unmet, err := scanTerms(
		`SELECT term_normalized, COUNT(*),
			COALESCE(SUM(CASE WHEN result_count = 0 THEN 1 ELSE 0 END), 0)
		FROM search_queries WHERE store_slug IN (` + ph + `) AND term_normalized != ''
		GROUP BY term_normalized HAVING SUM(CASE WHEN result_count > 0 THEN 1 ELSE 0 END) = 0
		ORDER BY COUNT(*) DESC LIMIT 50`)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	catalogZero := map[string]bool{}
	rows, err := db.QueryContext(ctx,
		`SELECT term_normalized FROM search_queries WHERE store_slug IN (`+ph+`) AND term_normalized != ''
		GROUP BY term_normalized HAVING SUM(CASE WHEN catalog_result_count IS NULL OR catalog_result_count > 0 THEN 1 ELSE 0 END) = 0`, args...)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			writeInternalError(w, err)
			return
		}
		catalogZero[t] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		writeInternalError(w, err)
		return
	}
	rows.Close()

	likeReplacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	topSearches := []map[string]interface{}{}
	for _, t := range top {
		topSearches = append(topSearches, map[string]interface{}{"term": t.term, "searches": t.n, "zeroResultSearches": t.z})
	}
	unmetInStore := []map[string]interface{}{}
	unmetCatalog := []map[string]interface{}{}
	for _, t := range unmet {
		matches := []map[string]interface{}{}
		pattern := "%" + likeReplacer.Replace(t.term) + "%"
		mrows, err := db.QueryContext(ctx,
			`SELECT id, title, image_url FROM products WHERE is_active AND title ILIKE $1 ESCAPE '\' ORDER BY title LIMIT 4`, pattern)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		for mrows.Next() {
			var pid, title string
			var img sql.NullString
			if err := mrows.Scan(&pid, &title, &img); err != nil {
				mrows.Close()
				writeInternalError(w, err)
				return
			}
			m := map[string]interface{}{"productId": pid, "title": title}
			if img.Valid && img.String != "" {
				m["imageUrl"] = img.String
			}
			matches = append(matches, m)
		}
		if err := mrows.Err(); err != nil {
			mrows.Close()
			writeInternalError(w, err)
			return
		}
		mrows.Close()
		unmetInStore = append(unmetInStore, map[string]interface{}{
			"term": t.term, "searches": t.n, "zeroResultSearches": t.z, "catalogMatches": matches,
		})
		if catalogZero[t.term] {
			unmetCatalog = append(unmetCatalog, map[string]interface{}{
				"term": t.term, "searches": t.n, "zeroResultSearches": t.z,
			})
		}
	}

	t01WriteJSON(w, http.StatusOK, map[string]interface{}{
		"topSearches":  topSearches,
		"unmetInStore": unmetInStore,
		"unmetCatalog": unmetCatalog,
	})
}
