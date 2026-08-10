package auth

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestEmailRegistrationAuthenticationAndPersistence(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	admin, err := store.RegisterEmail(" Admin@Example.com ", "Admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if admin.Role != RoleAdmin || admin.Email != "admin@example.com" {
		t.Fatalf("unexpected bootstrap user: %#v", admin)
	}
	reader, err := store.RegisterEmail("reader@example.com", "Reader", "a sufficiently long password")
	if err != nil {
		t.Fatal(err)
	}
	if reader.Role != RoleReader {
		t.Fatalf("expected reader role, got %q", reader.Role)
	}
	if _, err := store.AuthenticateEmail(admin.Email, "wrong password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials, got %v", err)
	}
	if _, err := store.AuthenticateEmail(admin.Email, "correct horse battery staple"); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewSQLiteStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	count, err := reloaded.CountUsers()
	if err != nil || count != 2 {
		t.Fatalf("persisted users count=%d err=%v", count, err)
	}
	var passwordHash string
	if err := reloaded.db.QueryRow("SELECT password_hash FROM users WHERE email = ?", admin.Email).Scan(&passwordHash); err != nil {
		t.Fatal(err)
	}
	if passwordHash == "" || strings.Contains(passwordHash, "correct horse battery staple") {
		t.Fatal("password was not stored as a one-way hash")
	}
	info, err := os.Stat(reloaded.Path())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("database mode=%o", info.Mode().Perm())
	}
}

func TestSettingsMigrationDefaultsAndPWAName(t *testing.T) {
	store := newTestStore(t)
	settings, err := store.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.SiteName != "MicsBook" || settings.PWAName != "MicsBook" {
		t.Fatalf("default names = site %q, PWA %q", settings.SiteName, settings.PWAName)
	}
	settings.SiteName = "MicsBook Library"
	settings.PWAName = "MicsBook Reader"
	if err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	updated, err := store.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if updated.SiteName != settings.SiteName || updated.PWAName != settings.PWAName {
		t.Fatalf("updated names = %#v", updated)
	}
	var version int
	if err := store.db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 3 {
		t.Fatalf("schema version = %d", version)
	}
}

func TestSessionsPersistOnlyTokenHashes(t *testing.T) {
	store := newTestStore(t)
	user, err := store.RegisterEmail("admin@example.com", "Admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.NewSession(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	sessionUser, err := store.UserForSession(token)
	if err != nil || sessionUser == nil {
		t.Fatalf("valid session rejected: user=%v err=%v", sessionUser, err)
	}
	var rawMatches int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM sessions WHERE token_hash = ?", token).Scan(&rawMatches); err != nil {
		t.Fatal(err)
	}
	if rawMatches != 0 {
		t.Fatal("raw session token was persisted")
	}
	if err := store.DeleteSession(token); err != nil {
		t.Fatal(err)
	}
	sessionUser, err = store.UserForSession(token)
	if err != nil || sessionUser != nil {
		t.Fatalf("deleted session remains valid: user=%v err=%v", sessionUser, err)
	}
}

func TestPrivateBookListsTagsAndRecentReadsAreIsolated(t *testing.T) {
	store := newTestStore(t)
	owner, err := store.RegisterEmail("owner@example.com", "Owner", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.RegisterEmail("other@example.com", "Other", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}

	clock := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }
	if err := store.RecordBookRead(owner.ID, "book-one"); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Minute)
	if err := store.RecordBookRead(owner.ID, "book-two"); err != nil {
		t.Fatal(err)
	}
	recent, err := store.RecentBookIDs(owner.ID, 10)
	if err != nil || len(recent) != 2 || recent[0] != "book-two" || recent[1] != "book-one" {
		t.Fatalf("recent=%v err=%v", recent, err)
	}
	otherRecent, err := store.RecentBookIDs(other.ID, 10)
	if err != nil || len(otherRecent) != 0 {
		t.Fatalf("other recent=%v err=%v", otherRecent, err)
	}

	list, err := store.CreateBookList(owner.ID, " Weekend Favorites ")
	if err != nil {
		t.Fatal(err)
	}
	if list.Name != "Weekend Favorites" {
		t.Fatalf("normalized list name=%q", list.Name)
	}
	if _, err := store.CreateBookList(owner.ID, "weekend favorites"); !errors.Is(err, ErrBookListNameExists) {
		t.Fatalf("case-insensitive duplicate error=%v", err)
	}
	if err := store.AddBookToList(owner.ID, list.ID, "book-one"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddBookToList(owner.ID, list.ID, "book-one"); err != nil {
		t.Fatal(err)
	}
	ids, err := store.BookIDsForList(owner.ID, list.ID)
	if err != nil || len(ids) != 1 || ids[0] != "book-one" {
		t.Fatalf("list IDs=%v err=%v", ids, err)
	}
	if _, err := store.BookListForUser(other.ID, list.ID); !errors.Is(err, ErrBookListNotFound) {
		t.Fatalf("another user read private list: %v", err)
	}
	if err := store.AddBookToList(other.ID, list.ID, "book-two"); !errors.Is(err, ErrBookListNotFound) {
		t.Fatalf("another user changed private list: %v", err)
	}

	if err := store.AddBookTag(owner.ID, "book-one", " Science Fiction "); err != nil {
		t.Fatal(err)
	}
	if err := store.AddBookTag(owner.ID, "book-one", "science fiction"); err != nil {
		t.Fatal(err)
	}
	tags, err := store.TagsForBook(owner.ID, "book-one")
	if err != nil || len(tags) != 1 || tags[0] != "Science Fiction" {
		t.Fatalf("book tags=%v err=%v", tags, err)
	}
	otherTags, err := store.TagsForBook(other.ID, "book-one")
	if err != nil || len(otherTags) != 0 {
		t.Fatalf("other tags=%v err=%v", otherTags, err)
	}
	if err := store.RemoveBookTag(owner.ID, "book-one", "SCIENCE FICTION"); err != nil {
		t.Fatal(err)
	}
	tags, err = store.TagsForBook(owner.ID, "book-one")
	if err != nil || len(tags) != 0 {
		t.Fatalf("removed tags=%v err=%v", tags, err)
	}
}

func TestLastActiveAdminIsProtected(t *testing.T) {
	store := newTestStore(t)
	admin, err := store.RegisterEmail("admin@example.com", "Admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateUser(admin.ID, RoleReader, true); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("expected last-admin error, got %v", err)
	}
	second, err := store.RegisterEmail("second@example.com", "Second", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateUser(second.ID, RoleAdmin, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateUser(admin.ID, RoleReader, true); err != nil {
		t.Fatalf("demote with second admin: %v", err)
	}
}

func TestGoogleCanBootstrapAdminAndHonorsRegistrationPolicy(t *testing.T) {
	store := newTestStore(t)
	admin, err := store.UpsertGoogle("first@example.com", "First", "google-first", false)
	if err != nil {
		t.Fatal(err)
	}
	if admin.Role != RoleAdmin {
		t.Fatalf("first Google role=%s", admin.Role)
	}
	linked, err := store.UpsertGoogle(admin.Email, admin.Name, "google-first", false)
	if err != nil || linked.ID != admin.ID {
		t.Fatalf("Google relogin user=%v err=%v", linked, err)
	}
	googleReader, err := store.UpsertGoogle("reader@example.com", "Reader", "google-reader", false)
	if err != nil {
		t.Fatal(err)
	}
	if googleReader.Role != RoleReader {
		t.Fatalf("expected reader, got %s", googleReader.Role)
	}
	settings, err := store.Settings()
	if err != nil {
		t.Fatal(err)
	}
	settings.RegistrationOpen = false
	if err := store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertGoogle("closed@example.com", "Closed", "google-closed", false); err == nil {
		t.Fatal("Google registration bypassed closed registration")
	}
}

func TestPBKDF2KnownVector(t *testing.T) {
	got := pbkdf2SHA256([]byte("password"), []byte("salt"), 2, 32)
	want := []byte{
		0xae, 0x4d, 0x0c, 0x95, 0xaf, 0x6b, 0x46, 0xd3,
		0x2d, 0x0a, 0xdf, 0xf9, 0x28, 0xf0, 0x6d, 0xd0,
		0x2a, 0x30, 0x3f, 0x8e, 0xf3, 0xc2, 0x51, 0xdf,
		0xd6, 0xe2, 0xd8, 0x5a, 0x95, 0x47, 0x4c, 0x43,
	}
	if !hmacEqual(got, want) {
		t.Fatalf("PBKDF2 mismatch: %x", got)
	}
}

func hmacEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
