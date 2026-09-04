package store

import (
	"context"
	"database/sql"
	"time"

	"proxyctl/internal/ident"
	"proxyctl/internal/model"
)

func (s *SQLite) CreateUser(ctx context.Context, u *model.User) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO users(
		id, username, display_name, password_hash, role, locale,
		totp_secret, totp_pending, totp_confirmed, disabled,
		created_at, updated_at, last_login_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(u.ID), u.Username, u.DisplayName, u.PasswordHash, string(u.Role), localeOrDefault(u.Locale),
		u.TOTPSecret, u.TOTPPending, boolInt(u.TOTPEnabled), boolInt(u.Disabled),
		nowRFC3339(u.CreatedAt), nowRFC3339(u.UpdatedAt), lastLoginValue(u.LastLoginAt),
	)
	if isUnique(err) {
		return model.ErrConflict("username already exists")
	}
	return wrap("CreateUser", err)
}

func (s *SQLite) GetUser(ctx context.Context, id model.ID) (*model.User, error) {
	row := s.db.QueryRowContext(ctx, userSelect+` WHERE id=?`, string(id))
	return scanUser(row)
}

func (s *SQLite) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	row := s.db.QueryRowContext(ctx, userSelect+` WHERE username=?`, username)
	return scanUserByName(row)
}

func (s *SQLite) ListUsers(ctx context.Context) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx, userSelect+` ORDER BY username`)
	if err != nil {
		return nil, wrap("ListUsers", err)
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *SQLite) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM users`).Scan(&n)
	return n, wrap("CountUsers", err)
}

func (s *SQLite) CountAdministrators(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM users WHERE role='administrator' AND disabled=0`).Scan(&n)
	return n, wrap("CountAdministrators", err)
}

func (s *SQLite) UpdateUser(ctx context.Context, u *model.User) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET
		username=?, display_name=?, password_hash=?, role=?, locale=?,
		totp_secret=?, totp_pending=?, totp_confirmed=?, disabled=?,
		updated_at=?, last_login_at=?
		WHERE id=?`,
		u.Username, u.DisplayName, u.PasswordHash, string(u.Role), localeOrDefault(u.Locale),
		u.TOTPSecret, u.TOTPPending, boolInt(u.TOTPEnabled), boolInt(u.Disabled),
		nowRFC3339(u.UpdatedAt), lastLoginValue(u.LastLoginAt), string(u.ID),
	)
	if err != nil {
		if isUnique(err) {
			return model.ErrConflict("username already exists")
		}
		return wrap("UpdateUser", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) ReplaceRecoveryCodes(ctx context.Context, userID model.ID, hashes []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrap("ReplaceRecoveryCodes", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_recovery_codes WHERE user_id=?`, string(userID)); err != nil {
		return wrap("ReplaceRecoveryCodes", err)
	}
	for _, h := range hashes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_recovery_codes(id,user_id,code_hash,used_at) VALUES(?,?,?,?)`,
			string(ident.New()), string(userID), h, ""); err != nil {
			return wrap("ReplaceRecoveryCodes", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return wrap("ReplaceRecoveryCodes", err)
	}
	return nil
}

func (s *SQLite) ListRecoveryCodeHashes(ctx context.Context, userID model.ID) ([]RecoveryCode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,user_id,code_hash,used_at FROM user_recovery_codes WHERE user_id=?`, string(userID))
	if err != nil {
		return nil, wrap("ListRecoveryCodeHashes", err)
	}
	defer rows.Close()
	var out []RecoveryCode
	for rows.Next() {
		var c RecoveryCode
		var used string
		if err := rows.Scan(&c.ID, &c.UserID, &c.Hash, &used); err != nil {
			return nil, wrap("ListRecoveryCodeHashes", err)
		}
		c.UsedAt = parseTime(used)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *SQLite) MarkRecoveryCodeUsed(ctx context.Context, id model.ID, usedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE user_recovery_codes SET used_at=? WHERE id=?`, nowRFC3339(usedAt), string(id))
	return wrap("MarkRecoveryCodeUsed", err)
}

const userSelect = `SELECT id, username, display_name, password_hash, role, locale,
	totp_secret, totp_pending, totp_confirmed, disabled, created_at, updated_at, last_login_at
	FROM users`

func scanUser(row scanner) (*model.User, error) {
	u, err := scanUserInto(row)
	if err != nil {
		return nil, wrap("GetUser", err)
	}
	return u, nil
}

func scanUserByName(row scanner) (*model.User, error) {
	u, err := scanUserInto(row)
	if err != nil {
		return nil, wrap("GetUserByUsername", err)
	}
	return u, nil
}

func scanUserRow(rows *sql.Rows) (*model.User, error) {
	return scanUserInto(rows)
}

func scanUserInto(row scanner) (*model.User, error) {
	var u model.User
	var created, updated, lastLogin string
	var confirmed, disabled int
	if err := row.Scan(
		&u.ID, &u.Username, &u.DisplayName, &u.PasswordHash, &u.Role, &u.Locale,
		&u.TOTPSecret, &u.TOTPPending, &confirmed, &disabled, &created, &updated, &lastLogin,
	); err != nil {
		return nil, err
	}
	u.TOTPEnabled = confirmed == 1 && u.TOTPSecret != ""
	u.Disabled = disabled == 1
	u.CreatedAt = parseTime(created)
	u.UpdatedAt = parseTime(updated)
	u.LastLoginAt = parseTime(lastLogin)
	u.Locale = localeOrDefault(u.Locale)
	return &u, nil
}

func localeOrDefault(s string) string {
	if s == "ru" {
		return "ru"
	}
	return "en"
}

func lastLoginValue(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return nowRFC3339(t)
}
