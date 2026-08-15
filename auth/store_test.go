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
	if version != 9 {
		t.Fatalf("schema version = %d, want 9", version)
	}
}

func TestReadingItemsAreEditableTaggableAndIsolated(t *testing.T) {
	store := newTestStore(t)
	owner, err := store.RegisterEmail("owner-notes@example.com", "Owner", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.RegisterEmail("other-notes@example.com", "Other", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, time.August, 10, 1, 2, 3, 0, time.UTC)
	store.now = func() time.Time { return clock }
	item, err := store.CreateReadingItem(
		owner.ID, "book-one", ReadingItemNote, "epubcfi(/6/4!/4/2)", "Chapter 1 · 12%",
		"Important idea", "Initial note", "Selected book text", []string{"Research", " research ", "Quote"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if item.Kind != ReadingItemNote || len(item.Tags) != 2 || item.Tags[0] != "Research" {
		t.Fatalf("created item=%#v", item)
	}
	items, err := store.ReadingItems(owner.ID, "book-one", 20)
	if err != nil || len(items) != 1 || len(items[0].Tags) != 2 {
		t.Fatalf("owner items=%#v err=%v", items, err)
	}
	otherItems, err := store.ReadingItems(other.ID, "book-one", 20)
	if err != nil || len(otherItems) != 0 {
		t.Fatalf("other items=%#v err=%v", otherItems, err)
	}
	if _, err := store.ReadingItemForUser(other.ID, item.ID); !errors.Is(err, ErrReadingItemNotFound) {
		t.Fatalf("another user read item: %v", err)
	}
	if _, err := store.UpdateReadingItem(other.ID, item.ID, "Stolen", "Changed", nil); !errors.Is(err, ErrReadingItemNotFound) {
		t.Fatalf("another user updated item: %v", err)
	}
	if err := store.DeleteReadingItem(other.ID, item.ID); !errors.Is(err, ErrReadingItemNotFound) {
		t.Fatalf("another user deleted item: %v", err)
	}

	clock = clock.Add(time.Minute)
	updated, err := store.UpdateReadingItem(owner.ID, item.ID, "Revised idea", "Updated note", []string{"Review"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Revised idea" || updated.Body != "Updated note" || len(updated.Tags) != 1 || updated.Tags[0] != "Review" {
		t.Fatalf("updated item=%#v", updated)
	}
	if !updated.UpdatedAt.Equal(clock) {
		t.Fatalf("updated at=%v", updated.UpdatedAt)
	}
	if err := store.DeleteReadingItem(owner.ID, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadingItemForUser(owner.ID, item.ID); !errors.Is(err, ErrReadingItemNotFound) {
		t.Fatalf("deleted item remains: %v", err)
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

func TestAPITokensAreHashedRevocableAndBoundToActiveUsers(t *testing.T) {
	store := newTestStore(t)
	admin, err := store.RegisterEmail("admin-token@example.com", "Admin", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := store.RegisterEmail("reader-token@example.com", "Reader", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return clock }
	raw, token, err := store.CreateAPIToken(reader.ID, "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "bbk_") || token.Name != "laptop" || !token.CreatedAt.Equal(clock) {
		t.Fatalf("created token raw=%q metadata=%#v", raw, token)
	}
	var rawMatches int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM api_tokens WHERE token_hash = ?", raw).Scan(&rawMatches); err != nil {
		t.Fatal(err)
	}
	if rawMatches != 0 {
		t.Fatal("raw API token was persisted")
	}
	if _, _, err := store.CreateAPIToken(reader.ID, "LAPTOP", nil); !errors.Is(err, ErrAPITokenNameExists) {
		t.Fatalf("duplicate token name error=%v", err)
	}
	clock = clock.Add(time.Minute)
	authenticated, used, err := store.UserForAPIToken(raw)
	if err != nil || authenticated == nil || authenticated.ID != reader.ID || used.LastUsedAt == nil || !used.LastUsedAt.Equal(clock) {
		t.Fatalf("API authentication user=%#v token=%#v err=%v", authenticated, used, err)
	}
	items, err := store.APITokens(reader.ID)
	if err != nil || len(items) != 1 || items[0].Name != "laptop" {
		t.Fatalf("token list=%#v err=%v", items, err)
	}
	if err := store.RevokeAPIToken(admin.ID, "laptop"); !errors.Is(err, ErrAPITokenNotFound) {
		t.Fatalf("another user revoked token: %v", err)
	}
	if err := store.RevokeCurrentAPIToken(raw); err != nil {
		t.Fatal(err)
	}
	if user, item, err := store.UserForAPIToken(raw); err != nil || user != nil || item != nil {
		t.Fatalf("revoked token authenticated: user=%#v token=%#v err=%v", user, item, err)
	}

	raw, _, err = store.CreateAPIToken(reader.ID, "tablet", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetPassword(reader.ID, "a different secure password"); err != nil {
		t.Fatal(err)
	}
	if user, _, err := store.UserForAPIToken(raw); err != nil || user != nil {
		t.Fatalf("password-reset token authenticated: user=%#v err=%v", user, err)
	}

	raw, _, err = store.CreateAPIToken(reader.ID, "phone", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateUser(reader.ID, RoleReader, false); err != nil {
		t.Fatal(err)
	}
	if user, _, err := store.UserForAPIToken(raw); err != nil || user != nil {
		t.Fatalf("disabled-user token authenticated: user=%#v err=%v", user, err)
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

func TestPasswordResetShareLinksAndLastIP(t *testing.T) {
	store := newTestStore(t)
	user, err := store.RegisterEmail("share@example.com", "Share", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !user.AllowShare || user.LastIP != "" {
		t.Fatalf("defaults user=%#v", user)
	}
	if err := store.SetShareLinks(user.ID, false); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.UserByID(user.ID)
	if err != nil || reloaded.AllowShare {
		t.Fatalf("share links still allowed: user=%v err=%v", reloaded, err)
	}
	if err := store.RecordLastIP(user.ID, "203.0.113.7"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetPassword(user.ID, "a brand new password"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateEmail(user.Email, "a brand new password"); err != nil {
		t.Fatalf("authenticate with new password: %v", err)
	}
	if _, err := store.AuthenticateEmail(user.Email, "correct horse battery staple"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password still valid: %v", err)
	}
	reloaded, err = store.UserByID(user.ID)
	if err != nil || reloaded.LastIP != "203.0.113.7" {
		t.Fatalf("last IP not recorded: user=%v err=%v", reloaded, err)
	}
	clock := time.Date(2026, time.August, 10, 2, 3, 4, 0, time.UTC)
	store.now = func() time.Time { return clock }
	if err := store.RecordBookRead(user.ID, "book-a"); err != nil {
		t.Fatal(err)
	}
	activities, err := store.RecentBooks(user.ID, 5)
	if err != nil || len(activities) != 1 || activities[0].BookID != "book-a" {
		t.Fatalf("recent books=%v err=%v", activities, err)
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

func TestBookRequestLifecycleAndResolution(t *testing.T) {
	store := newTestStore(t)
	reader, err := store.RegisterEmail("requester@example.com", "Requester", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	request, err := store.CreateBookRequest(reader.ID, "  The Martian  ", "Andy Weir", "English paperback, 2014")
	if err != nil {
		t.Fatal(err)
	}
	if request.Title != "The Martian" || request.Author != "Andy Weir" || request.Status != BookRequestPending {
		t.Fatalf("unexpected request: %#v", request)
	}
	if _, err := store.CreateBookRequest(reader.ID, "   ", "", ""); err == nil {
		t.Fatal("expected a title to be required")
	}
	if _, err := store.CreateBookRequest(reader.ID, strings.Repeat("x", 301), "", ""); err == nil {
		t.Fatal("expected overlong titles to be rejected")
	}

	mine, err := store.BookRequestsForUser(reader.ID)
	if err != nil || len(mine) != 1 {
		t.Fatalf("requests for user = %d, %v", len(mine), err)
	}
	count, err := store.PendingBookRequestCount()
	if err != nil || count != 1 {
		t.Fatalf("pending count = %d, %v", count, err)
	}
	userCount, err := store.PendingBookRequestCountForUser(reader.ID)
	if err != nil || userCount != 1 {
		t.Fatalf("pending count for user = %d, %v", userCount, err)
	}
	all, err := store.BookRequestsAll()
	if err != nil || len(all) != 1 {
		t.Fatalf("all requests = %d, %v", len(all), err)
	}
	if all[0].RequesterName != "Requester" || all[0].RequesterEmail != "requester@example.com" {
		t.Fatalf("requester details = %q %q", all[0].RequesterName, all[0].RequesterEmail)
	}

	if err := store.ResolveBookRequest(request.ID, BookRequestPending, "", ""); err == nil {
		t.Fatal("expected pending to be an invalid resolution")
	}
	if err := store.ResolveBookRequest(request.ID, BookRequestUnavailable, "", ""); err == nil {
		t.Fatal("expected a message for an unavailable resolution")
	}
	if err := store.ResolveBookRequest(request.ID, BookRequestUnavailable, "", "Out of print, not available anywhere."); err != nil {
		t.Fatalf("resolve unavailable: %v", err)
	}
	mine, err = store.BookRequestsForUser(reader.ID)
	if err != nil || len(mine) != 1 {
		t.Fatalf("requests after resolution = %d, %v", len(mine), err)
	}
	if mine[0].Status != BookRequestUnavailable || mine[0].Message != "Out of print, not available anywhere." {
		t.Fatalf("unavailable resolution not stored: %#v", mine[0])
	}

	second, err := store.CreateBookRequest(reader.ID, "Dune", "Frank Herbert", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveBookRequest(second.ID, BookRequestAdded, "abc123", "Enjoy!"); err != nil {
		t.Fatalf("resolve added: %v", err)
	}
	mine, err = store.BookRequestsForUser(reader.ID)
	if err != nil || len(mine) != 2 {
		t.Fatalf("requests = %d, %v", len(mine), err)
	}
	var added *BookRequest
	for i := range mine {
		if mine[i].ID == second.ID {
			added = &mine[i]
		}
	}
	if added == nil || added.Status != BookRequestAdded || added.BookID != "abc123" || added.Message != "Enjoy!" {
		t.Fatalf("added resolution not stored: %#v", added)
	}
	if err := store.ResolveBookRequest("missing", BookRequestAdded, "abc123", ""); err == nil {
		t.Fatal("expected resolving an unknown request to fail")
	}
}

func TestUpdateProfileValidationAndPersistence(t *testing.T) {
	store := newTestStore(t)
	reader, err := store.RegisterEmail("profile@example.com", "Profile User", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateProfile(reader.ID, "  Bookworm  ", "  Loves sci-fi.  ", "  Shanghai  "); err != nil {
		t.Fatalf("update profile: %v", err)
	}
	updated, err := store.UserByID(reader.ID)
	if err != nil || updated == nil {
		t.Fatalf("reload user: %v", err)
	}
	if updated.DisplayName != "Bookworm" || updated.Bio != "Loves sci-fi." || updated.Location != "Shanghai" {
		t.Fatalf("profile fields = %q %q %q", updated.DisplayName, updated.Bio, updated.Location)
	}
	if err := store.UpdateProfile(reader.ID, strings.Repeat("x", 101), "", ""); err == nil {
		t.Fatal("expected overlong display names to be rejected")
	}
	if err := store.UpdateProfile(reader.ID, "", strings.Repeat("x", 1001), ""); err == nil {
		t.Fatal("expected overlong bio to be rejected")
	}
	if err := store.UpdateProfile(reader.ID, "", "", strings.Repeat("x", 201)); err == nil {
		t.Fatal("expected overlong location to be rejected")
	}
	if err := store.UpdateProfile("missing", "x", "", ""); err == nil {
		t.Fatal("expected unknown user to be rejected")
	}
}
