package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/drup/platform/internal/jobrunner"
	"github.com/lib/pq"
)

func TestCandidateBudgetCancellationRequiresSameChainDeadlineForBadConnection(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"observed supplier SLA chain", errors.Join(&pq.Error{Code: "57014"}, driver.ErrBadConn, context.DeadlineExceeded, context.DeadlineExceeded), true},
		{"bare bad connection", driver.ErrBadConn, false},
		{"postgres cancellation without deadline", errors.Join(&pq.Error{Code: "57014"}, driver.ErrBadConn), false},
		{"cancelled without deadline", errors.Join(context.Canceled, driver.ErrBadConn), false},
		{"metadata in deadline chain", errors.Join(jobrunner.ErrMetadata, driver.ErrBadConn, context.DeadlineExceeded), false},
		{"wrapped metadata", errors.Join(fmt.Errorf("recording: %w", jobrunner.ErrMetadata), driver.ErrBadConn, context.DeadlineExceeded), false},
		{"unknown joined leaf", errors.Join(errors.New("unexpected failure"), driver.ErrBadConn, context.DeadlineExceeded), false},
		{"deadline", context.DeadlineExceeded, true},
		{"transaction rollback", sql.ErrTxDone, true},
		{"postgres cancellation", &pq.Error{Code: "57014"}, true},
		{"unrelated postgres error", &pq.Error{Code: "23505"}, false},
		{"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCandidateBudgetCancellation(tc.err); got != tc.want {
				t.Fatalf("cancellation=%v want %v: %v", got, tc.want, tc.err)
			}
		})
	}
}
