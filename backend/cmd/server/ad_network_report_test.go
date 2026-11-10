package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/repository"
)

// Temp tables are connection-local: these report tests never change the
// public fixtures used by other packages, or any production data.
func adReportDB(t *testing.T) *sql.DB {
	t.Helper()
	db := opsHTTPDB(t)
	db.SetMaxOpenConns(1)
	_, err := db.Exec(`CREATE TEMP TABLE stores(id VARCHAR(50) PRIMARY KEY,name TEXT);
CREATE TEMP TABLE seller_campaigns(id UUID PRIMARY KEY,store_id VARCHAR(50),name TEXT);
CREATE TEMP TABLE seller_campaign_events(event_type TEXT NOT NULL,occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
CREATE TEMP TABLE ad_event_rejections(id BIGSERIAL PRIMARY KEY,campaign_id TEXT NOT NULL,event_type TEXT NOT NULL,reason TEXT NOT NULL,ip_address TEXT NOT NULL DEFAULT 'private-ip',token_prefix TEXT NOT NULL DEFAULT 'private-token',occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW());`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func adReportRequest(t *testing.T, db *sql.DB, days string, role string) *httptest.ResponseRecorder {
	t.Helper()
	service := auth.NewJWTService(strings.Repeat("r", 32))
	handler := RequireRoles(service, "admin", "support_specialist")(func(w http.ResponseWriter, r *http.Request) {
		handleAdNetworkReport(w, r, repository.NewStoreRepository(db))
	})
	r := httptest.NewRequest("GET", "/api/v1/admin/ad-network/report"+days, nil)
	if role != "" {
		token, err := service.GenerateToken("report-test", "09123456789", "Report test", role, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}
func decodeAdReport(t *testing.T, w *httptest.ResponseRecorder) repository.AdNetworkReport {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var report repository.AdNetworkReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	return report
}
func TestAdNetworkReportEmptyAuthorizationAndErrors(t *testing.T) {
	db := adReportDB(t)
	for _, tc := range []struct {
		role string
		want int
	}{{"", 401}, {"seller", 403}, {"supplier", 403}, {"logistics_specialist", 403}, {"admin", 200}, {"support_specialist", 200}} {
		w := adReportRequest(t, db, "", tc.role)
		if w.Code != tc.want {
			t.Fatalf("role=%s status=%d", tc.role, w.Code)
		}
		if w.Code == 200 {
			r := decodeAdReport(t, w)
			if r.WindowDays != 7 || r.Summary.TotalRejectedEvents != 0 || r.Summary.AcceptedClicks != 0 || r.Reasons == nil || r.Incidents == nil {
				t.Fatalf("empty report=%+v", r)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("report cache must be disabled")
			}
		}
	}
	for _, query := range []string{"?days=1", "?days=-7", "?days=abc", "?days=99999"} {
		if w := adReportRequest(t, db, query, "admin"); w.Code != 400 {
			t.Fatal(query, w.Code)
		}
	}
	w := httptest.NewRecorder()
	handleAdNetworkReport(w, httptest.NewRequest("POST", "/", nil), nil)
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	handleAdNetworkReport(w, httptest.NewRequest("GET", "/", nil), nil)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	db.Close()
	if w := adReportRequest(t, db, "", "admin"); w.Code != 500 {
		t.Fatalf("database failure must not become empty success: %d", w.Code)
	}
}
func TestAdNetworkReportCountsWindowsOrphansAndLogLimit(t *testing.T) {
	db := adReportDB(t)
	_, err := db.Exec(`INSERT INTO stores VALUES('report-store','Actual store');
INSERT INTO seller_campaigns VALUES('10000000-0000-0000-0000-000000000001','report-store','Actual campaign');
INSERT INTO seller_campaign_events(event_type,occurred_at) VALUES('click',NOW()-INTERVAL '1 day'),('impression',NOW()-INTERVAL '6 days'),('click',NOW()-INTERVAL '20 days'),('impression',NOW()-INTERVAL '70 days'),('click',NOW()-INTERVAL '100 days'),('click',NOW()+INTERVAL '1 day');
INSERT INTO ad_event_rejections(campaign_id,event_type,reason,occurred_at) VALUES
('10000000-0000-0000-0000-000000000001','click','duplicate_click',NOW()-INTERVAL '1 day'),
('10000000-0000-0000-0000-000000000001','click','duplicate_click',NOW()-INTERVAL '1 day'),
('10000000-0000-0000-0000-000000000001','impression','duplicate_click',NOW()-INTERVAL '2 days'),
('missing-or-invalid-legacy-id','challenge','bot_detected',NOW()-INTERVAL '1 day'),
('missing-or-invalid-legacy-id','legacy','unknown_reason',NOW()-INTERVAL '1 day'),
('10000000-0000-0000-0000-000000000001','click','too_fast',NOW()-INTERVAL '20 days'),
('10000000-0000-0000-0000-000000000001','click','too_fast',NOW()-INTERVAL '100 days'),
('10000000-0000-0000-0000-000000000001','click','too_fast',NOW()+INTERVAL '1 day');`)
	if err != nil {
		t.Fatal(err)
	}
	w := adReportRequest(t, db, "?days=7", "admin")
	r := decodeAdReport(t, w)
	s := r.Summary
	if s.AcceptedClicks != 1 || s.AcceptedImpressions != 1 || s.RejectedClicks != 2 || s.RejectedImpressions != 1 || s.RejectedChallenges != 1 || s.OtherRejectedEvents != 1 || s.TotalRejectedEvents != 5 {
		t.Fatalf("wrong summary %+v", s)
	}
	if len(r.Incidents) != 5 || len(r.Reasons) != 4 || r.Reasons[0].Count != 2 {
		t.Fatalf("wrong logs/reasons %+v", r)
	}
	known, orphan := false, false
	for _, item := range r.Incidents {
		if item.CampaignID == "missing-or-invalid-legacy-id" {
			orphan = true
			if item.CampaignName != nil || item.StoreName != nil {
				t.Fatal("invented store")
			}
		} else {
			known = true
			if item.CampaignName == nil || *item.CampaignName != "Actual campaign" || item.StoreName == nil || *item.StoreName != "Actual store" {
				t.Fatalf("lost store/campaign %+v", item)
			}
		}
	}
	if !known || !orphan {
		t.Fatal("missing incident")
	}
	for _, private := range []string{"private-ip", "private-token", "ip_address", "token_prefix"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatalf("leaked %s", private)
		}
	}
	r = decodeAdReport(t, adReportRequest(t, db, "?days=30", "admin"))
	if r.Summary.AcceptedClicks != 2 || r.Summary.AcceptedImpressions != 1 || r.Summary.RejectedClicks != 3 || r.Summary.TotalRejectedEvents != 6 {
		t.Fatalf("30-day summary %+v", r.Summary)
	}
	r = decodeAdReport(t, adReportRequest(t, db, "?days=90", "admin"))
	if r.Summary.AcceptedImpressions != 2 || r.Summary.TotalRejectedEvents != 6 {
		t.Fatalf("90-day summary %+v", r.Summary)
	}
	_, err = db.Exec(`INSERT INTO ad_event_rejections(campaign_id,event_type,reason,occurred_at) SELECT 'missing','click','too_fast',NOW()-INTERVAL '1 minute' FROM generate_series(1,60)`)
	if err != nil {
		t.Fatal(err)
	}
	r = decodeAdReport(t, adReportRequest(t, db, "?days=7", "admin"))
	if len(r.Incidents) != 50 || r.Limit != 50 || r.Summary.TotalRejectedEvents != 65 {
		t.Fatalf("limit/total mismatch %+v", r)
	}
	previous := int64(1 << 62)
	for _, item := range r.Incidents {
		id, err := strconv.ParseInt(item.ID, 10, 64)
		if err != nil || id >= previous {
			t.Fatal("unstable log order")
		}
		previous = id
	}
}

// Production predates the reconstruction migration: both rejection IDs and
// campaign IDs are UUIDs there, while a fresh install uses BIGSERIAL/TEXT.
func TestAdNetworkReportUUIDRejectionSchema(t *testing.T) {
	db := adReportDB(t)
	_, err := db.Exec(`ALTER TABLE ad_event_rejections ALTER COLUMN id DROP DEFAULT;
ALTER TABLE ad_event_rejections ALTER COLUMN id TYPE UUID USING gen_random_uuid();
ALTER TABLE ad_event_rejections ALTER COLUMN campaign_id TYPE UUID USING campaign_id::uuid;
INSERT INTO stores VALUES('uuid-report-store','UUID store');
INSERT INTO seller_campaigns VALUES('20000000-0000-0000-0000-000000000001','uuid-report-store','UUID campaign');
INSERT INTO ad_event_rejections(id,campaign_id,event_type,reason) VALUES
('30000000-0000-0000-0000-000000000001','20000000-0000-0000-0000-000000000001','click','bot_detected'),
('30000000-0000-0000-0000-000000000002','20000000-0000-0000-0000-000000000099','challenge','bot_detected');`)
	if err != nil {
		t.Fatal(err)
	}
	r := decodeAdReport(t, adReportRequest(t, db, "?days=7", "admin"))
	if r.Summary.TotalRejectedEvents != 2 || len(r.Incidents) != 2 {
		t.Fatalf("wrong UUID report %+v", r)
	}
	known, orphan := false, false
	for _, item := range r.Incidents {
		if item.CampaignName != nil {
			known = true
			if *item.CampaignName != "UUID campaign" || item.StoreName == nil || *item.StoreName != "UUID store" {
				t.Fatal("lost UUID relationship")
			}
		} else {
			orphan = true
		}
	}
	if !known || !orphan {
		t.Fatal("UUID joins must keep both known and unknown campaigns")
	}
}
