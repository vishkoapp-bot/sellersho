package repository

import (
	"context"
	"database/sql"
)

func (r *StoreRepository) AllowAdChallenge(ctx context.Context, identity string) (bool, error) {
	var n int
	e := r.db.QueryRowContext(ctx, `INSERT INTO ad_request_limits(identity_key,bucket,count) VALUES($1,date_trunc('minute',NOW()),1) ON CONFLICT(identity_key,bucket) DO UPDATE SET count=ad_request_limits.count+1 WHERE ad_request_limits.count<60 RETURNING count`, identity).Scan(&n)
	if e == sql.ErrNoRows {
		return false, nil
	}
	return e == nil, e
}
