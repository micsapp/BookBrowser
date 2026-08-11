package cli

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

type globalOptions struct {
	URL        string
	Token      string
	Profile    string
	EnvFile    string
	JSON       bool
	ShowToken  bool
	AllowHTTP  bool
	Timeout    time.Duration
	urlSet     bool
	tokenSet   bool
	profileSet bool
}

type remoteConfig struct {
	BaseURL     string
	Token       string
	Profile     string
	ProfileData *Profile
}

func parseEnvFile(path string) (map[string]string, error) {
	values := make(map[string]string)
	if strings.TrimSpace(path) == "" {
		return values, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open environment file: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '\'' && value[len(value)-1] == '\'' || value[0] == '"' && value[len(value)-1] == '"') {
			value = value[1 : len(value)-1]
		} else if index := strings.Index(value, " #"); index >= 0 {
			value = strings.TrimSpace(value[:index])
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func normalizeBaseURL(value string, allowHTTP bool) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid server URL %q", value)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" {
		return "", errorsNewURL("server URL must be an origin without credentials, query, fragment, or path")
	}
	parsed.Path, parsed.RawPath = "", ""
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", errorsNewURL("server URL scheme must be https or http")
	}
	if parsed.Scheme == "http" && !allowHTTP && !isLoopbackHost(parsed.Hostname()) {
		return "", errorsNewURL("plain HTTP is allowed only for loopback servers; use --allow-http to override")
	}
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func errorsNewURL(message string) error { return fmt.Errorf("invalid server URL: %s", message) }

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func resolveRemote(options globalOptions, profiles *ProfileStore, needToken bool) (*remoteConfig, error) {
	fileEnv, err := parseEnvFile(options.EnvFile)
	if err != nil {
		return nil, err
	}
	profileName := ""
	if options.profileSet {
		profileName = options.Profile
	} else if value := strings.TrimSpace(os.Getenv("BOOKBROWSER_PROFILE")); value != "" {
		profileName = value
	} else {
		profileName, err = profiles.Current()
		if err != nil {
			return nil, err
		}
	}
	var profile *Profile
	if profileName != "" {
		profile, err = profiles.Read(profileName)
		if err != nil {
			return nil, err
		}
		if profile == nil {
			return nil, fmt.Errorf("profile %q does not exist", profileName)
		}
	}

	baseURL := ""
	if options.urlSet {
		baseURL = options.URL
	} else if value := strings.TrimSpace(os.Getenv("BOOKBROWSER_URL")); value != "" {
		baseURL = value
	} else if value := strings.TrimSpace(fileEnv["BOOKBROWSER_URL"]); value != "" {
		baseURL = value
	} else if profile != nil {
		baseURL = profile.BaseURL
	}
	if baseURL == "" {
		return nil, fmt.Errorf("no server URL configured; run login or pass --url / BOOKBROWSER_URL")
	}
	baseURL, err = normalizeBaseURL(baseURL, options.AllowHTTP)
	if err != nil {
		return nil, err
	}

	token := ""
	if options.tokenSet {
		token = options.Token
	} else if value := strings.TrimSpace(os.Getenv("BOOKBROWSER_TOKEN")); value != "" {
		token = value
	} else if value := strings.TrimSpace(fileEnv["BOOKBROWSER_TOKEN"]); value != "" {
		token = value
	} else if profile != nil {
		token = profile.Token
	}
	if needToken && token == "" {
		return nil, fmt.Errorf("no API token configured; run login or pass --token / BOOKBROWSER_TOKEN")
	}
	return &remoteConfig{BaseURL: baseURL, Token: token, Profile: profileName, ProfileData: profile}, nil
}
