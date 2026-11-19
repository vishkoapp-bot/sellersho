package repository_test

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/drup/platform/internal/repository"
)

// Freeze the report's wire allowlist independently of the database fixture.
// Exact keys catch unexpected fields even when their values are empty or null.
func TestQUAL01AdNetworkReportJSONProjection(t *testing.T) {
	stamp := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	campaign, store := "Fixture campaign", "Fixture store"
	report := repository.AdNetworkReport{
		WindowDays: 7, From: stamp.AddDate(0, 0, -7), To: stamp, Limit: 50,
		Summary: repository.AdNetworkSummary{RejectedClicks: 1, TotalRejectedEvents: 1},
		Reasons: []repository.AdRejectionReason{{Reason: "duplicate_click", EventType: "click", Count: 1}},
		Incidents: []repository.AdRejectionIncident{{
			ID: "1", CampaignID: "fixture-campaign", CampaignName: &campaign, StoreName: &store,
			EventType: "click", Reason: "duplicate_click", OccurredAt: stamp,
		}},
	}
	body, err := json.Marshal(&report)
	if err != nil {
		t.Fatal(err)
	}

	keys := func(label string, raw json.RawMessage, want ...string) map[string]json.RawMessage {
		t.Helper()
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		got := make([]string, 0, len(object))
		for key := range object {
			got = append(got, key)
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Fatalf("%s JSON keys = %v, want %v", label, got, want)
		}
		return object
	}
	oneRow := func(label string, raw json.RawMessage) json.RawMessage {
		t.Helper()
		var rows []json.RawMessage
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if len(rows) != 1 {
			t.Fatalf("%s has %d rows, want 1", label, len(rows))
		}
		return rows[0]
	}

	root := keys("report", body, "windowDays", "from", "to", "summary", "reasons", "incidents", "limit")
	keys("summary", root["summary"], "acceptedClicks", "acceptedImpressions", "rejectedClicks", "rejectedImpressions", "rejectedChallenges", "otherRejectedEvents", "totalRejectedEvents")
	keys("reason", oneRow("reasons", root["reasons"]), "reason", "eventType", "count")
	keys("incident", oneRow("incidents", root["incidents"]), "id", "campaignId", "campaignName", "storeName", "eventType", "reason", "occurredAt")
}
