package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

func normalizeBookID(bookID string) (string, error) {
	bookID = strings.TrimSpace(bookID)
	if bookID == "" || len(bookID) > 200 {
		return "", errors.New("invalid book ID")
	}
	return bookID, nil
}

func normalizeListName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", errors.New("enter a list name")
	}
	if utf8.RuneCountInString(name) > 80 {
		return "", errors.New("list name must not exceed 80 characters")
	}
	return name, nil
}

func normalizeTag(tag string) (string, error) {
	tag = strings.Join(strings.Fields(tag), " ")
	if tag == "" {
		return "", errors.New("enter a tag")
	}
	if utf8.RuneCountInString(tag) > 40 {
		return "", errors.New("tag must not exceed 40 characters")
	}
	return tag, nil
}

func (s *SQLiteStore) RecordBookRead(userID, bookID string) error {
	bookID, err := normalizeBookID(bookID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO user_book_activity(user_id, book_id, last_read_at)
		VALUES (?, ?, ?)
		ON CONFLICT(user_id, book_id) DO UPDATE SET last_read_at = excluded.last_read_at`,
		userID, bookID, s.now().UTC().Unix())
	return err
}

func (s *SQLiteStore) RecentBookIDs(userID string, limit int) ([]string, error) {
	if limit < 1 {
		limit = 12
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT book_id FROM user_book_activity
		WHERE user_id = ? ORDER BY last_read_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBookIDs(rows)
}

func (s *SQLiteStore) BookListsForUser(userID string) ([]BookList, error) {
	rows, err := s.db.Query(`SELECT l.id, l.user_id, l.name, COUNT(i.book_id), l.created_at, l.updated_at
		FROM user_book_lists l
		LEFT JOIN user_book_list_items i ON i.list_id = l.id
		WHERE l.user_id = ?
		GROUP BY l.id, l.user_id, l.name, l.created_at, l.updated_at
		ORDER BY l.updated_at DESC, l.name COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lists := make([]BookList, 0)
	for rows.Next() {
		var list BookList
		var createdAt, updatedAt int64
		if err := rows.Scan(&list.ID, &list.UserID, &list.Name, &list.BookCount, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		list.CreatedAt = time.Unix(createdAt, 0).UTC()
		list.UpdatedAt = time.Unix(updatedAt, 0).UTC()
		lists = append(lists, list)
	}
	return lists, rows.Err()
}

func (s *SQLiteStore) BookListsForBook(userID, bookID string) ([]BookList, error) {
	bookID, err := normalizeBookID(bookID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT l.id, l.user_id, l.name, COUNT(i.book_id),
		EXISTS(SELECT 1 FROM user_book_list_items selected WHERE selected.list_id = l.id AND selected.book_id = ?),
		l.created_at, l.updated_at
		FROM user_book_lists l
		LEFT JOIN user_book_list_items i ON i.list_id = l.id
		WHERE l.user_id = ?
		GROUP BY l.id, l.user_id, l.name, l.created_at, l.updated_at
		ORDER BY l.name COLLATE NOCASE`, bookID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lists := make([]BookList, 0)
	for rows.Next() {
		var list BookList
		var contains int
		var createdAt, updatedAt int64
		if err := rows.Scan(&list.ID, &list.UserID, &list.Name, &list.BookCount, &contains, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		list.ContainsBook = contains == 1
		list.CreatedAt = time.Unix(createdAt, 0).UTC()
		list.UpdatedAt = time.Unix(updatedAt, 0).UTC()
		lists = append(lists, list)
	}
	return lists, rows.Err()
}

func (s *SQLiteStore) BookListForUser(userID, listID string) (*BookList, error) {
	var list BookList
	var createdAt, updatedAt int64
	err := s.db.QueryRow(`SELECT l.id, l.user_id, l.name, COUNT(i.book_id), l.created_at, l.updated_at
		FROM user_book_lists l
		LEFT JOIN user_book_list_items i ON i.list_id = l.id
		WHERE l.user_id = ? AND l.id = ?
		GROUP BY l.id, l.user_id, l.name, l.created_at, l.updated_at`, userID, strings.TrimSpace(listID)).
		Scan(&list.ID, &list.UserID, &list.Name, &list.BookCount, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBookListNotFound
	}
	if err != nil {
		return nil, err
	}
	list.CreatedAt = time.Unix(createdAt, 0).UTC()
	list.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return &list, nil
}

func (s *SQLiteStore) BookIDsForList(userID, listID string) ([]string, error) {
	if _, err := s.BookListForUser(userID, listID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT i.book_id FROM user_book_list_items i
		JOIN user_book_lists l ON l.id = i.list_id
		WHERE l.user_id = ? AND l.id = ? ORDER BY i.added_at DESC`, userID, strings.TrimSpace(listID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBookIDs(rows)
}

func (s *SQLiteStore) CreateBookList(userID, name string) (*BookList, error) {
	name, err := normalizeListName(name)
	if err != nil {
		return nil, err
	}
	id, err := randomToken(s.rand, 16)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	var duplicate int
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM user_book_lists
		WHERE user_id = ? AND name = ? COLLATE NOCASE)`, userID, name).Scan(&duplicate); err != nil {
		return nil, err
	}
	if duplicate == 1 {
		return nil, ErrBookListNameExists
	}
	if _, err := s.db.Exec(`INSERT INTO user_book_lists(id, user_id, name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`, id, userID, name, now.Unix(), now.Unix()); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			return nil, ErrBookListNameExists
		}
		return nil, err
	}
	return &BookList{ID: id, UserID: userID, Name: name, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *SQLiteStore) DeleteBookList(userID, listID string) error {
	result, err := s.db.Exec(`DELETE FROM user_book_lists WHERE id = ? AND user_id = ?`, strings.TrimSpace(listID), userID)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected == 0 {
		return ErrBookListNotFound
	}
	return nil
}

func (s *SQLiteStore) AddBookToList(userID, listID, bookID string) error {
	bookID, err := normalizeBookID(bookID)
	if err != nil {
		return err
	}
	if _, err := s.BookListForUser(userID, listID); err != nil {
		return err
	}
	now := s.now().UTC().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT OR IGNORE INTO user_book_list_items(list_id, book_id, added_at)
		VALUES (?, ?, ?)`, strings.TrimSpace(listID), bookID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE user_book_lists SET updated_at = ? WHERE id = ? AND user_id = ?`, now, strings.TrimSpace(listID), userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) RemoveBookFromList(userID, listID, bookID string) error {
	bookID, err := normalizeBookID(bookID)
	if err != nil {
		return err
	}
	if _, err := s.BookListForUser(userID, listID); err != nil {
		return err
	}
	now := s.now().UTC().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM user_book_list_items WHERE list_id = ? AND book_id = ?`, strings.TrimSpace(listID), bookID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE user_book_lists SET updated_at = ? WHERE id = ? AND user_id = ?`, now, strings.TrimSpace(listID), userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) TagsForUser(userID string) ([]BookTag, error) {
	rows, err := s.db.Query(`SELECT MIN(tag), COUNT(DISTINCT book_id) FROM user_book_tags
		WHERE user_id = ? GROUP BY tag COLLATE NOCASE ORDER BY tag COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := make([]BookTag, 0)
	for rows.Next() {
		var tag BookTag
		if err := rows.Scan(&tag.Name, &tag.BookCount); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (s *SQLiteStore) TagsForBook(userID, bookID string) ([]string, error) {
	bookID, err := normalizeBookID(bookID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT tag FROM user_book_tags
		WHERE user_id = ? AND book_id = ? ORDER BY tag COLLATE NOCASE`, userID, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := make([]string, 0)
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (s *SQLiteStore) BookIDsForTag(userID, tag string) ([]string, error) {
	tag, err := normalizeTag(tag)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT book_id FROM user_book_tags
		WHERE user_id = ? AND tag = ? COLLATE NOCASE ORDER BY created_at DESC`, userID, tag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBookIDs(rows)
}

func (s *SQLiteStore) AddBookTag(userID, bookID, tag string) error {
	bookID, err := normalizeBookID(bookID)
	if err != nil {
		return err
	}
	tag, err = normalizeTag(tag)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR IGNORE INTO user_book_tags(user_id, book_id, tag, created_at)
		VALUES (?, ?, ?, ?)`, userID, bookID, tag, s.now().UTC().Unix())
	return err
}

func (s *SQLiteStore) RemoveBookTag(userID, bookID, tag string) error {
	bookID, err := normalizeBookID(bookID)
	if err != nil {
		return err
	}
	tag, err = normalizeTag(tag)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM user_book_tags
		WHERE user_id = ? AND book_id = ? AND tag = ? COLLATE NOCASE`, userID, bookID, tag)
	return err
}

func scanBookIDs(rows *sql.Rows) ([]string, error) {
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read book IDs: %w", err)
	}
	return ids, nil
}
