package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

type Role string

const (
	RoleReader  Role = "reader"
	RoleManager Role = "manager"
	RoleAdmin   Role = "admin"

	passwordIterations = 210000
	passwordKeyBytes   = 32
	passwordSaltBytes  = 16
	sessionDuration    = 30 * 24 * time.Hour
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailExists        = errors.New("an account with that email already exists")
	ErrInactive           = errors.New("account is disabled")
	ErrInvalidRole        = errors.New("invalid role")
	ErrLastAdmin          = errors.New("the last active administrator cannot be demoted or disabled")
	ErrIdentityConflict   = errors.New("the Google identity belongs to another account")
	ErrBookListNotFound   = errors.New("book list not found")
	ErrBookListNameExists = errors.New("a book list with that name already exists")
)

type User struct {
	ID            string
	Email         string
	Name          string
	Role          Role
	Active        bool
	PasswordHash  string
	GoogleSubject string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastLoginAt   *time.Time
}

type Settings struct {
	SiteName           string
	PWAName            string
	RegistrationOpen   bool
	AnonymousBookLinks bool
}

type BookList struct {
	ID           string
	UserID       string
	Name         string
	BookCount    int
	ContainsBook bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type BookTag struct {
	Name      string
	BookCount int
}

// Store is the persistence boundary used by the HTTP layer. SQLite is the
// default implementation, while a future managed database can implement the
// same contract without changing authentication or administration handlers.
type Store interface {
	Path() string
	Close() error
	CountUsers() (int, error)
	CanRegister() (bool, error)
	Settings() (Settings, error)
	UpdateSettings(Settings) error
	RegisterEmail(email, name, password string) (*User, error)
	AuthenticateEmail(email, password string) (*User, error)
	UpsertGoogle(email, name, subject string, emailAuthoritative bool) (*User, error)
	NewSession(userID string) (string, error)
	UserForSession(token string) (*User, error)
	DeleteSession(token string) error
	Users() ([]User, error)
	UserByID(id string) (*User, error)
	UserByEmail(email string) (*User, error)
	UpdateUser(id string, role Role, active bool) (*User, error)
	RecordBookRead(userID, bookID string) error
	RecentBookIDs(userID string, limit int) ([]string, error)
	BookListsForUser(userID string) ([]BookList, error)
	BookListsForBook(userID, bookID string) ([]BookList, error)
	BookListForUser(userID, listID string) (*BookList, error)
	BookIDsForList(userID, listID string) ([]string, error)
	CreateBookList(userID, name string) (*BookList, error)
	DeleteBookList(userID, listID string) error
	AddBookToList(userID, listID, bookID string) error
	RemoveBookFromList(userID, listID, bookID string) error
	TagsForUser(userID string) ([]BookTag, error)
	TagsForBook(userID, bookID string) ([]string, error)
	BookIDsForTag(userID, tag string) ([]string, error)
	AddBookTag(userID, bookID, tag string) error
	RemoveBookTag(userID, bookID, tag string) error
}

func DefaultSettings() Settings {
	return Settings{
		SiteName:           "MicsBook",
		PWAName:            "MicsBook",
		RegistrationOpen:   true,
		AnonymousBookLinks: true,
	}
}

func (r Role) Valid() bool {
	return r == RoleReader || r == RoleManager || r == RoleAdmin
}

func (r Role) Allows(required Role) bool {
	return roleRank(r) >= roleRank(required)
}

func roleRank(role Role) int {
	switch role {
	case RoleAdmin:
		return 3
	case RoleManager:
		return 2
	case RoleReader:
		return 1
	default:
		return 0
	}
}

func normalizeEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || len(email) > 254 ||
		strings.ContainsAny(email, " \t\r\n<>,;:\\()[]\"") ||
		strings.HasPrefix(parts[0], ".") || strings.HasSuffix(parts[0], ".") ||
		strings.HasPrefix(parts[1], ".") || strings.HasSuffix(parts[1], ".") ||
		!strings.Contains(parts[1], ".") {
		return "", errors.New("enter a valid email address")
	}
	return email, nil
}

func normalizeName(name, email string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = strings.SplitN(email, "@", 2)[0]
	}
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}

func randomToken(reader io.Reader, size int) (string, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(reader, buf); err != nil {
		return "", fmt.Errorf("generate secure random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func tokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func hashPassword(password string, reader io.Reader) (string, error) {
	salt := make([]byte, passwordSaltBytes)
	if _, err := io.ReadFull(reader, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := pbkdf2SHA256([]byte(password), salt, passwordIterations, passwordKeyBytes)
	return fmt.Sprintf(
		"pbkdf2-sha256$%d$%s$%s",
		passwordIterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 || iterations > 2000000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) < 8 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(expected) < 16 {
		return false
	}
	actual := pbkdf2SHA256([]byte(password), salt, iterations, len(expected))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLength int) []byte {
	hashLength := sha256.Size
	blocks := (keyLength + hashLength - 1) / hashLength
	derived := make([]byte, 0, blocks*hashLength)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		var counter [4]byte
		binary.BigEndian.PutUint32(counter[:], uint32(block))
		mac.Write(counter[:])
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		derived = append(derived, t...)
	}
	return derived[:keyLength]
}
