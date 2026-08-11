package server

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type libraryOperationError struct {
	Code    string
	Message string
	Status  int
}

func (e *libraryOperationError) Error() string { return e.Message }

func libraryError(err error) *libraryOperationError {
	var target *libraryOperationError
	if errors.As(err, &target) {
		return target
	}
	return &libraryOperationError{Code: "library_operation_failed", Message: "The library operation failed.", Status: 500}
}

func (s *Server) storeUploadedBook(originalName string, input io.Reader) (string, int64, error) {
	filename := safeUploadName(originalName)
	if filename == "" || !supportedBookExtension(filepath.Ext(filename)) {
		return "", 0, &libraryOperationError{Code: "unsupported_book", Message: "That file type is not supported.", Status: 400}
	}
	destination := filepath.Join(s.BookDir, filename)
	if _, err := os.Stat(destination); err == nil {
		return "", 0, &libraryOperationError{Code: "book_filename_exists", Message: "A book with that filename already exists.", Status: 409}
	} else if !os.IsNotExist(err) {
		return "", 0, &libraryOperationError{Code: "destination_check_failed", Message: "The destination could not be checked.", Status: 500}
	}
	tmp, err := os.CreateTemp(s.BookDir, ".book-upload-*")
	if err != nil {
		return "", 0, &libraryOperationError{Code: "upload_storage_failed", Message: "The upload could not be stored.", Status: 500}
	}
	tmpName := tmp.Name()
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = os.Remove(tmpName)
		}
	}()
	written, err := io.Copy(tmp, io.LimitReader(input, maxBookUploadBytes+1))
	if err != nil || written > maxBookUploadBytes {
		return "", 0, &libraryOperationError{Code: "book_too_large", Message: "The upload failed or exceeds 256 MiB.", Status: 413}
	}
	if err := tmp.Sync(); err != nil {
		return "", 0, &libraryOperationError{Code: "upload_save_failed", Message: "The upload could not be saved.", Status: 500}
	}
	if err := tmp.Close(); err != nil {
		return "", 0, &libraryOperationError{Code: "upload_save_failed", Message: "The upload could not be saved.", Status: 500}
	}
	if err := os.Chmod(tmpName, 0644); err != nil {
		return "", 0, &libraryOperationError{Code: "upload_permission_failed", Message: "The uploaded file permissions could not be set.", Status: 500}
	}
	if err := os.Rename(tmpName, destination); err != nil {
		return "", 0, &libraryOperationError{Code: "upload_install_failed", Message: "The uploaded book could not be installed.", Status: 500}
	}
	keep = true
	return filename, written, nil
}

func (s *Server) removeLibraryBook(id string) (string, error) {
	book := s.findBook(id)
	if book == nil {
		return "", &libraryOperationError{Code: "book_not_found", Message: "Book not found.", Status: 404}
	}
	relative, err := filepath.Rel(s.BookDir, book.FilePath)
	if err != nil || relative == ".." || filepath.IsAbs(relative) || len(relative) > 2 && relative[:3] == ".."+string(filepath.Separator) {
		return "", &libraryOperationError{Code: "book_path_outside_library", Message: "The book path is outside the managed library.", Status: 400}
	}
	trashDir := filepath.Join(s.BookDir, ".bookbrowser", "trash")
	if err := os.MkdirAll(trashDir, 0700); err != nil {
		return "", &libraryOperationError{Code: "trash_create_failed", Message: "The recoverable-delete folder could not be created.", Status: 500}
	}
	trashName := fmt.Sprintf("%s-%d-%s.deleted", book.ID(), time.Now().Unix(), safeUploadName(filepath.Base(book.FilePath)))
	destination := filepath.Join(trashDir, trashName)
	if err := os.Rename(book.FilePath, destination); err != nil {
		return "", &libraryOperationError{Code: "book_remove_failed", Message: "The book could not be moved to recoverable storage.", Status: 500}
	}
	return destination, nil
}
