package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db   *sql.DB
	path string
	now  func() time.Time
	rand io.Reader
}

func NewSQLiteStore(dir string) (*SQLiteStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create auth data directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, fmt.Errorf("secure auth data directory: %w", err)
	}
	path := filepath.Join(dir, "bookbrowser.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite auth store: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &SQLiteStore{db: db, path: path, now: time.Now, rand: rand.Reader}
	if err := store.initialize(); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		db.Close()
		return nil, fmt.Errorf("secure SQLite auth store: %w", err)
	}
	return store, nil
}

func (s *SQLiteStore) initialize() error {
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("configure SQLite: %w", err)
		}
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	var version int
	if err := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > 8 {
		return fmt.Errorf("unsupported auth schema version %d", version)
	}
	if version == 0 {
		if err := s.migrateV1(); err != nil {
			return err
		}
		version = 1
	}
	if version == 1 {
		if err := s.migrateV2(); err != nil {
			return err
		}
		version = 2
	}
	if version == 2 {
		if err := s.migrateV3(); err != nil {
			return err
		}
		version = 3
	}
	if version == 3 {
		if err := s.migrateV4(); err != nil {
			return err
		}
		version = 4
	}
	if version == 4 {
		if err := s.migrateV5(); err != nil {
			return err
		}
		version = 5
	}
	if version == 5 {
		if err := s.migrateV6(); err != nil {
			return err
		}
		version = 6
	}
	if version == 6 {
		if err := s.migrateV7(); err != nil {
			return err
		}
		version = 7
	}
	if version == 7 {
		if err := s.migrateV8(); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) migrateV8() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE book_requests (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title TEXT NOT NULL,
			author TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending'
				CHECK (status IN ('pending', 'added', 'unavailable')),
			book_id TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX book_requests_user_idx ON book_requests(user_id, created_at DESC)`,
		`CREATE INDEX book_requests_status_idx ON book_requests(status, created_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("apply auth migration v8: %w", err)
		}
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations(version, applied_at) VALUES(8, ?)", s.now().UTC().Unix()); err != nil {
		return fmt.Errorf("record auth migration v8: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit auth migration v8: %w", err)
	}
	return nil
}

func (s *SQLiteStore) migrateV7() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`CREATE TABLE api_tokens (
			token_hash TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL COLLATE NOCASE,
			created_at INTEGER NOT NULL,
			last_used_at INTEGER,
			expires_at INTEGER,
			UNIQUE (user_id, name)
		)`,
		`CREATE INDEX api_tokens_user_idx ON api_tokens(user_id, created_at DESC)`,
		`CREATE INDEX api_tokens_expiry_idx ON api_tokens(expires_at)`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("apply auth migration v7: %w", err)
		}
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations(version, applied_at) VALUES(7, ?)", s.now().UTC().Unix()); err != nil {
		return fmt.Errorf("record auth migration v7: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit auth migration v7: %w", err)
	}
	return nil
}

func (s *SQLiteStore) migrateV6() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`ALTER TABLE users ADD COLUMN language TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("apply auth migration v6: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations(version, applied_at) VALUES(6, ?)", s.now().UTC().Unix()); err != nil {
		return fmt.Errorf("record auth migration v6: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit auth migration v6: %w", err)
	}
	return nil
}

func (s *SQLiteStore) migrateV5() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`ALTER TABLE users ADD COLUMN last_ip TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN allow_create_share_links INTEGER NOT NULL DEFAULT 1`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("apply auth migration v5: %w", err)
		}
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations(version, applied_at) VALUES(5, ?)", s.now().UTC().Unix()); err != nil {
		return fmt.Errorf("record auth migration v5: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit auth migration v5: %w", err)
	}
	return nil
}

func (s *SQLiteStore) migrateV4() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE user_reading_items (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			book_id TEXT NOT NULL,
			kind TEXT NOT NULL CHECK (kind IN ('bookmark', 'note')),
			locator TEXT NOT NULL,
			locator_label TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			body TEXT NOT NULL DEFAULT '',
			excerpt TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX user_reading_items_book_idx
			ON user_reading_items(user_id, book_id, updated_at DESC)`,
		`CREATE INDEX user_reading_items_recent_idx
			ON user_reading_items(user_id, updated_at DESC)`,
		`CREATE TABLE user_reading_item_tags (
			item_id TEXT NOT NULL REFERENCES user_reading_items(id) ON DELETE CASCADE,
			tag TEXT NOT NULL COLLATE NOCASE,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (item_id, tag)
		)`,
		`CREATE INDEX user_reading_item_tags_lookup_idx
			ON user_reading_item_tags(tag, created_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("apply auth migration v4: %w", err)
		}
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations(version, applied_at) VALUES(4, ?)", s.now().UTC().Unix()); err != nil {
		return fmt.Errorf("record auth migration v4: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit auth migration v4: %w", err)
	}
	return nil
}

func (s *SQLiteStore) migrateV1() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE users (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE COLLATE NOCASE,
			name TEXT NOT NULL,
			role TEXT NOT NULL CHECK (role IN ('reader', 'manager', 'admin')),
			active INTEGER NOT NULL CHECK (active IN (0, 1)),
			password_hash TEXT NOT NULL DEFAULT '',
			google_subject TEXT UNIQUE,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			last_login_at INTEGER
		)`,
		`CREATE TABLE sessions (
			token_hash TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		)`,
		`CREATE INDEX sessions_user_id_idx ON sessions(user_id)`,
		`CREATE INDEX sessions_expires_at_idx ON sessions(expires_at)`,
		`CREATE TABLE settings (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			site_name TEXT NOT NULL,
			registration_open INTEGER NOT NULL CHECK (registration_open IN (0, 1)),
			anonymous_book_links INTEGER NOT NULL CHECK (anonymous_book_links IN (0, 1))
		)`,
		`INSERT INTO settings (id, site_name, registration_open, anonymous_book_links)
		 VALUES (1, 'BookBrowser', 1, 1)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("apply auth migration v1: %w", err)
		}
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations(version, applied_at) VALUES(1, ?)", s.now().UTC().Unix()); err != nil {
		return fmt.Errorf("record auth migration v1: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit auth migration v1: %w", err)
	}
	return nil
}

func (s *SQLiteStore) migrateV2() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`ALTER TABLE settings ADD COLUMN pwa_name TEXT NOT NULL DEFAULT 'MicsBook'`,
		`UPDATE settings SET site_name = 'MicsBook' WHERE site_name = 'BookBrowser'`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("apply auth migration v2: %w", err)
		}
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations(version, applied_at) VALUES(2, ?)", s.now().UTC().Unix()); err != nil {
		return fmt.Errorf("record auth migration v2: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit auth migration v2: %w", err)
	}
	return nil
}

func (s *SQLiteStore) migrateV3() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE user_book_activity (
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			book_id TEXT NOT NULL,
			last_read_at INTEGER NOT NULL,
			PRIMARY KEY (user_id, book_id)
		)`,
		`CREATE INDEX user_book_activity_recent_idx
			ON user_book_activity(user_id, last_read_at DESC)`,
		`CREATE TABLE user_book_lists (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL COLLATE NOCASE,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE (user_id, name)
		)`,
		`CREATE INDEX user_book_lists_user_idx
			ON user_book_lists(user_id, updated_at DESC)`,
		`CREATE TABLE user_book_list_items (
			list_id TEXT NOT NULL REFERENCES user_book_lists(id) ON DELETE CASCADE,
			book_id TEXT NOT NULL,
			added_at INTEGER NOT NULL,
			PRIMARY KEY (list_id, book_id)
		)`,
		`CREATE INDEX user_book_list_items_added_idx
			ON user_book_list_items(list_id, added_at DESC)`,
		`CREATE TABLE user_book_tags (
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			book_id TEXT NOT NULL,
			tag TEXT NOT NULL COLLATE NOCASE,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (user_id, book_id, tag)
		)`,
		`CREATE INDEX user_book_tags_lookup_idx
			ON user_book_tags(user_id, tag, created_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("apply auth migration v3: %w", err)
		}
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations(version, applied_at) VALUES(3, ?)", s.now().UTC().Unix()); err != nil {
		return fmt.Errorf("record auth migration v3: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit auth migration v3: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Path() string { return s.path }

func (s *SQLiteStore) Close() error { return s.db.Close() }

func (s *SQLiteStore) CountUsers() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

func (s *SQLiteStore) CanRegister() (bool, error) {
	var count, open int
	err := s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM users), registration_open FROM settings WHERE id = 1`).Scan(&count, &open)
	return count == 0 || open == 1, err
}

func (s *SQLiteStore) Settings() (Settings, error) {
	var settings Settings
	var registrationOpen, anonymousBookLinks int
	err := s.db.QueryRow(`SELECT site_name, pwa_name, registration_open, anonymous_book_links FROM settings WHERE id = 1`).Scan(
		&settings.SiteName, &settings.PWAName, &registrationOpen, &anonymousBookLinks,
	)
	settings.RegistrationOpen = registrationOpen == 1
	settings.AnonymousBookLinks = anonymousBookLinks == 1
	return settings, err
}

func (s *SQLiteStore) UpdateSettings(settings Settings) error {
	settings.SiteName = strings.TrimSpace(settings.SiteName)
	if settings.SiteName == "" {
		settings.SiteName = DefaultSettings().SiteName
	}
	if len(settings.SiteName) > 80 {
		return errors.New("site name must not exceed 80 characters")
	}
	settings.PWAName = strings.TrimSpace(settings.PWAName)
	if settings.PWAName == "" {
		settings.PWAName = DefaultSettings().PWAName
	}
	if len(settings.PWAName) > 80 {
		return errors.New("PWA app name must not exceed 80 characters")
	}
	_, err := s.db.Exec(`UPDATE settings
		SET site_name = ?, pwa_name = ?, registration_open = ?, anonymous_book_links = ? WHERE id = 1`,
		settings.SiteName, settings.PWAName, boolInt(settings.RegistrationOpen), boolInt(settings.AnonymousBookLinks),
	)
	return err
}

func (s *SQLiteStore) RegisterEmail(email, name, password string) (*User, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return nil, err
	}
	name = normalizeName(name, email)
	if len(password) < 10 {
		return nil, errors.New("password must contain at least 10 characters")
	}
	hash, err := hashPassword(password, s.rand)
	if err != nil {
		return nil, err
	}
	id, err := randomToken(s.rand, 16)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var count, registrationOpen, duplicate int
	if err := tx.QueryRow(`SELECT (SELECT COUNT(*) FROM users), registration_open,
		EXISTS(SELECT 1 FROM users WHERE email = ? COLLATE NOCASE) FROM settings WHERE id = 1`, email).
		Scan(&count, &registrationOpen, &duplicate); err != nil {
		return nil, err
	}
	if duplicate == 1 {
		return nil, ErrEmailExists
	}
	if count != 0 && registrationOpen != 1 {
		return nil, errors.New("registration is closed")
	}
	role := RoleReader
	if count == 0 {
		role = RoleAdmin
	}
	now := s.now().UTC()
	user := &User{ID: id, Email: email, Name: name, Role: role, Active: true, AllowShare: true, PasswordHash: hash, CreatedAt: now, UpdatedAt: now}
	if _, err := tx.Exec(`INSERT INTO users
		(id, email, name, role, active, password_hash, google_subject, created_at, updated_at)
		VALUES (?, ?, ?, ?, 1, ?, NULL, ?, ?)`, user.ID, user.Email, user.Name, user.Role, user.PasswordHash, now.Unix(), now.Unix()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *SQLiteStore) AuthenticateEmail(email, password string) (*User, error) {
	user, err := s.queryUser(`SELECT `+userColumns+` FROM users WHERE email = ? COLLATE NOCASE`, strings.TrimSpace(email))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && user.PasswordHash == "") {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if !verifyPassword(user.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}
	if !user.Active {
		return nil, ErrInactive
	}
	now := s.now().UTC()
	if _, err := s.db.Exec("UPDATE users SET last_login_at = ?, updated_at = ? WHERE id = ?", now.Unix(), now.Unix(), user.ID); err != nil {
		return nil, err
	}
	user.LastLoginAt = &now
	user.UpdatedAt = now
	return user, nil
}

func (s *SQLiteStore) UpsertGoogle(email, name, subject string, emailAuthoritative bool) (*User, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return nil, err
	}
	name = normalizeName(name, email)
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil, errors.New("Google subject is required")
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	user, err := queryUserWith(tx, `SELECT `+userColumns+` FROM users WHERE google_subject = ?`, subject)
	if err == nil {
		if !user.Active {
			return nil, ErrInactive
		}
		now := s.now().UTC()
		if _, err := tx.Exec(`UPDATE users SET name = CASE WHEN name = '' THEN ? ELSE name END,
			last_login_at = ?, updated_at = ? WHERE id = ?`, name, now.Unix(), now.Unix(), user.ID); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		user.LastLoginAt = &now
		user.UpdatedAt = now
		return user, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	user, err = queryUserWith(tx, `SELECT `+userColumns+` FROM users WHERE email = ? COLLATE NOCASE`, email)
	if err == nil {
		if !user.Active {
			return nil, ErrInactive
		}
		if user.GoogleSubject != "" && user.GoogleSubject != subject {
			return nil, ErrIdentityConflict
		}
		if !emailAuthoritative {
			return nil, ErrIdentityConflict
		}
		now := s.now().UTC()
		if _, err := tx.Exec(`UPDATE users SET google_subject = ?, name = CASE WHEN name = '' THEN ? ELSE name END,
			last_login_at = ?, updated_at = ? WHERE id = ?`, subject, name, now.Unix(), now.Unix(), user.ID); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		user.GoogleSubject = subject
		user.LastLoginAt = &now
		user.UpdatedAt = now
		return user, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var count, registrationOpen int
	if err := tx.QueryRow(`SELECT (SELECT COUNT(*) FROM users), registration_open FROM settings WHERE id = 1`).Scan(&count, &registrationOpen); err != nil {
		return nil, err
	}
	if count != 0 && registrationOpen != 1 {
		return nil, errors.New("registration is closed")
	}
	id, err := randomToken(s.rand, 16)
	if err != nil {
		return nil, err
	}
	role := RoleReader
	if count == 0 {
		role = RoleAdmin
	}
	now := s.now().UTC()
	user = &User{ID: id, Email: email, Name: name, Role: role, Active: true, AllowShare: true, GoogleSubject: subject, CreatedAt: now, UpdatedAt: now, LastLoginAt: &now}
	if _, err := tx.Exec(`INSERT INTO users
		(id, email, name, role, active, password_hash, google_subject, created_at, updated_at, last_login_at)
		VALUES (?, ?, ?, ?, 1, '', ?, ?, ?, ?)`, id, email, name, role, subject, now.Unix(), now.Unix(), now.Unix()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return user, nil
}

func (s *SQLiteStore) NewSession(userID string) (string, error) {
	token, err := randomToken(s.rand, 32)
	if err != nil {
		return "", err
	}
	now := s.now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var active int
	if err := tx.QueryRow("SELECT active FROM users WHERE id = ?", userID).Scan(&active); err != nil || active != 1 {
		if err == nil {
			err = ErrInactive
		}
		return "", err
	}
	if _, err := tx.Exec("DELETE FROM sessions WHERE expires_at <= ?", now.Unix()); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`INSERT INTO sessions(token_hash, user_id, created_at, expires_at) VALUES(?, ?, ?, ?)`,
		tokenHash(token), userID, now.Unix(), now.Add(sessionDuration).Unix()); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE token_hash IN (
		SELECT token_hash FROM sessions WHERE user_id = ? ORDER BY created_at DESC LIMIT -1 OFFSET 20
	)`, userID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return token, nil
}

func (s *SQLiteStore) UserForSession(token string) (*User, error) {
	if token == "" {
		return nil, nil
	}
	user, err := s.queryUser(`SELECT `+prefixedUserColumns+` FROM users u
		JOIN sessions s ON s.user_id = u.id
		WHERE s.token_hash = ? AND s.expires_at > ? AND u.active = 1`, tokenHash(token), s.now().UTC().Unix())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return user, err
}

func (s *SQLiteStore) DeleteSession(token string) error {
	_, err := s.db.Exec("DELETE FROM sessions WHERE token_hash = ?", tokenHash(token))
	return err
}

var apiTokenNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,80}$`)

func normalizeAPITokenName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !apiTokenNamePattern.MatchString(name) {
		return "", errors.New("token name must contain only letters, digits, dot, underscore, or hyphen and be at most 80 characters")
	}
	return name, nil
}

func (s *SQLiteStore) CreateAPIToken(userID, name string, expiresAt *time.Time) (string, *APIToken, error) {
	name, err := normalizeAPITokenName(name)
	if err != nil {
		return "", nil, err
	}
	random, err := randomToken(s.rand, 32)
	if err != nil {
		return "", nil, err
	}
	raw := "bbk_" + random
	now := s.now().UTC()
	var expiry interface{}
	var normalizedExpiry *time.Time
	if expiresAt != nil {
		value := expiresAt.UTC()
		if !value.After(now) {
			return "", nil, errors.New("token expiry must be in the future")
		}
		normalizedExpiry = &value
		expiry = value.Unix()
	}

	tx, err := s.db.Begin()
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback()
	var active, duplicate int
	if err := tx.QueryRow(`SELECT active,
		EXISTS(SELECT 1 FROM api_tokens WHERE user_id = ? AND name = ? COLLATE NOCASE)
		FROM users WHERE id = ?`, userID, name, userID).Scan(&active, &duplicate); err != nil {
		return "", nil, err
	}
	if active != 1 {
		return "", nil, ErrInactive
	}
	if duplicate == 1 {
		return "", nil, ErrAPITokenNameExists
	}
	if _, err := tx.Exec(`INSERT INTO api_tokens
		(token_hash, user_id, name, created_at, expires_at) VALUES(?, ?, ?, ?, ?)`,
		tokenHash(raw), userID, name, now.Unix(), expiry); err != nil {
		return "", nil, err
	}
	if err := tx.Commit(); err != nil {
		return "", nil, err
	}
	return raw, &APIToken{Name: name, UserID: userID, CreatedAt: now, ExpiresAt: normalizedExpiry}, nil
}

func (s *SQLiteStore) UserForAPIToken(token string) (*User, *APIToken, error) {
	if !strings.HasPrefix(token, "bbk_") || len(token) < 20 {
		return nil, nil, nil
	}
	now := s.now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	var item APIToken
	var createdAt int64
	var lastUsedAt, expiresAt sql.NullInt64
	if err := tx.QueryRow(`SELECT user_id, name, created_at, last_used_at, expires_at
		FROM api_tokens WHERE token_hash = ? AND (expires_at IS NULL OR expires_at > ?)`,
		tokenHash(token), now.Unix()).Scan(
		&item.UserID, &item.Name, &createdAt, &lastUsedAt, &expiresAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	user, err := queryUserWith(tx, `SELECT `+userColumns+` FROM users WHERE id = ? AND active = 1`, item.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec("UPDATE api_tokens SET last_used_at = ? WHERE token_hash = ?", now.Unix(), tokenHash(token)); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	item.CreatedAt = time.Unix(createdAt, 0).UTC()
	item.LastUsedAt = &now
	if expiresAt.Valid {
		value := time.Unix(expiresAt.Int64, 0).UTC()
		item.ExpiresAt = &value
	}
	return user, &item, nil
}

func (s *SQLiteStore) APITokens(userID string) ([]APIToken, error) {
	rows, err := s.db.Query(`SELECT name, created_at, last_used_at, expires_at
		FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]APIToken, 0)
	for rows.Next() {
		var item APIToken
		var createdAt int64
		var lastUsedAt, expiresAt sql.NullInt64
		if err := rows.Scan(&item.Name, &createdAt, &lastUsedAt, &expiresAt); err != nil {
			return nil, err
		}
		item.UserID = userID
		item.CreatedAt = time.Unix(createdAt, 0).UTC()
		if lastUsedAt.Valid {
			value := time.Unix(lastUsedAt.Int64, 0).UTC()
			item.LastUsedAt = &value
		}
		if expiresAt.Valid {
			value := time.Unix(expiresAt.Int64, 0).UTC()
			item.ExpiresAt = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SQLiteStore) RevokeAPIToken(userID, name string) error {
	name, err := normalizeAPITokenName(name)
	if err != nil {
		return err
	}
	result, err := s.db.Exec("DELETE FROM api_tokens WHERE user_id = ? AND name = ? COLLATE NOCASE", userID, name)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrAPITokenNotFound
	}
	return nil
}

func (s *SQLiteStore) RevokeCurrentAPIToken(token string) error {
	result, err := s.db.Exec("DELETE FROM api_tokens WHERE token_hash = ?", tokenHash(token))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrAPITokenNotFound
	}
	return nil
}

func (s *SQLiteStore) Users() ([]User, error) {
	rows, err := s.db.Query(`SELECT ` + userColumns + ` FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *user)
	}
	sort.SliceStable(users, func(i, j int) bool { return users[i].CreatedAt.Before(users[j].CreatedAt) })
	return users, rows.Err()
}

func (s *SQLiteStore) UserByID(id string) (*User, error) {
	user, err := s.queryUser(`SELECT `+userColumns+` FROM users WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return user, err
}

func (s *SQLiteStore) UserByEmail(email string) (*User, error) {
	user, err := s.queryUser(`SELECT `+userColumns+` FROM users WHERE email = ? COLLATE NOCASE`, strings.TrimSpace(email))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return user, err
}

func (s *SQLiteStore) UpdateUser(id string, role Role, active bool) (*User, error) {
	if !role.Valid() {
		return nil, ErrInvalidRole
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	user, err := queryUserWith(tx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("user not found")
		}
		return nil, err
	}
	if user.Active && user.Role == RoleAdmin && (!active || role != RoleAdmin) {
		var activeAdmins int
		if err := tx.QueryRow("SELECT COUNT(*) FROM users WHERE active = 1 AND role = 'admin'").Scan(&activeAdmins); err != nil {
			return nil, err
		}
		if activeAdmins <= 1 {
			return nil, ErrLastAdmin
		}
	}
	now := s.now().UTC()
	if _, err := tx.Exec("UPDATE users SET role = ?, active = ?, updated_at = ? WHERE id = ?", role, boolInt(active), now.Unix(), id); err != nil {
		return nil, err
	}
	if !active {
		if _, err := tx.Exec("DELETE FROM sessions WHERE user_id = ?", id); err != nil {
			return nil, err
		}
		if _, err := tx.Exec("DELETE FROM api_tokens WHERE user_id = ?", id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	user.Role = role
	user.Active = active
	user.UpdatedAt = now
	return user, nil
}

func (s *SQLiteStore) SetPassword(userID, password string) error {
	if len(password) < 10 {
		return errors.New("password must contain at least 10 characters")
	}
	hash, err := hashPassword(password, s.rand)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec("UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?", hash, now.Unix(), userID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errors.New("user not found")
	}
	if _, err := tx.Exec("DELETE FROM sessions WHERE user_id = ?", userID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM api_tokens WHERE user_id = ?", userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) SetShareLinks(userID string, allow bool) error {
	result, err := s.db.Exec("UPDATE users SET allow_create_share_links = ? WHERE id = ?", boolInt(allow), userID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errors.New("user not found")
	}
	return nil
}

func (s *SQLiteStore) RecordLastIP(userID, ip string) error {
	_, err := s.db.Exec("UPDATE users SET last_ip = ? WHERE id = ? AND last_ip != ?", ip, userID, ip)
	return err
}

func (s *SQLiteStore) LanguageForUser(userID string) (string, error) {
	var language string
	err := s.db.QueryRow(`SELECT COALESCE(language, '') FROM users WHERE id = ?`, userID).Scan(&language)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return strings.TrimSpace(language), err
}

func (s *SQLiteStore) SetLanguage(userID, language string) error {
	result, err := s.db.Exec("UPDATE users SET language = ? WHERE id = ?", strings.TrimSpace(language), userID)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return errors.New("user not found")
	}
	return nil
}

const (
	bookRequestTitleMax   = 300
	bookRequestAuthorMax  = 200
	bookRequestNotesMax   = 2000
	bookRequestMessageMax = 2000
)

func normalizeBookRequestFields(title, author, notes string) (string, string, string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", "", "", errors.New("a book title is required")
	}
	if len(title) > bookRequestTitleMax {
		return "", "", "", fmt.Errorf("the title must not exceed %d characters", bookRequestTitleMax)
	}
	author = strings.TrimSpace(author)
	if len(author) > bookRequestAuthorMax {
		return "", "", "", fmt.Errorf("the author must not exceed %d characters", bookRequestAuthorMax)
	}
	notes = strings.TrimSpace(notes)
	if len(notes) > bookRequestNotesMax {
		return "", "", "", fmt.Errorf("the notes must not exceed %d characters", bookRequestNotesMax)
	}
	return title, author, notes, nil
}

const bookRequestColumns = `id, user_id, title, author, notes, status, book_id, message, created_at, updated_at`

func scanBookRequest(row scanner) (*BookRequest, error) {
	var request BookRequest
	var createdAt, updatedAt int64
	if err := row.Scan(
		&request.ID, &request.UserID, &request.Title, &request.Author, &request.Notes,
		&request.Status, &request.BookID, &request.Message, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	request.CreatedAt = time.Unix(createdAt, 0).UTC()
	request.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return &request, nil
}

func (s *SQLiteStore) CreateBookRequest(userID, title, author, notes string) (*BookRequest, error) {
	title, author, notes, err := normalizeBookRequestFields(title, author, notes)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var active int
	if err := tx.QueryRow("SELECT active FROM users WHERE id = ?", userID).Scan(&active); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("user not found")
		}
		return nil, err
	}
	if active != 1 {
		return nil, ErrInactive
	}
	id, err := randomToken(s.rand, 16)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if _, err := tx.Exec(`INSERT INTO book_requests
		(id, user_id, title, author, notes, status, book_id, message, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'pending', '', '', ?, ?)`,
		id, userID, title, author, notes, now.Unix(), now.Unix()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &BookRequest{
		ID: id, UserID: userID, Title: title, Author: author, Notes: notes,
		Status: BookRequestPending, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *SQLiteStore) BookRequestsForUser(userID string) ([]BookRequest, error) {
	rows, err := s.db.Query(`SELECT `+bookRequestColumns+` FROM book_requests
		WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	requests := make([]BookRequest, 0)
	for rows.Next() {
		request, err := scanBookRequest(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, *request)
	}
	return requests, rows.Err()
}

func (s *SQLiteStore) BookRequestsAll() ([]BookRequest, error) {
	rows, err := s.db.Query(`SELECT ` + prefixedBookRequestColumns + ` FROM book_requests r
		JOIN users u ON u.id = r.user_id
		ORDER BY CASE r.status WHEN 'pending' THEN 0 ELSE 1 END, r.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	requests := make([]BookRequest, 0)
	for rows.Next() {
		request, err := scanBookRequestWithRequester(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, *request)
	}
	return requests, rows.Err()
}

const prefixedBookRequestColumns = `r.id, r.user_id, r.title, r.author, r.notes, r.status,
	r.book_id, r.message, r.created_at, r.updated_at, u.name, u.email`

func scanBookRequestWithRequester(row scanner) (*BookRequest, error) {
	var request BookRequest
	var createdAt, updatedAt int64
	if err := row.Scan(
		&request.ID, &request.UserID, &request.Title, &request.Author, &request.Notes,
		&request.Status, &request.BookID, &request.Message, &createdAt, &updatedAt,
		&request.RequesterName, &request.RequesterEmail,
	); err != nil {
		return nil, err
	}
	request.CreatedAt = time.Unix(createdAt, 0).UTC()
	request.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return &request, nil
}

func (s *SQLiteStore) PendingBookRequestCount() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM book_requests WHERE status = 'pending'").Scan(&count)
	return count, err
}

func (s *SQLiteStore) PendingBookRequestCountForUser(userID string) (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM book_requests WHERE user_id = ? AND status = 'pending'", userID).Scan(&count)
	return count, err
}

func (s *SQLiteStore) ResolveBookRequest(requestID string, status BookRequestStatus, bookID, message string) error {
	if !status.Valid() || status == BookRequestPending {
		return errors.New("invalid book request status")
	}
	bookID = strings.TrimSpace(bookID)
	message = strings.TrimSpace(message)
	if status == BookRequestUnavailable && message == "" {
		return errors.New("explain why the book could not be found")
	}
	if len(message) > bookRequestMessageMax {
		return fmt.Errorf("the message must not exceed %d characters", bookRequestMessageMax)
	}
	if len(bookID) > 64 {
		return errors.New("the book reference is invalid")
	}
	result, err := s.db.Exec(`UPDATE book_requests
		SET status = ?, book_id = ?, message = ?, updated_at = ?
		WHERE id = ?`,
		status, bookID, message, s.now().UTC().Unix(), requestID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errors.New("book request not found")
	}
	return nil
}

const userColumns = `id, email, name, role, active, password_hash,
	COALESCE(google_subject, ''), COALESCE(last_ip, ''), allow_create_share_links,
	created_at, updated_at, last_login_at`

const prefixedUserColumns = `u.id, u.email, u.name, u.role, u.active, u.password_hash,
	COALESCE(u.google_subject, ''), COALESCE(u.last_ip, ''), u.allow_create_share_links,
	u.created_at, u.updated_at, u.last_login_at`

type scanner interface {
	Scan(dest ...interface{}) error
}

func (s *SQLiteStore) queryUser(query string, args ...interface{}) (*User, error) {
	return queryUserWith(s.db, query, args...)
}

type queryRower interface {
	QueryRow(query string, args ...interface{}) *sql.Row
}

func queryUserWith(db queryRower, query string, args ...interface{}) (*User, error) {
	return scanUser(db.QueryRow(query, args...))
}

func scanUser(row scanner) (*User, error) {
	var user User
	var active int
	var allowShare int
	var createdAt, updatedAt int64
	var lastLogin sql.NullInt64
	if err := row.Scan(
		&user.ID, &user.Email, &user.Name, &user.Role, &active, &user.PasswordHash,
		&user.GoogleSubject, &user.LastIP, &allowShare, &createdAt, &updatedAt, &lastLogin,
	); err != nil {
		return nil, err
	}
	user.Active = active == 1
	user.AllowShare = allowShare == 1
	user.CreatedAt = time.Unix(createdAt, 0).UTC()
	user.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	if lastLogin.Valid {
		value := time.Unix(lastLogin.Int64, 0).UTC()
		user.LastLoginAt = &value
	}
	return &user, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
