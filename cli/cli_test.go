package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func immediateAfter(time.Duration) <-chan time.Time {
	ready := make(chan time.Time, 1)
	ready <- time.Now()
	return ready
}

func newTestRunner(t *testing.T, input string) (*Runner, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	t.Setenv("BOOKBROWSER_CLI_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	runner, err := NewRunner(strings.NewReader(input), &stdout, &stderr, "test-version")
	if err != nil {
		t.Fatal(err)
	}
	runner.Now = func() time.Time { return time.Date(2026, time.August, 11, 15, 0, 0, 0, time.UTC) }
	runner.After = immediateAfter
	return runner, &stdout, &stderr
}

func TestPasswordLoginSavesSecureProfileWithoutPrintingToken(t *testing.T) {
	var received map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/login" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"bbk_super_secret","api_token":{"name":"laptop"},"user":{"id":"u1","email":"reader@example.com","name":"Reader","role":"reader","active":true}}`))
	}))
	defer server.Close()
	runner, stdout, _ := newTestRunner(t, "")
	code, err := runner.execute(context.Background(), []string{
		"login", "--url", server.URL, "--email", "reader@example.com",
		"--password", "correct horse battery staple", "--token-name", "laptop",
	})
	if err != nil || code != ExitOK {
		t.Fatalf("login code=%d err=%v", code, err)
	}
	if received["password"] != "correct horse battery staple" || received["token_name"] != "laptop" {
		t.Fatalf("login request=%#v", received)
	}
	if strings.Contains(stdout.String(), "bbk_super_secret") {
		t.Fatalf("stdout leaked token: %s", stdout.String())
	}
	profile, err := runner.Profiles.Read("127")
	if err != nil || profile == nil || profile.Token != "bbk_super_secret" {
		t.Fatalf("saved profile=%#v err=%v", profile, err)
	}
	current, err := runner.Profiles.Current()
	if err != nil || current != "127" {
		t.Fatalf("current=%q err=%v", current, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(runner.Profiles.profilesDir(), "127.json"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("profile mode=%v", info.Mode().Perm())
		}
	}
}

func TestGoogleLoginLinkAndBrowserModesPollAndSave(t *testing.T) {
	for _, test := range []struct {
		method       string
		browserCalls int
	}{
		{method: "google-link", browserCalls: 0},
		{method: "google-browser", browserCalls: 1},
	} {
		t.Run(test.method, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/auth/google/start":
					_, _ = w.Write([]byte(`{"challenge_id":"challenge","poll_secret":"private-poll-secret","verification_url":"https://books.example/cli/google/public","expires_at":"2026-08-11T15:05:00Z","interval_seconds":3}`))
				case "/api/v1/auth/google/poll":
					_, _ = w.Write([]byte(`{"status":"approved","token":"bbk_google_secret","api_token":{"name":"google-cli"},"user":{"id":"g1","email":"google@example.com","name":"Google User","role":"reader","active":true}}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			runner, stdout, stderr := newTestRunner(t, "")
			calls := 0
			runner.OpenBrowser = func(target string) error {
				calls++
				if target != "https://books.example/cli/google/public" {
					t.Fatalf("opened URL=%q", target)
				}
				return nil
			}
			code, err := runner.execute(context.Background(), []string{
				"login", "--method", test.method, "--url", server.URL, "--token-name", "google-cli",
			})
			if err != nil || code != ExitOK {
				t.Fatalf("login code=%d err=%v stderr=%s", code, err, stderr.String())
			}
			if calls != test.browserCalls {
				t.Fatalf("browser calls=%d want=%d", calls, test.browserCalls)
			}
			if !strings.Contains(stderr.String(), "https://books.example/cli/google/public") || strings.Contains(stdout.String(), "bbk_google_secret") {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			profile, err := runner.Profiles.Read("127")
			if err != nil || profile == nil || profile.Token != "bbk_google_secret" {
				t.Fatalf("profile=%#v err=%v", profile, err)
			}
		})
	}
}

func TestConfigPriorityFlagsEnvironmentFileAndProfile(t *testing.T) {
	t.Setenv("BOOKBROWSER_CLI_HOME", t.TempDir())
	profiles, err := NewProfileStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := profiles.EnsureWritable(); err != nil {
		t.Fatal(err)
	}
	if err := profiles.Write("saved", Profile{BaseURL: "https://profile.example", Token: "profile-token"}, false); err != nil {
		t.Fatal(err)
	}
	if err := profiles.SetCurrent("saved"); err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(t.TempDir(), "cli.env")
	if err := os.WriteFile(envFile, []byte("BOOKBROWSER_URL=https://file.example\nBOOKBROWSER_TOKEN=file-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOOKBROWSER_URL", "https://env.example")
	t.Setenv("BOOKBROWSER_TOKEN", "env-token")
	config, err := resolveRemote(globalOptions{EnvFile: envFile}, profiles, true)
	if err != nil || config.BaseURL != "https://env.example" || config.Token != "env-token" {
		t.Fatalf("environment config=%#v err=%v", config, err)
	}
	config, err = resolveRemote(globalOptions{
		URL: "https://flag.example", Token: "flag-token", EnvFile: envFile,
		urlSet: true, tokenSet: true,
	}, profiles, true)
	if err != nil || config.BaseURL != "https://flag.example" || config.Token != "flag-token" {
		t.Fatalf("flag config=%#v err=%v", config, err)
	}
	if _, err := normalizeBaseURL("http://public.example", false); err == nil {
		t.Fatal("non-loopback plain HTTP was accepted")
	}
	if _, err := normalizeBaseURL("http://127.0.0.1:8090", false); err != nil {
		t.Fatalf("loopback HTTP rejected: %v", err)
	}
}
