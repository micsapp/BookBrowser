package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

const bookConversionTimeout = 10 * time.Minute

// storeUploadedBook installs normal uploads unchanged. MOBI-family uploads are
// converted once to EPUB before installation, so all browser reads use the
// existing EPUB reader and the original file is not indexed.
func (s *Server) storeUploadedBook(originalName string, input io.Reader) (string, int64, error) {
	filename := safeUploadName(originalName)
	if filename == "" || !supportedBookExtension(filepath.Ext(filename)) {
		return "", 0, &libraryOperationError{Code: "unsupported_book", Message: "That file type is not supported.", Status: 400}
	}

	installedName := filename
	if isMobiFilename(filename) {
		installedName = strings.TrimSuffix(filename, filepath.Ext(filename)) + ".epub"
	}
	destination := filepath.Join(s.BookDir, installedName)
	if _, err := os.Stat(destination); err == nil {
		return "", 0, &libraryOperationError{Code: "book_filename_exists", Message: "A book with that filename already exists.", Status: 409}
	} else if !os.IsNotExist(err) {
		return "", 0, &libraryOperationError{Code: "destination_check_failed", Message: "The destination could not be checked.", Status: 500}
	}

	// Calibre determines the input format from the filename extension.
	tmp, err := os.CreateTemp(s.BookDir, ".book-upload-*"+filepath.Ext(filename))
	if err != nil {
		return "", 0, &libraryOperationError{Code: "upload_storage_failed", Message: "The upload could not be stored.", Status: 500}
	}
	tmpName := tmp.Name()
	keepInput := false
	defer func() {
		_ = tmp.Close()
		if !keepInput {
			_ = os.Remove(tmpName)
		}
	}()

	written, err := io.Copy(tmp, io.LimitReader(input, maxBookUploadBytes+1))
	if err != nil || written > maxBookUploadBytes {
		return "", 0, &libraryOperationError{Code: "book_too_large", Message: "The upload failed or exceeds 256 MiB.", Status: 413}
	}
	if err := tmp.Sync(); err != nil {
		return "", 0, &libraryOperationError{Code: "upload_save_failed", Message: "The upload could not be stored.", Status: 500}
	}
	if err := tmp.Close(); err != nil {
		return "", 0, &libraryOperationError{Code: "upload_save_failed", Message: "The upload could not be stored.", Status: 500}
	}
	if err := os.Chmod(tmpName, 0644); err != nil {
		return "", 0, &libraryOperationError{Code: "upload_permission_failed", Message: "The uploaded file permissions could not be set.", Status: 500}
	}

	finalName := tmpName
	finalSize := written
	if isMobiFilename(filename) {
		finalName, finalSize, err = convertUploadedBook(tmpName, s.BookDir)
		if err != nil {
			return "", 0, err
		}
	}
	if err := os.Rename(finalName, destination); err != nil {
		if finalName != tmpName {
			_ = os.Remove(finalName)
		}
		return "", 0, &libraryOperationError{Code: "upload_install_failed", Message: "The uploaded book could not be installed.", Status: 500}
	}
	if finalName != tmpName {
		// The MOBI is deliberately not retained in the library (Option A).
		_ = os.Remove(tmpName)
	}
	keepInput = true
	return installedName, finalSize, nil
}

func isMobiFilename(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".mobi", ".azw", ".azw3":
		return true
	default:
		return false
	}
}

func convertUploadedBook(inputPath, bookDir string) (string, int64, error) {
	executable, err := exec.LookPath("ebook-convert")
	if err != nil {
		return "", 0, &libraryOperationError{Code: "converter_unavailable", Message: "MOBI books require Calibre (ebook-convert) to be installed on the server.", Status: 503}
	}
	output, err := os.CreateTemp(bookDir, ".book-convert-*.epub")
	if err != nil {
		return "", 0, &libraryOperationError{Code: "conversion_storage_failed", Message: "The converted book could not be stored.", Status: 500}
	}
	outputPath := output.Name()
	if err := output.Close(); err != nil {
		_ = os.Remove(outputPath)
		return "", 0, &libraryOperationError{Code: "conversion_storage_failed", Message: "The converted book could not be stored.", Status: 500}
	}
	// ebook-convert expects to create its output file itself.
	if err := os.Remove(outputPath); err != nil {
		return "", 0, &libraryOperationError{Code: "conversion_storage_failed", Message: "The converted book could not be stored.", Status: 500}
	}
	keepOutput := false
	defer func() {
		if !keepOutput {
			_ = os.Remove(outputPath)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), bookConversionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, inputPath, outputPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return "", 0, &libraryOperationError{Code: "conversion_timeout", Message: "Converting the MOBI book took too long.", Status: 504}
		}
		if details := strings.TrimSpace(string(output)); details != "" {
			if len(details) > 1000 {
				details = details[len(details)-1000:]
			}
			log.Printf("MOBI conversion failed for %s: %s", inputPath, details)
			return "", 0, &libraryOperationError{Code: "conversion_failed", Message: "The MOBI book could not be converted: " + details, Status: 422}
		}
		log.Printf("MOBI conversion failed for %s: %v", inputPath, err)
		return "", 0, &libraryOperationError{Code: "conversion_failed", Message: "The MOBI book could not be converted.", Status: 422}
	}

	info, err := os.Stat(outputPath)
	if err != nil || info.Size() == 0 {
		return "", 0, &libraryOperationError{Code: "conversion_failed", Message: "The converter did not produce a valid EPUB.", Status: 422}
	}
	if info.Size() > maxBookUploadBytes {
		return "", 0, &libraryOperationError{Code: "book_too_large", Message: "The converted EPUB exceeds 256 MiB.", Status: 413}
	}
	if err := os.Chmod(outputPath, 0644); err != nil {
		return "", 0, &libraryOperationError{Code: "conversion_permission_failed", Message: "The converted book permissions could not be set.", Status: 500}
	}
	// Ownership of the converted file passes to the caller, which installs it or
	// removes it if installation fails.
	keepOutput = true
	return outputPath, info.Size(), nil
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
