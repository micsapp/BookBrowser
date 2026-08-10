package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

func normalizeReadingText(value, field string, maximum int, required bool) (string, error) {
	value = strings.TrimSpace(value)
	if required && value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if utf8.RuneCountInString(value) > maximum {
		return "", fmt.Errorf("%s must not exceed %d characters", field, maximum)
	}
	return value, nil
}

func normalizeReadingTags(tags []string) ([]string, error) {
	result := make([]string, 0, len(tags))
	seen := make(map[string]bool)
	for _, raw := range tags {
		tag, err := normalizeTag(raw)
		if err != nil {
			if strings.TrimSpace(raw) == "" {
				continue
			}
			return nil, err
		}
		key := strings.ToLower(tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, tag)
		if len(result) > 12 {
			return nil, errors.New("use no more than 12 tags on one bookmark or note")
		}
	}
	return result, nil
}

func validateReadingItem(bookID string, kind ReadingItemKind, locator, locatorLabel, title, body, excerpt string, tags []string) (ReadingItem, error) {
	bookID, err := normalizeBookID(bookID)
	if err != nil {
		return ReadingItem{}, err
	}
	if !kind.Valid() {
		return ReadingItem{}, errors.New("invalid reading item kind")
	}
	locator, err = normalizeReadingText(locator, "location", 4000, true)
	if err != nil {
		return ReadingItem{}, err
	}
	locatorLabel, err = normalizeReadingText(locatorLabel, "location label", 200, false)
	if err != nil {
		return ReadingItem{}, err
	}
	title, err = normalizeReadingText(title, "title", 200, false)
	if err != nil {
		return ReadingItem{}, err
	}
	if title == "" {
		if kind == ReadingItemBookmark {
			title = "Bookmark"
		} else {
			title = "Note"
		}
	}
	body, err = normalizeReadingText(body, "note", 20000, false)
	if err != nil {
		return ReadingItem{}, err
	}
	excerpt, err = normalizeReadingText(excerpt, "excerpt", 2000, false)
	if err != nil {
		return ReadingItem{}, err
	}
	tags, err = normalizeReadingTags(tags)
	if err != nil {
		return ReadingItem{}, err
	}
	return ReadingItem{
		BookID: bookID, Kind: kind, Locator: locator, LocatorLabel: locatorLabel,
		Title: title, Body: body, Excerpt: excerpt, Tags: tags,
	}, nil
}

func (s *SQLiteStore) ReadingItems(userID, bookID string, limit int) ([]ReadingItem, error) {
	if limit < 1 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	query := `SELECT id, user_id, book_id, kind, locator, locator_label, title, body, excerpt, created_at, updated_at
		FROM user_reading_items WHERE user_id = ?`
	args := []interface{}{userID}
	if strings.TrimSpace(bookID) != "" {
		normalized, err := normalizeBookID(bookID)
		if err != nil {
			return nil, err
		}
		query += " AND book_id = ?"
		args = append(args, normalized)
	}
	query += " ORDER BY updated_at DESC, created_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	items := make([]ReadingItem, 0)
	for rows.Next() {
		item, err := scanReadingItem(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if err := s.attachReadingTags(userID, items); err != nil {
		return nil, err
	}
	return items, nil
}

func scanReadingItem(scanner interface{ Scan(...interface{}) error }) (ReadingItem, error) {
	var item ReadingItem
	var createdAt, updatedAt int64
	err := scanner.Scan(
		&item.ID, &item.UserID, &item.BookID, &item.Kind, &item.Locator,
		&item.LocatorLabel, &item.Title, &item.Body, &item.Excerpt, &createdAt, &updatedAt,
	)
	item.CreatedAt = time.Unix(createdAt, 0).UTC()
	item.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return item, err
}

func (s *SQLiteStore) attachReadingTags(userID string, items []ReadingItem) error {
	if len(items) == 0 {
		return nil
	}
	byID := make(map[string]*ReadingItem, len(items))
	for index := range items {
		byID[items[index].ID] = &items[index]
	}
	rows, err := s.db.Query(`SELECT t.item_id, t.tag FROM user_reading_item_tags t
		JOIN user_reading_items i ON i.id = t.item_id
		WHERE i.user_id = ? ORDER BY t.tag COLLATE NOCASE`, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var itemID, tag string
		if err := rows.Scan(&itemID, &tag); err != nil {
			return err
		}
		if item := byID[itemID]; item != nil {
			item.Tags = append(item.Tags, tag)
		}
	}
	return rows.Err()
}

func (s *SQLiteStore) ReadingItemForUser(userID, itemID string) (*ReadingItem, error) {
	item, err := scanReadingItem(s.db.QueryRow(`SELECT id, user_id, book_id, kind, locator, locator_label,
		title, body, excerpt, created_at, updated_at FROM user_reading_items
		WHERE user_id = ? AND id = ?`, userID, strings.TrimSpace(itemID)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReadingItemNotFound
	}
	if err != nil {
		return nil, err
	}
	items := []ReadingItem{item}
	if err := s.attachReadingTags(userID, items); err != nil {
		return nil, err
	}
	return &items[0], nil
}

func (s *SQLiteStore) CreateReadingItem(userID, bookID string, kind ReadingItemKind, locator, locatorLabel, title, body, excerpt string, tags []string) (*ReadingItem, error) {
	item, err := validateReadingItem(bookID, kind, locator, locatorLabel, title, body, excerpt, tags)
	if err != nil {
		return nil, err
	}
	item.ID, err = randomToken(s.rand, 16)
	if err != nil {
		return nil, err
	}
	item.UserID = userID
	item.CreatedAt = s.now().UTC()
	item.UpdatedAt = item.CreatedAt
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO user_reading_items
		(id, user_id, book_id, kind, locator, locator_label, title, body, excerpt, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, item.UserID, item.BookID, item.Kind,
		item.Locator, item.LocatorLabel, item.Title, item.Body, item.Excerpt, item.CreatedAt.Unix(), item.UpdatedAt.Unix()); err != nil {
		return nil, err
	}
	if err := insertReadingTags(tx, item.ID, item.Tags, item.CreatedAt.Unix()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *SQLiteStore) UpdateReadingItem(userID, itemID, title, body string, tags []string) (*ReadingItem, error) {
	title, err := normalizeReadingText(title, "title", 200, false)
	if err != nil {
		return nil, err
	}
	body, err = normalizeReadingText(body, "note", 20000, false)
	if err != nil {
		return nil, err
	}
	tags, err = normalizeReadingTags(tags)
	if err != nil {
		return nil, err
	}
	item, err := s.ReadingItemForUser(userID, itemID)
	if err != nil {
		return nil, err
	}
	if title == "" {
		if item.Kind == ReadingItemBookmark {
			title = "Bookmark"
		} else {
			title = "Note"
		}
	}
	now := s.now().UTC()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE user_reading_items SET title = ?, body = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`, title, body, now.Unix(), strings.TrimSpace(itemID), userID)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected == 0 {
		if err != nil {
			return nil, err
		}
		return nil, ErrReadingItemNotFound
	}
	if _, err := tx.Exec(`DELETE FROM user_reading_item_tags WHERE item_id = ?`, strings.TrimSpace(itemID)); err != nil {
		return nil, err
	}
	if err := insertReadingTags(tx, strings.TrimSpace(itemID), tags, now.Unix()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ReadingItemForUser(userID, itemID)
}

func insertReadingTags(tx *sql.Tx, itemID string, tags []string, timestamp int64) error {
	for _, tag := range tags {
		if _, err := tx.Exec(`INSERT INTO user_reading_item_tags(item_id, tag, created_at)
			VALUES (?, ?, ?)`, itemID, tag, timestamp); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) DeleteReadingItem(userID, itemID string) error {
	result, err := s.db.Exec(`DELETE FROM user_reading_items WHERE id = ? AND user_id = ?`, strings.TrimSpace(itemID), userID)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrReadingItemNotFound
	}
	return nil
}
