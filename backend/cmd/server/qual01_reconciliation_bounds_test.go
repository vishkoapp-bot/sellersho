package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

// QUAL-01 G4: bounded review-evidence selection/pagination (G4a), bounded
// full-report/result payloads (G4b), and durable replay for a retry after a
// different decision (G4c).

func TestQual01HistoricalReconciliationReportPagination(t *testing.T) {
	db := opsHTTPDB(t)
	suffix := uuid.NewString()[:12]
	fixtures := []string{
		"qual-page-01-" + suffix,
		"qual-page-02-" + suffix,
		"qual-page-03-" + suffix,
	}
	for _, product := range fixtures {
		if _, err := db.Exec(`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,fulfillment_method) VALUES ($1,'Pagination fixture',$1,'digital','sup-technolife',100000,50000,1000,TRUE,1,'warehouse')`, product); err != nil {
			t.Fatal(err)
		}
	}
	// One fixture carries 60 movements so its nested evidence is bounded.
	for i := 0; i < 60; i++ {
		if _, err := db.Exec(`INSERT INTO inventory_movements (id,product_id,supplier_id,quantity,to_status,to_location_key,reference_type,reference_id,idempotency_key) VALUES ($1,$2,'sup-technolife',1,'sellable','qual01-location','test',$3,$4)`, fmt.Sprintf("qual-mov-%s-%02d", suffix, i), fixtures[0], fmt.Sprintf("movement-reference-%02d", i), "retry-qual-mov-"+suffix+fmt.Sprintf("-%02d", i)); err != nil {
			t.Fatal(err)
		}
	}
	repo := repository.NewSupplierRepository(db)
	service := auth.NewJWTService(uuid.NewString())
	reader := &middlewareCapabilityReader{}
	service.SetCapabilityReader(reader)
	handler := requireGlobalWarehouseRoles(service, "logistics_specialist", "admin")(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		handleGetHistoricalInventoryReport(w, r, r.Context(), repo)
	})
	get := func(query string, want int) map[string]json.RawMessage {
		t.Helper()
		reader.capabilities = []string{"logistics_specialist"}
		token, err := service.GenerateTokenWithCapabilities("fixture-user", "", "", "logistics_specialist", []string{"logistics_specialist"}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodGet, "/api/v1/warehouse/inventory/reconciliation-report"+query, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != want {
			t.Fatalf("GET %s: got %d, want %d: %s", query, w.Code, want, w.Body)
		}
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("GET %s: bad envelope: %v", query, err)
		}
		return envelope
	}
	rowsOf := func(envelope map[string]json.RawMessage) []map[string]any {
		t.Helper()
		var rows []map[string]any
		if err := json.Unmarshal(envelope["rows"], &rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	idsOf := func(rows []map[string]any) []string {
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row["productId"].(string))
		}
		return ids
	}

	// Invalid page/limit are rejected, oversized limit is clamped.
	get("?page=0", http.StatusBadRequest)
	get("?page=abc", http.StatusBadRequest)
	get("?page=-1", http.StatusBadRequest)
	get("?limit=0", http.StatusBadRequest)
	get("?limit=abc", http.StatusBadRequest)
	clamped := get("?limit=1000", http.StatusOK)
	if string(clamped["limit"]) != "200" {
		t.Fatalf("oversized limit was not clamped to 200: %s", clamped["limit"])
	}

	// Default fetch is bounded and reports the total.
	def := get("", http.StatusOK)
	if string(def["page"]) != "1" || string(def["limit"]) != "200" {
		t.Fatalf("default page/limit not 1/200: %s/%s", def["page"], def["limit"])
	}
	var total int
	if err := json.Unmarshal(def["total"], &total); err != nil || total < len(fixtures) {
		t.Fatalf("total not reported: %s (%v)", def["total"], err)
	}
	defRows := rowsOf(def)
	if len(defRows) != total && total <= 200 {
		t.Fatalf("default fetch returned %d rows for total %d", len(defRows), total)
	}
	if !slices.IsSorted(idsOf(defRows)) {
		t.Fatalf("default fetch is not deterministically ordered by product ID")
	}

	// Walk pages of 1: every row appears exactly once, pages are stable.
	seen := map[string]bool{}
	pages := 0
	for page := 1; ; page++ {
		env := get(fmt.Sprintf("?page=%d&limit=1", page), http.StatusOK)
		var p, l, tot int
		if err := json.Unmarshal(env["page"], &p); err != nil || p != page {
			t.Fatalf("page %d echoed as %s", page, env["page"])
		}
		if err := json.Unmarshal(env["limit"], &l); err != nil || l != 1 {
			t.Fatalf("page %d limit echoed as %s", page, env["limit"])
		}
		if err := json.Unmarshal(env["total"], &tot); err != nil || tot != total {
			t.Fatalf("page %d total %s, want %d", page, env["total"], total)
		}
		rows := rowsOf(env)
		if len(rows) == 0 {
			break
		}
		if len(rows) != 1 {
			t.Fatalf("page %d returned %d rows, want 1", page, len(rows))
		}
		id := rows[0]["productId"].(string)
		if seen[id] {
			t.Fatalf("product %s repeated across pages", id)
		}
		seen[id] = true
		pages++
		if pages > total+2 {
			t.Fatalf("page walk did not terminate (total %d)", total)
		}
	}
	if pages != total {
		t.Fatalf("page walk saw %d rows, total %d", pages, total)
	}
	for _, product := range fixtures {
		if !seen[product] {
			t.Fatalf("pagination walk missed fixture %s", product)
		}
	}
	// A repeated page fetch is byte-stable.
	first := get("?page=2&limit=1", http.StatusOK)
	second := get("?page=2&limit=1", http.StatusOK)
	if string(first["rows"]) != string(second["rows"]) {
		t.Fatalf("repeated page fetch is not stable")
	}

	// Nested evidence is bounded per row with an explicit signal.
	var found map[string]any
	for _, row := range defRows {
		if row["productId"] == fixtures[0] {
			found = row
		}
	}
	if found == nil {
		// The fixture may sit past the default page only if total > 200;
		// fetch it directly by walking pages of 1 instead.
		for page := 1; page <= total; page++ {
			for _, row := range rowsOf(get(fmt.Sprintf("?page=%d&limit=1", page), http.StatusOK)) {
				if row["productId"] == fixtures[0] {
					found = row
				}
			}
		}
	}
	if found == nil {
		t.Fatalf("nested-evidence fixture %s missing from report", fixtures[0])
	}
	movements, _ := found["movements"].([]any)
	evidenceIDs, _ := found["evidenceIds"].([]any)
	if len(movements) != repository.MaxHistoricalNestedEvidence {
		t.Fatalf("movements not capped at %d: got %d", repository.MaxHistoricalNestedEvidence, len(movements))
	}
	if found["evidenceTruncated"] != true || found["evidenceTotal"] != float64(60) {
		t.Fatalf("nested bound not signalled: truncated=%v total=%v", found["evidenceTruncated"], found["evidenceTotal"])
	}
	if len(evidenceIDs) > repository.MaxHistoricalNestedEvidence {
		t.Fatalf("evidenceIds selection not capped: got %d", len(evidenceIDs))
	}
}

func TestQual01HistoricalReconciliationReviewEvidenceBounds(t *testing.T) {
	db := opsHTTPDB(t)
	product := "qual-evidence-" + uuid.NewString()[:12]
	if _, err := db.Exec(`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,fulfillment_method) VALUES ($1,'Evidence fixture',$1,'digital','sup-technolife',100000,50000,1000,TRUE,2,'warehouse')`, product); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewSupplierRepository(db)
	service := auth.NewJWTService(uuid.NewString())
	reader := &middlewareCapabilityReader{}
	service.SetCapabilityReader(reader)
	handler := requireGlobalWarehouseRoles(service, "logistics_specialist", "admin")(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		handleReviewHistoricalInventory(w, r, r.Context(), repo, nil)
	})
	counts := func() (reviews, audit int) {
		t.Helper()
		if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM historical_reconciliation_reviews WHERE product_id=$1), (SELECT COUNT(*) FROM audit_logs WHERE action='historical_reconciliation_review' AND details LIKE '%'||$1||'%')`, product).Scan(&reviews, &audit); err != nil {
			t.Fatal(err)
		}
		return reviews, audit
	}
	post := func(body string, want int) string {
		t.Helper()
		reader.capabilities = []string{"logistics_specialist"}
		token, err := service.GenerateTokenWithCapabilities("fixture-reviewer", "", "", "logistics_specialist", []string{"logistics_specialist"}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/api/v1/warehouse/inventory/reconciliation-reviews/"+product, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != want {
			t.Fatalf("POST %s: got %d, want %d: %s", body, w.Code, want, w.Body)
		}
		return w.Body.String()
	}

	// 51 evidence entries are rejected, not truncated; nothing is written.
	oversized := make([]string, 0, repository.MaxHistoricalReviewEvidence+1)
	for i := 0; i < repository.MaxHistoricalReviewEvidence+1; i++ {
		oversized = append(oversized, fmt.Sprintf("evidence-%d", i))
	}
	oversizedJSON, _ := json.Marshal(oversized)
	post(fmt.Sprintf(`{"status":"in_review","reason":"too much evidence","evidence":%s}`, oversizedJSON), http.StatusBadRequest)
	if reviews, audit := counts(); reviews != 0 || audit != 0 {
		t.Fatalf("oversized evidence wrote review/audit: %d/%d", reviews, audit)
	}
	// One over-long entry is rejected as well.
	post(fmt.Sprintf(`{"status":"in_review","reason":"long entry","evidence":["%s"]}`, strings.Repeat("x", repository.MaxHistoricalEvidenceEntryLength+1)), http.StatusBadRequest)
	if reviews, audit := counts(); reviews != 0 || audit != 0 {
		t.Fatalf("over-long evidence entry wrote review/audit: %d/%d", reviews, audit)
	}
	// An empty entry is rejected as well.
	post(`{"status":"in_review","reason":"empty entry","evidence":["  "]}`, http.StatusBadRequest)
	if reviews, audit := counts(); reviews != 0 || audit != 0 {
		t.Fatalf("empty evidence entry wrote review/audit: %d/%d", reviews, audit)
	}
	// Exactly the cap is accepted.
	atCap := make([]string, 0, repository.MaxHistoricalReviewEvidence)
	for i := 0; i < repository.MaxHistoricalReviewEvidence; i++ {
		atCap = append(atCap, fmt.Sprintf("evidence-%d", i))
	}
	atCapJSON, _ := json.Marshal(atCap)
	result := post(fmt.Sprintf(`{"status":"in_review","reason":"at cap","evidence":%s}`, atCapJSON), http.StatusOK)
	var saved repository.HistoricalReview
	if err := json.Unmarshal([]byte(result), &saved); err != nil || len(saved.EvidenceUsed) != repository.MaxHistoricalReviewEvidence {
		t.Fatalf("at-cap evidence not stored: %s (%v)", result, err)
	}
}

func TestQual01HistoricalReviewRetryAfterDifferentDecision(t *testing.T) {
	db := opsHTTPDB(t)
	product := "qual-replay-" + uuid.NewString()[:12]
	if _, err := db.Exec(`INSERT INTO products (id,title,slug,category_slug,supplier_id,original_price,supplier_base_cost,platform_margin,is_original,stock,fulfillment_method) VALUES ($1,'Replay fixture',$1,'digital','sup-technolife',100000,50000,1000,TRUE,2,'warehouse')`, product); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewSupplierRepository(db)
	service := auth.NewJWTService(uuid.NewString())
	reader := &middlewareCapabilityReader{}
	service.SetCapabilityReader(reader)
	handler := requireGlobalWarehouseRoles(service, "logistics_specialist", "admin")(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		handleReviewHistoricalInventory(w, r, r.Context(), repo, nil)
	})
	post := func(body string) repository.HistoricalReview {
		t.Helper()
		reader.capabilities = []string{"logistics_specialist"}
		token, err := service.GenerateTokenWithCapabilities("fixture-reviewer", "", "", "logistics_specialist", []string{"logistics_specialist"}, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/api/v1/warehouse/inventory/reconciliation-reviews/"+product, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("POST %s: got %d, want 200: %s", body, w.Code, w.Body)
		}
		var saved repository.HistoricalReview
		if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
			t.Fatalf("bad review response: %s (%v)", w.Body, err)
		}
		return saved
	}
	counts := func() (reviews, history, audit int) {
		t.Helper()
		if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM historical_reconciliation_reviews WHERE product_id=$1), (SELECT COUNT(*) FROM historical_reconciliation_review_decisions WHERE product_id=$1), (SELECT COUNT(*) FROM audit_logs WHERE action='historical_reconciliation_review' AND details LIKE '%'||$1||'%')`, product).Scan(&reviews, &history, &audit); err != nil {
			t.Fatal(err)
		}
		return reviews, history, audit
	}
	currentStatus := func() string {
		t.Helper()
		var status string
		if err := db.QueryRow(`SELECT review_status FROM historical_reconciliation_reviews WHERE product_id=$1`, product).Scan(&status); err != nil {
			t.Fatal(err)
		}
		return status
	}

	first := post(`{"status":"in_review","reason":"first decision","evidence":["evidence-a"]}`)
	if first.ReviewStatus != "in_review" || first.DecisionReason != "first decision" {
		t.Fatalf("unexpected first decision: %+v", first)
	}
	second := post(`{"status":"awaiting_evidence","reason":"second decision","evidence":["evidence-b"]}`)
	if second.ID != first.ID || second.ReviewStatus != "awaiting_evidence" {
		t.Fatalf("changed decision must update the same current review: first=%+v second=%+v", first, second)
	}
	if reviews, history, audit := counts(); reviews != 1 || history != 2 || audit != 2 {
		t.Fatalf("after two decisions want 1 current / 2 history / 2 audit, got %d/%d/%d", reviews, history, audit)
	}

	// Retry of the superseded first decision replays its own durable entry:
	// same status/reason/evidence and original timestamps, a history ID —
	// and the current row stays on the second decision with no new audit.
	replay := post(`{"status":"in_review","reason":"first decision","evidence":["evidence-a"]}`)
	if replay.ReviewStatus != "in_review" || replay.DecisionReason != "first decision" || len(replay.EvidenceUsed) != 1 || replay.EvidenceUsed[0] != "evidence-a" {
		t.Fatalf("superseded replay did not return the first decision: %+v", replay)
	}
	if replay.ID == first.ID || !strings.HasPrefix(replay.ID, "HREV-D-") {
		t.Fatalf("superseded replay must carry its own durable history ID, got %s (current %s)", replay.ID, first.ID)
	}
	if replay.ReviewedAt == nil || first.ReviewedAt == nil || !replay.ReviewedAt.Equal(*first.ReviewedAt) {
		t.Fatalf("superseded replay must keep the first decision's timestamps: %v vs %v", replay.ReviewedAt, first.ReviewedAt)
	}
	if got := currentStatus(); got != "awaiting_evidence" {
		t.Fatalf("superseded retry flipped the current review to %s", got)
	}
	if reviews, history, audit := counts(); reviews != 1 || history != 2 || audit != 2 {
		t.Fatalf("superseded replay wrote state: %d current / %d history / %d audit", reviews, history, audit)
	}

	// Repeating the superseded retry returns the identical durable record.
	again := post(`{"status":"in_review","reason":"first decision","evidence":["evidence-a"]}`)
	if again.ID != replay.ID || again.ReviewedAt == nil || replay.ReviewedAt == nil || !again.ReviewedAt.Equal(*replay.ReviewedAt) {
		t.Fatalf("repeated superseded replay not identical: %+v vs %+v", again, replay)
	}
	if reviews, history, audit := counts(); reviews != 1 || history != 2 || audit != 2 {
		t.Fatalf("repeated superseded replay wrote state: %d/%d/%d", reviews, history, audit)
	}

	// An exact replay of the current decision still returns the current row.
	current := post(`{"status":"awaiting_evidence","reason":"second decision","evidence":["evidence-b"]}`)
	if current.ID != second.ID || current.ReviewStatus != "awaiting_evidence" {
		t.Fatalf("current replay did not return the current review: %+v", current)
	}
	if reviews, history, audit := counts(); reviews != 1 || history != 2 || audit != 2 {
		t.Fatalf("current replay wrote state: %d/%d/%d", reviews, history, audit)
	}
}
