package repository

import (
	"context"
	"database/sql"
	"errors"
	"github.com/drup/platform/internal/jobrunner"
	"time"
)

type JobStore struct{ DB *sql.DB }

func (s JobStore) Acquire(ctx context.Context, job, owner string, ttl time.Duration) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO background_job_state(job,owner,lease_until,started_at) VALUES($1,$2,clock_timestamp()+$3*INTERVAL '1 millisecond',clock_timestamp()) ON CONFLICT(job) DO UPDATE SET owner=EXCLUDED.owner,lease_until=EXCLUDED.lease_until,started_at=EXCLUDED.started_at WHERE background_job_state.owner IS NULL OR background_job_state.lease_until<=clock_timestamp()`, job, owner, ttl.Milliseconds())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
func (s JobStore) Complete(ctx context.Context, o jobrunner.Outcome) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Holding only the metadata row makes idempotent outcome + legacy shapes atomic.
	var owner sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT owner FROM background_job_state WHERE job=$1 FOR UPDATE`, o.Job).Scan(&owner); err != nil {
		return err
	}
	if !owner.Valid || owner.String != o.Owner {
		return errors.New("job ownership lost")
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO background_job_outcomes(run_id,job,started_at,duration_ms,affected,confirmed,disabled,error_code) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(run_id) DO NOTHING`, o.Owner, o.Job, o.Started, o.Duration.Milliseconds(), o.Result.Affected, o.Result.Confirmed, o.Result.Disabled, o.ErrorCode)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE background_job_state SET finished_at=clock_timestamp(),last_success_at=CASE WHEN $3='' THEN clock_timestamp() ELSE last_success_at END,last_error=$3,disabled=$4,runs=runs+1,affected=affected+$5 WHERE job=$1 AND owner=$2`, o.Job, o.Owner, o.ErrorCode, o.Result.Disabled, o.Result.Affected); err != nil {
		return err
	}
	if o.Job == "delivery-lock-releaser" {
		_, err = tx.ExecContext(ctx, `INSERT INTO job_runs(job_id,started_at,released,confirmed,duration_ms,error) VALUES($1,$2,$3,$4,$5,$6)`, o.Job, o.Started, o.Result.Affected, o.Result.Confirmed, o.Duration.Milliseconds(), o.ErrorCode)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO operations_job_runs(job,affected,error,started_at,finished_at) VALUES($1,$2,$3,$4,clock_timestamp())`, o.Job, o.Result.Affected, o.ErrorCode, o.Started)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s JobStore) Release(ctx context.Context, job, owner string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE background_job_state SET owner=NULL,lease_until=NULL WHERE job=$1 AND owner=$2`, job, owner)
	return err
}

// Retry state is operational metadata; a poison row yields to the next ready row.
func RecordJobCandidateRetry(ctx context.Context, db *sql.DB, job, id string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO background_job_candidate_retry(job,candidate,next_retry_at,reason) VALUES($1,$2,clock_timestamp()+INTERVAL '5 minutes','candidate_rejected') ON CONFLICT(job,candidate) DO UPDATE SET next_retry_at=EXCLUDED.next_retry_at,reason=EXCLUDED.reason`, job, id)
	return err
}
