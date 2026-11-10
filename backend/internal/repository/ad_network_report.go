package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrAdReportWindow = errors.New("report window must be 7, 30 or 90 days")

type AdNetworkSummary struct {
	AcceptedClicks      int64 `json:"acceptedClicks"`
	AcceptedImpressions int64 `json:"acceptedImpressions"`
	RejectedClicks      int64 `json:"rejectedClicks"`
	RejectedImpressions int64 `json:"rejectedImpressions"`
	RejectedChallenges  int64 `json:"rejectedChallenges"`
	OtherRejectedEvents int64 `json:"otherRejectedEvents"`
	TotalRejectedEvents int64 `json:"totalRejectedEvents"`
}
type AdRejectionReason struct {
	Reason    string `json:"reason"`
	EventType string `json:"eventType"`
	Count     int64  `json:"count"`
}
type AdRejectionIncident struct {
	ID           string    `json:"id"`
	CampaignID   string    `json:"campaignId"`
	CampaignName *string   `json:"campaignName"`
	StoreName    *string   `json:"storeName"`
	EventType    string    `json:"eventType"`
	Reason       string    `json:"reason"`
	OccurredAt   time.Time `json:"occurredAt"`
}
type AdNetworkReport struct {
	WindowDays int                   `json:"windowDays"`
	From       time.Time             `json:"from"`
	To         time.Time             `json:"to"`
	Summary    AdNetworkSummary      `json:"summary"`
	Reasons    []AdRejectionReason   `json:"reasons"`
	Incidents  []AdRejectionIncident `json:"incidents"`
	Limit      int                   `json:"limit"`
}

// All aggregates and rows share one read-only snapshot and time window.
// Rejections are recorded attempts, not proof of fraud or financial charges.
// Never return raw IPs, fingerprint hashes or advertising tokens to this view.
func (r *StoreRepository) GetAdNetworkReport(ctx context.Context, days int) (*AdNetworkReport, error) {
	if days != 7 && days != 30 && days != 90 {
		return nil, ErrAdReportWindow
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := &AdNetworkReport{WindowDays: days, Limit: 50, Reasons: []AdRejectionReason{}, Incidents: []AdRejectionIncident{}}
	if err = tx.QueryRowContext(ctx, `SELECT NOW()-make_interval(days=>$1),NOW()`, days).Scan(&out.From, &out.To); err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FILTER(WHERE event_type='click'),COUNT(*) FILTER(WHERE event_type='impression') FROM seller_campaign_events WHERE occurred_at>=$1 AND occurred_at<=$2`, out.From, out.To).Scan(&out.Summary.AcceptedClicks, &out.Summary.AcceptedImpressions); err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FILTER(WHERE event_type='click'),COUNT(*) FILTER(WHERE event_type='impression'),COUNT(*) FILTER(WHERE event_type='challenge'),COUNT(*) FILTER(WHERE event_type NOT IN ('click','impression','challenge')),COUNT(*) FROM ad_event_rejections WHERE occurred_at>=$1 AND occurred_at<=$2`, out.From, out.To).Scan(&out.Summary.RejectedClicks, &out.Summary.RejectedImpressions, &out.Summary.RejectedChallenges, &out.Summary.OtherRejectedEvents, &out.Summary.TotalRejectedEvents); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT reason,event_type,COUNT(*) FROM ad_event_rejections WHERE occurred_at>=$1 AND occurred_at<=$2 GROUP BY reason,event_type ORDER BY COUNT(*) DESC,reason,event_type`, out.From, out.To)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v AdRejectionReason
		if err = rows.Scan(&v.Reason, &v.EventType, &v.Count); err != nil {
			rows.Close()
			return nil, err
		}
		out.Reasons = append(out.Reasons, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Compare text to text: rejected challenges may refer to unknown or deleted
	// campaigns, and legacy identifiers need not parse as UUIDs.
	rows, err = tx.QueryContext(ctx, `SELECT r.id::text,r.campaign_id::text,c.name,s.name,r.event_type,r.reason,r.occurred_at FROM ad_event_rejections r LEFT JOIN seller_campaigns c ON c.id::text=r.campaign_id::text LEFT JOIN stores s ON s.id=c.store_id WHERE r.occurred_at>=$1 AND r.occurred_at<=$2 ORDER BY r.occurred_at DESC,r.id DESC LIMIT $3`, out.From, out.To, out.Limit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v AdRejectionIncident
		if err = rows.Scan(&v.ID, &v.CampaignID, &v.CampaignName, &v.StoreName, &v.EventType, &v.Reason, &v.OccurredAt); err != nil {
			rows.Close()
			return nil, err
		}
		out.Incidents = append(out.Incidents, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
