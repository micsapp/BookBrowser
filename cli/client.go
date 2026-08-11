package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxClientResponseBytes int64 = 2 << 20

type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("server returned HTTP %d", e.Status)
}

type Client struct {
	BaseURL   string
	Token     string
	UserAgent string
	HTTP      *http.Client
}

func NewClient(baseURL, token, version string, timeout time.Duration) *Client {
	origin, _ := url.Parse(baseURL)
	client := &http.Client{Timeout: timeout}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		if !strings.EqualFold(req.URL.Scheme, origin.Scheme) || !strings.EqualFold(req.URL.Host, origin.Host) {
			return fmt.Errorf("refusing cross-origin redirect to %s", req.URL.Redacted())
		}
		return nil
	}
	return &Client{BaseURL: strings.TrimSuffix(baseURL, "/"), Token: token, UserAgent: "bookbrowser-cli/" + version, HTTP: client}
}

func (c *Client) newRequest(ctx context.Context, method, requestPath string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+requestPath, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return req, nil
}

func parseAPIError(response *http.Response, body []byte) error {
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &envelope)
	message := strings.TrimSpace(envelope.Error.Message)
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	return &APIError{Status: response.StatusCode, Code: envelope.Error.Code, Message: message}
}

func (c *Client) JSON(ctx context.Context, method, requestPath string, input, output interface{}) (int, http.Header, error) {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := c.newRequest(ctx, method, requestPath, body)
	if err != nil {
		return 0, nil, err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxClientResponseBytes+1))
	if err != nil {
		return response.StatusCode, response.Header, err
	}
	if int64(len(data)) > maxClientResponseBytes {
		return response.StatusCode, response.Header, fmt.Errorf("server response exceeds %d bytes", maxClientResponseBytes)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, response.Header, parseAPIError(response, data)
	}
	if output != nil && len(data) != 0 {
		if err := json.Unmarshal(data, output); err != nil {
			return response.StatusCode, response.Header, fmt.Errorf("decode server response: %w", err)
		}
	}
	return response.StatusCode, response.Header, nil
}

func (c *Client) Upload(ctx context.Context, localPath string) (map[string]interface{}, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	writeDone := make(chan error, 1)
	go func() {
		part, err := multipartWriter.CreateFormFile("book", filepath.Base(localPath))
		if err == nil {
			_, err = io.Copy(part, file)
		}
		if closeErr := multipartWriter.Close(); err == nil {
			err = closeErr
		}
		_ = writer.CloseWithError(err)
		writeDone <- err
	}()
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v1/library/books", reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	response, err := c.HTTP.Do(req)
	if err != nil {
		_ = reader.CloseWithError(err)
		return nil, err
	}
	defer response.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, maxClientResponseBytes+1))
	writeErr := <-writeDone
	if writeErr != nil {
		return nil, writeErr
	}
	if readErr != nil {
		return nil, readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, parseAPIError(response, data)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Download(ctx context.Context, bookID string) (io.ReadCloser, string, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/api/v1/books/"+url.PathEscape(bookID)+"/download", nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/octet-stream")
	response, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(response.Body, maxClientResponseBytes))
		return nil, "", parseAPIError(response, data)
	}
	filename := ""
	if _, params, err := mime.ParseMediaType(response.Header.Get("Content-Disposition")); err == nil {
		filename = filepath.Base(params["filename"])
	}
	return response.Body, filename, nil
}
