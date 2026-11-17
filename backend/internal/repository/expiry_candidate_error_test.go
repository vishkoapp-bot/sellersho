package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/drup/platform/internal/jobrunner"
	"github.com/lib/pq"
	"testing"
	"time"
)

// Semantic metadata wrappers must remain review failures even when their cause
// is a deadline. Current metadata also appears as an errors.Join sentinel.
type expiryMetadataFixtureError struct{ cause error }

func (e expiryMetadataFixtureError) Error() string        { return "metadata fixture" }
func (e expiryMetadataFixtureError) Unwrap() error        { return e.cause }
func (e expiryMetadataFixtureError) Is(target error) bool { return target == jobrunner.ErrMetadata }

type expirySQLStateFixtureError struct {
	code  string
	cause error
}

func (e expirySQLStateFixtureError) Error() string    { return "SQL state fixture" }
func (e expirySQLStateFixtureError) SQLState() string { return e.code }
func (e expirySQLStateFixtureError) Unwrap() error    { return e.cause }

type expiryUnknownDeadlineFixtureError struct{}

func (expiryUnknownDeadlineFixtureError) Error() string { return "unknown leaf claiming deadline" }
func (expiryUnknownDeadlineFixtureError) Is(target error) bool {
	return target == context.DeadlineExceeded
}

func TestExpiryCandidateTransientRequiresEntireKnownErrorTree(t *testing.T) {
	candidate, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	<-candidate.Done()
	ended := candidate.Err()
	if ended != context.DeadlineExceeded {
		t.Fatalf("actual candidate did not expire: %v", ended)
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	live, liveCancel := context.WithCancel(context.Background())
	defer liveCancel()
	unknown := errors.New("unknown candidate failure")
	tests := []struct {
		name            string
		err, contextErr error
		want            bool
	}{
		{"expired bare bad connection", driver.ErrBadConn, ended, true},
		{"expired wrapped bad connection", fmt.Errorf("candidate SQL: %w", driver.ErrBadConn), ended, true},
		{"canceled bad connection", driver.ErrBadConn, canceled.Err(), true},
		{"expired transaction done", sql.ErrTxDone, ended, true},
		{"expired wrapped transaction done", fmt.Errorf("rollback: %w", sql.ErrTxDone), ended, true},
		{"live bad connection", driver.ErrBadConn, live.Err(), false},
		{"live wrapped bad connection", fmt.Errorf("candidate SQL: %w", driver.ErrBadConn), nil, false},
		{"live transaction done", sql.ErrTxDone, nil, false},
		{"live wrapped transaction done", fmt.Errorf("rollback: %w", sql.ErrTxDone), nil, false},
		{"unknown context evidence", driver.ErrBadConn, unknown, false},
		{"unknown evidence transaction done", sql.ErrTxDone, unknown, false},
		{"deadline ordinary", context.DeadlineExceeded, nil, true},
		{"wrapped canceled ordinary", fmt.Errorf("candidate: %w", context.Canceled), nil, true},
		{"nil", nil, ended, false},
		{"unknown leaf", unknown, ended, false},
		{"unknown claiming deadline", expiryUnknownDeadlineFixtureError{}, ended, false},
		{"wrapped unknown", fmt.Errorf("candidate: %w", unknown), ended, false},
		{"financial leaf", ErrFinancePrincipalUnresolved, ended, false},
		{"refund leaf", ErrRefundPrincipalUnresolved, ended, false},
		{"unknown joined bad connection", errors.Join(unknown, driver.ErrBadConn), ended, false},
		{"unknown joined deadline", errors.Join(unknown, ended), ended, false},
		{"deadline first unknown sibling", errors.Join(ended, unknown), ended, false},
		{"financial joined deadline", errors.Join(ErrFinancePrincipalUnresolved, ended), ended, false},
		{"wrapped refund joined deadline", fmt.Errorf("candidate: %w", errors.Join(ended, ErrRefundPrincipalUnresolved)), ended, false},
		{"metadata leaf", jobrunner.ErrMetadata, ended, false},
		{"metadata joined bad connection", errors.Join(jobrunner.ErrMetadata, driver.ErrBadConn), ended, false},
		{"wrapped metadata joined bad connection", errors.Join(fmt.Errorf("record: %w", jobrunner.ErrMetadata), driver.ErrBadConn), ended, false},
		{"metadata joined deadline", errors.Join(jobrunner.ErrMetadata, ended), ended, false},
		{"nested metadata deadline", fmt.Errorf("candidate: %w", errors.Join(fmt.Errorf("record: %w", jobrunner.ErrMetadata), ended)), ended, false},
		{"metadata semantic wrapper", expiryMetadataFixtureError{ended}, ended, false},
		{"all known joined expired", errors.Join(fmt.Errorf("SQL: %w", driver.ErrBadConn), sql.ErrTxDone, ended, &pq.Error{Code: "55P03"}), ended, true},
		{"all known joined missing evidence", errors.Join(driver.ErrBadConn, ended), nil, false},
		{"mixed SQL state leaves", errors.Join(&pq.Error{Code: "57014"}, &pq.Error{Code: "23505"}), ended, false},
		{"unknown SQL with deadline", errors.Join(&pq.Error{Code: "23505"}, ended), ended, false},
		{"known SQL wrapper unknown cause", expirySQLStateFixtureError{"57014", unknown}, ended, false},
		{"unknown SQL wrapper deadline", expirySQLStateFixtureError{"23505", ended}, ended, false},
	}
	for _, code := range []string{"57014", "55P03", "40001", "40P01"} {
		tests = append(tests, struct {
			name            string
			err, contextErr error
			want            bool
		}{"known SQL " + code, &pq.Error{Code: pq.ErrorCode(code)}, nil, true}, struct {
			name            string
			err, contextErr error
			want            bool
		}{"wrapped known SQL " + code, fmt.Errorf("candidate SQL: %w", &pq.Error{Code: pq.ErrorCode(code)}), nil, true})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := expiryCandidateTransient(tc.err, tc.contextErr); got != tc.want {
				t.Fatalf("transient=%v want=%v error=%T %v candidate_context=%v", got, tc.want, tc.err, tc.err, tc.contextErr)
			}
		})
	}
}
