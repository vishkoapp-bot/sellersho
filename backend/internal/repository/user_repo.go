package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/phone"
	"github.com/google/uuid"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) GetProfileName(ctx context.Context, userID string) (string, error) {
	var name string
	err := r.db.QueryRowContext(ctx, `SELECT full_name FROM users WHERE id = $1 AND is_active`, userID).Scan(&name)
	return name, err
}

func (r *UserRepository) SetProfileName(ctx context.Context, userID, name string) (string, error) {
	err := r.db.QueryRowContext(ctx, `
		UPDATE users SET full_name = $2, updated_at = NOW()
		WHERE id = $1 AND is_active RETURNING full_name
	`, userID, name).Scan(&name)
	return name, err
}

// FindByMobile retrieves user by unique mobile phone
func (r *UserRepository) FindByMobile(ctx context.Context, mobile string) (*domain.User, error) {
	mobile = phone.Normalize(mobile)
	query := `SELECT id, mobile, full_name, role, is_active, COALESCE(username, ''), created_at, updated_at FROM users WHERE mobile = $1`
	row := r.db.QueryRowContext(ctx, query, mobile)

	var u domain.User
	if err := row.Scan(&u.ID, &u.Mobile, &u.FullName, &u.Role, &u.IsActive, &u.Username, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query user by mobile: %w", err)
	}

	return &u, nil
}

// FindByUsername retrieves an active or inactive staff account by username.
func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*domain.User, string, int, *time.Time, error) {
	query := `
		SELECT id, mobile, full_name, role, is_active, COALESCE(username, ''),
		       COALESCE(password_hash, ''), failed_login_attempts, locked_until,
		       created_at, updated_at
		FROM users
		WHERE LOWER(username) = LOWER($1)
	`
	row := r.db.QueryRowContext(ctx, query, username)

	var u domain.User
	var passwordHash string
	var failedAttempts int
	var lockedUntil *time.Time
	if err := row.Scan(
		&u.ID, &u.Mobile, &u.FullName, &u.Role, &u.IsActive, &u.Username,
		&passwordHash, &failedAttempts, &lockedUntil, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, "", 0, nil, nil
		}
		return nil, "", 0, nil, fmt.Errorf("failed to query user by username: %w", err)
	}

	return &u, passwordHash, failedAttempts, lockedUntil, nil
}

func (r *UserRepository) RecordLoginFailure(ctx context.Context, userID string, lockDuration time.Duration) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET failed_login_attempts = failed_login_attempts + 1,
		    locked_until = CASE
		        WHEN failed_login_attempts + 1 >= 3 THEN NOW() + $2::interval
		        ELSE locked_until
		    END,
		    updated_at = NOW()
		WHERE id = $1
	`, userID, lockDuration.String())
	return err
}

func (r *UserRepository) ResetLoginFailures(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET failed_login_attempts = 0, locked_until = NULL, updated_at = NOW()
		WHERE id = $1
	`, userID)
	return err
}

// UpsertUser finds or creates a user with default role and name
func (r *UserRepository) UpsertUser(ctx context.Context, mobile, defaultName, defaultRole string) (*domain.User, error) {
	mobile = phone.Normalize(mobile)
	existing, err := r.FindByMobile(ctx, mobile)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	newID := uuid.New().String()
	now := time.Now()
	query := `
		INSERT INTO users (id, mobile, full_name, role, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, TRUE, $5, $5)
		RETURNING id, mobile, full_name, role, is_active, '', created_at, updated_at
	`
	row := r.db.QueryRowContext(ctx, query, newID, mobile, defaultName, defaultRole, now)

	var u domain.User
	if err := row.Scan(&u.ID, &u.Mobile, &u.FullName, &u.Role, &u.IsActive, &u.Username, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, fmt.Errorf("failed to insert user: %w", err)
	}

	return &u, nil
}
