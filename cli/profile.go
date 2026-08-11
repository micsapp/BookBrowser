package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

type Profile struct {
	BaseURL   string    `json:"base_url"`
	Token     string    `json:"token"`
	Email     string    `json:"email"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	TokenName string    `json:"token_name"`
	SavedAt   time.Time `json:"saved_at"`
}

type ProfileStore struct {
	Root string
}

func NewProfileStore() (*ProfileStore, error) {
	if override := strings.TrimSpace(os.Getenv("BOOKBROWSER_CLI_HOME")); override != "" {
		return &ProfileStore{Root: override}, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("find user configuration directory: %w", err)
	}
	return &ProfileStore{Root: filepath.Join(dir, "bookbrowser", "cli")}, nil
}

func validateProfileName(name string) error {
	if !profileNamePattern.MatchString(name) {
		return fmt.Errorf("invalid profile name %q: use letters, digits, dot, underscore, or hyphen (maximum 64)", name)
	}
	return nil
}

func deriveProfileName(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err == nil {
		name := strings.Split(parsed.Hostname(), ".")[0]
		if profileNamePattern.MatchString(name) {
			return name
		}
	}
	return "default"
}

func (s *ProfileStore) profilesDir() string { return filepath.Join(s.Root, "profiles") }
func (s *ProfileStore) currentPath() string { return filepath.Join(s.Root, "current") }

func (s *ProfileStore) profilePath(name string) (string, error) {
	if err := validateProfileName(name); err != nil {
		return "", err
	}
	return filepath.Join(s.profilesDir(), name+".json"), nil
}

func (s *ProfileStore) EnsureWritable() error {
	if err := os.MkdirAll(s.profilesDir(), 0700); err != nil {
		return fmt.Errorf("create CLI profile directory: %w", err)
	}
	if err := os.Chmod(s.Root, 0700); err != nil && !errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("secure CLI directory: %w", err)
	}
	if err := os.Chmod(s.profilesDir(), 0700); err != nil && !errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("secure CLI profile directory: %w", err)
	}
	probe, err := os.CreateTemp(s.profilesDir(), ".write-test-*")
	if err != nil {
		return fmt.Errorf("CLI profile directory is not writable: %w", err)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := replaceFile(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func (s *ProfileStore) Write(name string, profile Profile, overwrite bool) error {
	path, err := s.profilePath(name)
	if err != nil {
		return err
	}
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("profile %q already exists; use --force to replace it", name)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := atomicWrite(path, data, 0600); err != nil {
		return fmt.Errorf("write profile %q: %w", name, err)
	}
	return nil
}

func (s *ProfileStore) Read(name string) (*Profile, error) {
	path, err := s.profilePath(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var profile Profile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("read profile %q: %w", name, err)
	}
	return &profile, nil
}

func (s *ProfileStore) List() ([]string, error) {
	entries, err := os.ReadDir(s.profilesDir())
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if profileNamePattern.MatchString(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func (s *ProfileStore) Current() (string, error) {
	data, err := os.ReadFile(s.currentPath())
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(string(data))
	if name == "" {
		return "", nil
	}
	if err := validateProfileName(name); err != nil {
		return "", err
	}
	return name, nil
}

func (s *ProfileStore) SetCurrent(name string) error {
	if err := validateProfileName(name); err != nil {
		return err
	}
	profile, err := s.Read(name)
	if err != nil {
		return err
	}
	if profile == nil {
		return fmt.Errorf("profile %q does not exist", name)
	}
	return atomicWrite(s.currentPath(), []byte(name+"\n"), 0600)
}

func (s *ProfileStore) clearCurrent() error {
	err := os.Remove(s.currentPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *ProfileStore) Remove(name string) error {
	path, err := s.profilePath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("profile %q does not exist", name)
		}
		return err
	}
	current, _ := s.Current()
	if current == name {
		names, err := s.List()
		if err != nil {
			return err
		}
		if len(names) == 0 {
			return s.clearCurrent()
		}
		return s.SetCurrent(names[0])
	}
	return nil
}

func (s *ProfileStore) Rename(oldName, newName string) error {
	if err := validateProfileName(oldName); err != nil {
		return err
	}
	if err := validateProfileName(newName); err != nil {
		return err
	}
	oldPath, _ := s.profilePath(oldName)
	newPath, _ := s.profilePath(newName)
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("profile %q already exists", newName)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("profile %q does not exist", oldName)
		}
		return err
	}
	current, _ := s.Current()
	if current == oldName {
		return atomicWrite(s.currentPath(), []byte(newName+"\n"), 0600)
	}
	return nil
}
