package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/drup/platform/internal/catalogpage"
	"github.com/lib/pq"
)

var ErrCatalogPriceIndexRequired = errors.New("authoritative catalog price index required for price sorting or filtering")

type CatalogCandidatePage struct {
	Keys    []catalogpage.Key
	HasMore bool
}

// GetProductCandidatePageTx is an unwired source-key primitive for the catalog
// index/paging implementation. It selects N+1 lightweight keys in SQL and returns
// at most N, without loading whole assortments or pricing any products. It is
// NOT the final public page: revision/index validation and priced eligibility
// must join this selection before public adoption. Price work fails explicitly
// rather than sorting by protected supplier costs or filtering after LIMIT.
func (r *CatalogRepository) GetProductCandidatePageTx(ctx context.Context, tx *sql.Tx, scope catalogpage.Scope, filter catalogpage.Filter, size int, after *catalogpage.Key) (CatalogCandidatePage, error) {
	page := CatalogCandidatePage{Keys: []catalogpage.Key{}}
	if tx == nil || size < 1 || size > catalogpage.MaxSize {
		return page, catalogpage.ErrInvalid
	}
	if err := catalogpage.Validate(scope, filter, after); err != nil {
		return page, err
	}
	if filter.Sort == "" {
		filter.Sort = "featured"
	}
	if filter.Sort == "cheapest" || filter.Sort == "expensive" || filter.Sort == "discount" || filter.MinPrice != nil || filter.MaxPrice != nil {
		return page, ErrCatalogPriceIndexRequired
	}
	scoped := scope.OwnerType == "dropshipper"
	if filter.StorePosition != (scoped && filter.Sort == "featured") {
		return page, catalogpage.ErrInvalid
	}
	args := []any{}
	arg := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
	// This SQL gate independently checks ID/type; a supplied Scope cannot promote
	// a dropshipper to central. Authoritative price revision checks remain a gate.
	searching := filter.Search != ""
	if searching {
		setSearchJITOff(ctx, tx)
	}
	hasCategories := false
	if searching {
		hasCategories = categoriesTableExistsTx(ctx, tx)
	}
	from := ` FROM products p JOIN stores scope_store ON scope_store.id=` + arg(scope.ID) + ` AND scope_store.owner_type=` + arg(scope.OwnerType)
	position := `0`
	if scoped {
		from += ` JOIN store_products sp ON sp.store_id=scope_store.id AND sp.product_id=p.id`
		position = `sp.position`
	}
	where := []string{notSuspendedSupplierClause}
	if !scoped {
		where = append(where, `p.is_active=TRUE`)
	}
	if filter.Category != "" && filter.Category != "all" {
		where = append(where, `p.category_slug=`+arg(filter.Category))
	}
	if filter.Niche != "" && filter.Niche != "all" {
		niche := arg(filter.Niche)
		where = append(where, `EXISTS(SELECT 1 FROM product_niches pn JOIN niches n ON n.id=pn.niche_id WHERE pn.product_id=p.id AND (n.slug=`+niche+` OR n.id=`+niche+`))`)
	}
	if len(filter.Brands) > 0 {
		where = append(where, `p.brand=ANY(`+arg(pq.Array(filter.Brands))+`::text[])`)
	}
	if filter.IsOriginal != nil {
		where = append(where, `p.is_original=`+arg(*filter.IsOriginal))
	}
	if filter.InStockOnly {
		where = append(where, `p.stock>0`)
	}
	rankExpr := ""
	if searching {
		searchWhere, searchRank := buildSearchCondition(filter.Search, arg, hasCategories)
		where = append(where, searchWhere)
		rankExpr = searchRank
	}
	attributeIDs := make([]int, 0, len(filter.Attributes))
	for id := range filter.Attributes {
		attributeIDs = append(attributeIDs, id)
	}
	sort.Ints(attributeIDs)
	for _, id := range attributeIDs {
		values := filter.Attributes[id]
		if len(values) == 0 {
			continue
		}
		ids := make([]int64, len(values))
		for i, value := range values {
			ids[i] = int64(value)
		}
		where = append(where, `EXISTS(SELECT 1 FROM product_attribute_values av WHERE av.product_id=p.id AND av.dk_attribute_id=`+arg(id)+` AND av.dk_value_id=ANY(`+arg(pq.Array(ids))+`::bigint[]))`)
	}
	order := `COALESCE(p.is_featured,FALSE) DESC,p.created_at DESC,p.id DESC`
	if filter.StorePosition {
		order = position + ` ASC,p.id ASC`
	} else if filter.Sort == "newest" {
		order = `p.created_at DESC,p.id DESC`
	}
	// Batch 2, A1: rank leads on search pages (see GetPricedProductPageTx).
	selectRank := ""
	if searching {
		order = rankExpr + ` ASC,` + order
		selectRank = `,` + rankExpr
	}
	if after != nil {
		var afterPred string
		switch {
		case filter.StorePosition:
			afterPred = `(` + position + `,p.id)>(` + arg(after.Position) + `,` + arg(after.ID) + `)`
		case filter.Sort == "newest":
			afterPred = `(p.created_at,p.id)<(` + arg(after.CreatedAt) + `,` + arg(after.ID) + `)`
		default:
			afterPred = `(COALESCE(p.is_featured,FALSE),p.created_at,p.id)<(` + arg(after.Featured) + `,` + arg(after.CreatedAt) + `,` + arg(after.ID) + `)`
		}
		if searching {
			rankArg := arg(after.Rank)
			where = append(where, `(`+rankExpr+` > `+rankArg+` OR (`+rankExpr+` = `+rankArg+` AND `+afterPred+`))`)
		} else {
			where = append(where, afterPred)
		}
	}
	query := `SELECT p.id,COALESCE(p.is_featured,FALSE),p.created_at,` + position + selectRank + from + ` WHERE ` + strings.Join(where, ` AND `) + ` ORDER BY ` + order + ` LIMIT ` + arg(size+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var key catalogpage.Key
		var featured bool
		var created time.Time
		var pos int64
		var rank int
		if err := scanRow(rows, searching, &key.ID, &featured, &created, &pos, &rank); err != nil {
			return page, err
		}
		if filter.StorePosition {
			key.Position = pos
		} else {
			key.CreatedAt = created.UTC()
			if filter.Sort == "featured" {
				key.Featured = featured
			}
		}
		key.Rank = rank
		page.Keys = append(page.Keys, key)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	page.HasMore = len(page.Keys) > size
	if page.HasMore {
		page.Keys = page.Keys[:size]
	}
	return page, nil
}
