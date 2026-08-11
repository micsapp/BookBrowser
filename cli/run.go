package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/spf13/pflag"
	"golang.org/x/term"
)

const (
	ExitOK       = 0
	ExitFailure  = 1
	ExitUsage    = 2
	ExitAuth     = 3
	ExitNotFound = 4
)

type Runner struct {
	In          io.Reader
	Out         io.Writer
	Err         io.Writer
	Version     string
	Profiles    *ProfileStore
	OpenBrowser func(string) error
	Now         func() time.Time
	After       func(time.Duration) <-chan time.Time
	reader      *bufio.Reader
	jsonOutput  bool
}

type loginResponse struct {
	Status   string `json:"status,omitempty"`
	Token    string `json:"token"`
	APIToken struct {
		Name string `json:"name"`
	} `json:"api_token"`
	User cliUser `json:"user"`
}

type cliUser struct {
	ID         string `json:"id"`
	Email      string `json:"email"`
	Name       string `json:"name"`
	Role       string `json:"role"`
	Active     bool   `json:"active"`
	AllowShare bool   `json:"allow_share_links"`
}

type cliBook struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Author      string     `json:"author"`
	Description string     `json:"description"`
	Series      string     `json:"series"`
	SeriesIndex float64    `json:"series_index"`
	Publisher   string     `json:"publisher"`
	ISBN        string     `json:"isbn"`
	PublishDate *time.Time `json:"publish_date"`
	Format      string     `json:"format"`
	FileSize    int64      `json:"file_size"`
	ModifiedAt  time.Time  `json:"modified_at"`
	HasCover    bool       `json:"has_cover"`
}

type usageError struct{ message string }

func (e *usageError) Error() string { return e.message }

type reportedError struct{ err error }

func (e *reportedError) Error() string { return e.err.Error() }
func (e *reportedError) Unwrap() error { return e.err }

func NewRunner(in io.Reader, out, errOut io.Writer, version string) (*Runner, error) {
	profiles, err := NewProfileStore()
	if err != nil {
		return nil, err
	}
	if in == nil {
		in = strings.NewReader("")
	}
	if out == nil {
		out = io.Discard
	}
	if errOut == nil {
		errOut = io.Discard
	}
	return &Runner{
		In: in, Out: out, Err: errOut, Version: version, Profiles: profiles,
		OpenBrowser: openBrowser, Now: time.Now, After: time.After, reader: bufio.NewReader(in),
	}, nil
}

func Run(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer, version string) int {
	runner, err := NewRunner(in, out, errOut, version)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return ExitFailure
	}
	code, err := runner.execute(ctx, args)
	if err == nil {
		return code
	}
	if code == ExitOK {
		code = exitCode(err)
	}
	runner.printError(err)
	return code
}

func exitCode(err error) int {
	var usage *usageError
	if errors.As(err, &usage) {
		return ExitUsage
	}
	var api *APIError
	if errors.As(err, &api) {
		switch api.Status {
		case http.StatusUnauthorized, http.StatusForbidden:
			return ExitAuth
		case http.StatusNotFound, http.StatusConflict:
			return ExitNotFound
		}
	}
	return ExitFailure
}

func (r *Runner) printError(err error) {
	var reported *reportedError
	if errors.As(err, &reported) {
		return
	}
	if r.jsonOutput {
		body := map[string]interface{}{"error": map[string]interface{}{"code": "cli_error", "message": err.Error()}}
		var api *APIError
		if errors.As(err, &api) {
			body["error"] = map[string]interface{}{"code": api.Code, "message": api.Message, "status": api.Status}
		}
		_ = json.NewEncoder(r.Out).Encode(body)
		return
	}
	fmt.Fprintln(r.Err, "Error:", err)
}

func newFlagSet(name string) *pflag.FlagSet {
	flags := pflag.NewFlagSet(name, pflag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.SortFlags = false
	return flags
}

func bindGlobalFlags(flags *pflag.FlagSet, options *globalOptions) {
	flags.StringVar(&options.URL, "url", options.URL, "server base URL")
	flags.StringVar(&options.Token, "token", options.Token, "one-shot bearer token")
	flags.StringVar(&options.Profile, "profile", options.Profile, "named profile")
	flags.StringVar(&options.EnvFile, "env-file", options.EnvFile, "explicit environment file")
	flags.BoolVar(&options.JSON, "json", options.JSON, "print JSON")
	flags.BoolVar(&options.ShowToken, "show-token", options.ShowToken, "include tokens in explicit profile output")
	flags.BoolVar(&options.AllowHTTP, "allow-http", options.AllowHTTP, "allow plain HTTP to a non-loopback server")
	flags.DurationVar(&options.Timeout, "timeout", defaultDuration(options.Timeout, 30*time.Second), "HTTP timeout")
}

func defaultDuration(value, fallback time.Duration) time.Duration {
	if value == 0 {
		return fallback
	}
	return value
}

func captureGlobalChanges(flags *pflag.FlagSet, options *globalOptions) {
	options.urlSet = options.urlSet || flags.Changed("url")
	options.tokenSet = options.tokenSet || flags.Changed("token")
	options.profileSet = options.profileSet || flags.Changed("profile")
}

func (r *Runner) execute(ctx context.Context, args []string) (int, error) {
	options := globalOptions{}
	root := newFlagSet("bookbrowser-cli")
	bindGlobalFlags(root, &options)
	showVersion := root.BoolP("version", "v", false, "print version")
	showHelp := root.BoolP("help", "h", false, "print help")
	root.SetInterspersed(false)
	if err := root.Parse(args); err != nil {
		return ExitUsage, &usageError{err.Error()}
	}
	captureGlobalChanges(root, &options)
	remaining := root.Args()
	if *showVersion {
		fmt.Fprintln(r.Out, r.Version)
		return ExitOK, nil
	}
	if *showHelp {
		r.printUsage()
		return ExitOK, nil
	}
	if len(remaining) == 0 {
		r.printUsage()
		return ExitOK, nil
	}
	command, commandArgs := remaining[0], remaining[1:]
	r.jsonOutput = options.JSON
	switch command {
	case "help", "-h", "--help":
		r.printUsage()
		return ExitOK, nil
	case "version":
		fmt.Fprintln(r.Out, r.Version)
		return ExitOK, nil
	case "login":
		return ExitOK, r.cmdLogin(ctx, commandArgs, options)
	case "logout":
		return ExitOK, r.cmdLogout(ctx, commandArgs, options)
	case "whoami":
		return ExitOK, r.cmdWhoami(ctx, commandArgs, options)
	case "profiles", "profile":
		return ExitOK, r.cmdProfiles(commandArgs, options)
	case "use":
		return ExitOK, r.cmdUse(commandArgs, options)
	case "tokens", "token":
		return ExitOK, r.cmdTokens(ctx, commandArgs, options)
	case "books", "book":
		return ExitOK, r.cmdBooks(ctx, commandArgs, options)
	case "library":
		return ExitOK, r.cmdLibrary(ctx, commandArgs, options)
	case "users", "user":
		return ExitOK, r.cmdUsers(ctx, commandArgs, options)
	case "settings", "setting":
		return ExitOK, r.cmdSettings(ctx, commandArgs, options)
	default:
		return ExitUsage, &usageError{fmt.Sprintf("unknown command %q; run bookbrowser-cli help", command)}
	}
}

func (r *Runner) commandFlags(name string, args []string, options globalOptions, add func(*pflag.FlagSet)) (*pflag.FlagSet, globalOptions, error) {
	flags := newFlagSet(name)
	bindGlobalFlags(flags, &options)
	if add != nil {
		add(flags)
	}
	if err := flags.Parse(args); err != nil {
		return nil, options, &usageError{err.Error()}
	}
	captureGlobalChanges(flags, &options)
	r.jsonOutput = options.JSON
	return flags, options, nil
}

func (r *Runner) printUsage() {
	fmt.Fprintln(r.Out, `Usage: bookbrowser-cli [global options] command [args]

Authentication:
  login                         Sign in and save a profile
    --method password           Email/password login (default)
    --method google-link        Print a short-lived Google URL and wait
    --method google-browser     Print the URL, open a browser, and wait
  logout                        Revoke the profile token and remove the profile
  whoami                        Show current server and user identity
  profiles [list|remove|rename] Manage saved profiles
  use <profile>                 Select the active profile
  tokens [list|revoke]          Manage server API tokens

Catalog:
  books list                    List books
  books search [query]          Search books and metadata
  books show <id>               Show one book
  books download <id>           Download one book

Administration:
  library status|upload|rescan|remove
  users list|show|update|password
  settings show|set

Other:
  version                       Print CLI version
  help                          Show this help

Global options:
  --url URL                     Override BOOKBROWSER_URL
  --token TOKEN                 Override BOOKBROWSER_TOKEN
  --profile NAME                Use a named profile
  --env-file PATH               Load an explicit environment file
  --json                        Machine-readable output
  --timeout DURATION            HTTP timeout (default 30s)
  --allow-http                  Permit non-loopback plain HTTP`)
}

func (r *Runner) prompt(label string) (string, error) {
	fmt.Fprint(r.Err, label)
	value, err := r.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (r *Runner) promptPassword(label string) (string, error) {
	fmt.Fprint(r.Err, label)
	if file, ok := r.In.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		value, err := term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(r.Err)
		return string(value), err
	}
	return r.prompt("")
}

func defaultTokenName(now time.Time) string {
	host, _ := os.Hostname()
	host = sanitizeTokenPart(host)
	if host == "" {
		host = "client"
	}
	name := "bookbrowser-cli." + host + "." + now.UTC().Format("20060102-150405") + "." + strconv.Itoa(os.Getpid())
	if len(name) > 80 {
		name = name[:80]
	}
	return strings.TrimRight(name, ".-")
}

func sanitizeTokenPart(value string) string {
	var out strings.Builder
	for _, ch := range value {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) || strings.ContainsRune("._-", ch) {
			out.WriteRune(ch)
		} else {
			out.WriteByte('-')
		}
	}
	return strings.Trim(out.String(), "-.")
}

func (r *Runner) loginURL(options globalOptions) (string, error) {
	fileEnv, err := parseEnvFile(options.EnvFile)
	if err != nil {
		return "", err
	}
	value := ""
	if options.urlSet {
		value = options.URL
	} else if env := strings.TrimSpace(os.Getenv("BOOKBROWSER_URL")); env != "" {
		value = env
	} else if env := strings.TrimSpace(fileEnv["BOOKBROWSER_URL"]); env != "" {
		value = env
	} else {
		name := ""
		if options.profileSet {
			name = options.Profile
		} else {
			name, _ = r.Profiles.Current()
		}
		if name != "" {
			profile, err := r.Profiles.Read(name)
			if err != nil {
				return "", err
			}
			if profile != nil {
				value = profile.BaseURL
			}
		}
	}
	if value == "" {
		return "", fmt.Errorf("login requires --url or BOOKBROWSER_URL")
	}
	return normalizeBaseURL(value, options.AllowHTTP)
}

func (r *Runner) cmdLogin(ctx context.Context, args []string, options globalOptions) error {
	var method, email, password, tokenName string
	var noSwitch, force bool
	flags, options, err := r.commandFlags("login", args, options, func(flags *pflag.FlagSet) {
		flags.StringVar(&method, "method", "password", "password, google-link, or google-browser")
		flags.StringVar(&email, "email", "", "login email")
		flags.StringVar(&password, "password", "", "login password (visible in process arguments)")
		flags.StringVar(&tokenName, "token-name", "", "server-side token name")
		flags.BoolVar(&noSwitch, "no-switch", false, "do not make the profile active")
		flags.BoolVar(&force, "force", false, "replace an existing profile")
	})
	if err != nil {
		return err
	}
	if len(flags.Args()) != 0 {
		return &usageError{"login accepts no positional arguments"}
	}
	if method != "password" && method != "google-link" && method != "google-browser" {
		return &usageError{"--method must be password, google-link, or google-browser"}
	}
	baseURL, err := r.loginURL(options)
	if err != nil {
		return err
	}
	profileName := ""
	if options.profileSet {
		profileName = options.Profile
	} else {
		profileName = deriveProfileName(baseURL)
	}
	if err := validateProfileName(profileName); err != nil {
		return err
	}
	if err := r.Profiles.EnsureWritable(); err != nil {
		return err
	}
	existingProfile, err := r.Profiles.Read(profileName)
	if err != nil {
		return err
	} else if existingProfile != nil && !force {
		return fmt.Errorf("profile %q already exists; use --force to replace it", profileName)
	}
	if tokenName == "" {
		tokenName = defaultTokenName(r.Now())
	}
	client := NewClient(baseURL, "", r.Version, options.Timeout)
	var response loginResponse
	if method == "password" {
		if email == "" {
			email, err = r.prompt("Email: ")
			if err != nil {
				return err
			}
		}
		if password == "" {
			password, err = r.promptPassword("Password: ")
			if err != nil {
				return err
			}
		}
		_, _, err = client.JSON(ctx, http.MethodPost, "/api/v1/auth/login", map[string]string{
			"email": email, "password": password, "token_name": tokenName,
		}, &response)
		password = ""
		if err != nil {
			return err
		}
	} else {
		response, err = r.googleLogin(ctx, client, method, tokenName, profileName)
		if err != nil {
			return err
		}
	}
	profile := Profile{
		BaseURL: baseURL, Token: response.Token, Email: response.User.Email,
		UserID: response.User.ID, Role: response.User.Role, TokenName: response.APIToken.Name,
		SavedAt: r.Now().UTC(),
	}
	if err := r.Profiles.Write(profileName, profile, force); err != nil {
		client.Token = response.Token
		_, _, revokeErr := client.JSON(context.Background(), http.MethodDelete, "/api/v1/token", nil, nil)
		if revokeErr != nil {
			return fmt.Errorf("%w; additionally failed to revoke the new token: %v", err, revokeErr)
		}
		return err
	}
	if !noSwitch {
		if err := r.Profiles.SetCurrent(profileName); err != nil {
			return err
		}
	}
	if existingProfile != nil && existingProfile.Token != "" && existingProfile.Token != response.Token {
		oldBaseURL, normalizeErr := normalizeBaseURL(existingProfile.BaseURL, options.AllowHTTP)
		if normalizeErr == nil {
			revokeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			oldClient := NewClient(oldBaseURL, existingProfile.Token, r.Version, 5*time.Second)
			_, _, revokeErr := oldClient.JSON(revokeCtx, http.MethodDelete, "/api/v1/token", nil, nil)
			cancel()
			if revokeErr != nil {
				fmt.Fprintf(r.Err, "Warning: the replaced profile's previous token could not be revoked: %v\n", revokeErr)
			}
		} else {
			fmt.Fprintf(r.Err, "Warning: the replaced profile's previous token could not be revoked: %v\n", normalizeErr)
		}
	}
	result := map[string]interface{}{"profile": profileName, "base_url": baseURL, "user": response.User, "token_name": response.APIToken.Name}
	if options.ShowToken {
		result["token"] = response.Token
	}
	if options.JSON {
		return writeJSON(r.Out, result)
	}
	fmt.Fprintf(r.Out, "Logged in as %s (%s); saved profile %q.\n", response.User.Email, response.User.Role, profileName)
	return nil
}

func (r *Runner) googleLogin(ctx context.Context, client *Client, method, tokenName, profileName string) (loginResponse, error) {
	host, _ := os.Hostname()
	var started struct {
		ChallengeID     string    `json:"challenge_id"`
		PollSecret      string    `json:"poll_secret"`
		VerificationURL string    `json:"verification_url"`
		ExpiresAt       time.Time `json:"expires_at"`
		Interval        int       `json:"interval_seconds"`
	}
	_, _, err := client.JSON(ctx, http.MethodPost, "/api/v1/auth/google/start", map[string]string{
		"token_name": tokenName, "client_name": host,
	}, &started)
	if err != nil {
		return loginResponse{}, err
	}
	completed := false
	defer func() {
		if completed {
			return
		}
		cancelCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _, _ = client.JSON(cancelCtx, http.MethodPost, "/api/v1/auth/google/cancel", map[string]string{
			"challenge_id": started.ChallengeID, "poll_secret": started.PollSecret,
		}, nil)
	}()
	fmt.Fprintf(r.Err, "Open this short-lived URL to sign in:\n\n%s\n\nTarget profile: %s\nExpires: %s\nWaiting for approval; press Ctrl+C to cancel.\n",
		started.VerificationURL, profileName, started.ExpiresAt.Local().Format(time.RFC1123))
	if method == "google-browser" {
		if err := r.OpenBrowser(started.VerificationURL); err != nil {
			fmt.Fprintf(r.Err, "Warning: %v; open the URL manually.\n", err)
		}
	}
	interval := time.Duration(started.Interval) * time.Second
	if interval < time.Second {
		interval = 3 * time.Second
	}
	for {
		select {
		case <-ctx.Done():
			return loginResponse{}, ctx.Err()
		case <-r.After(interval):
		}
		var response loginResponse
		status, headers, err := client.JSON(ctx, http.MethodPost, "/api/v1/auth/google/poll", map[string]string{
			"challenge_id": started.ChallengeID, "poll_secret": started.PollSecret,
		}, &response)
		if err == nil && status == http.StatusOK {
			completed = true
			return response, nil
		}
		if err == nil && status == http.StatusAccepted {
			continue
		}
		var api *APIError
		if errors.As(err, &api) && api.Status == http.StatusTooManyRequests {
			if seconds, parseErr := strconv.Atoi(headers.Get("Retry-After")); parseErr == nil && seconds > 0 {
				interval = time.Duration(seconds) * time.Second
			}
			continue
		}
		return loginResponse{}, err
	}
}

func writeJSON(output io.Writer, value interface{}) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func (r *Runner) cmdLogout(ctx context.Context, args []string, options globalOptions) error {
	var localOnly bool
	flags, options, err := r.commandFlags("logout", args, options, func(flags *pflag.FlagSet) {
		flags.BoolVar(&localOnly, "local-only", false, "remove the local profile without server revocation")
	})
	if err != nil {
		return err
	}
	if len(flags.Args()) != 0 {
		return &usageError{"logout accepts no positional arguments"}
	}
	name := options.Profile
	if !options.profileSet {
		name, err = r.Profiles.Current()
		if err != nil {
			return err
		}
	}
	if name == "" {
		return fmt.Errorf("no active profile")
	}
	profile, err := r.Profiles.Read(name)
	if err != nil || profile == nil {
		if err == nil {
			err = fmt.Errorf("profile %q does not exist", name)
		}
		return err
	}
	if !localOnly {
		baseURL, err := normalizeBaseURL(profile.BaseURL, options.AllowHTTP)
		if err != nil {
			return err
		}
		client := NewClient(baseURL, profile.Token, r.Version, options.Timeout)
		if _, _, err := client.JSON(ctx, http.MethodDelete, "/api/v1/token", nil, nil); err != nil {
			return fmt.Errorf("server token was not revoked; profile kept (use --local-only to override): %w", err)
		}
	}
	if err := r.Profiles.Remove(name); err != nil {
		return err
	}
	if options.JSON {
		return writeJSON(r.Out, map[string]interface{}{"profile": name, "removed": true, "server_revoked": !localOnly})
	}
	fmt.Fprintf(r.Out, "Logged out profile %q.\n", name)
	return nil
}

func (r *Runner) authenticatedClient(options globalOptions) (*Client, *remoteConfig, error) {
	config, err := resolveRemote(options, r.Profiles, true)
	if err != nil {
		return nil, nil, err
	}
	return NewClient(config.BaseURL, config.Token, r.Version, options.Timeout), config, nil
}

func (r *Runner) cmdWhoami(ctx context.Context, args []string, options globalOptions) error {
	flags, options, err := r.commandFlags("whoami", args, options, nil)
	if err != nil {
		return err
	}
	if len(flags.Args()) != 0 {
		return &usageError{"whoami accepts no positional arguments"}
	}
	client, config, err := r.authenticatedClient(options)
	if err != nil {
		return err
	}
	var response struct {
		User   cliUser                `json:"user"`
		Token  map[string]interface{} `json:"token"`
		Server map[string]interface{} `json:"server"`
	}
	if _, _, err := client.JSON(ctx, http.MethodGet, "/api/v1/me", nil, &response); err != nil {
		return err
	}
	if options.JSON {
		return writeJSON(r.Out, map[string]interface{}{"profile": config.Profile, "base_url": config.BaseURL, "user": response.User, "token": response.Token, "server": response.Server})
	}
	fmt.Fprintf(r.Out, "Profile: %s\nServer:  %s\nUser:    %s <%s>\nRole:    %s\n", emptyDash(config.Profile), config.BaseURL, response.User.Name, response.User.Email, response.User.Role)
	return nil
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func (r *Runner) cmdProfiles(args []string, options globalOptions) error {
	flags, options, err := r.commandFlags("profiles", args, options, nil)
	if err != nil {
		return err
	}
	pos := flags.Args()
	sub := "list"
	if len(pos) > 0 {
		sub, pos = pos[0], pos[1:]
	}
	switch sub {
	case "list":
		if len(pos) != 0 {
			return &usageError{"profiles list accepts no arguments"}
		}
		names, err := r.Profiles.List()
		if err != nil {
			return err
		}
		current, _ := r.Profiles.Current()
		items := make([]map[string]interface{}, 0, len(names))
		for _, name := range names {
			profile, err := r.Profiles.Read(name)
			if err != nil {
				return err
			}
			item := map[string]interface{}{"name": name, "active": name == current, "base_url": profile.BaseURL, "email": profile.Email, "role": profile.Role, "token_name": profile.TokenName, "saved_at": profile.SavedAt}
			if options.ShowToken {
				item["token"] = profile.Token
			}
			items = append(items, item)
		}
		if options.JSON {
			return writeJSON(r.Out, map[string]interface{}{"profiles": items})
		}
		writer := tabwriter.NewWriter(r.Out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "ACTIVE\tNAME\tEMAIL\tROLE\tSERVER")
		for _, item := range items {
			active := ""
			if item["active"].(bool) {
				active = "*"
			}
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\n", active, item["name"], item["email"], item["role"], item["base_url"])
		}
		return writer.Flush()
	case "remove", "rm":
		if len(pos) != 1 {
			return &usageError{"profiles remove requires NAME"}
		}
		if err := r.Profiles.Remove(pos[0]); err != nil {
			return err
		}
	case "rename":
		if len(pos) != 2 {
			return &usageError{"profiles rename requires OLD NEW"}
		}
		if err := r.Profiles.Rename(pos[0], pos[1]); err != nil {
			return err
		}
	default:
		return &usageError{"profiles subcommand must be list, remove, or rename"}
	}
	if options.JSON {
		return writeJSON(r.Out, map[string]bool{"updated": true})
	}
	fmt.Fprintln(r.Out, "Profile updated.")
	return nil
}

func (r *Runner) cmdUse(args []string, options globalOptions) error {
	flags, options, err := r.commandFlags("use", args, options, nil)
	if err != nil {
		return err
	}
	if len(flags.Args()) != 1 {
		return &usageError{"use requires PROFILE"}
	}
	if err := r.Profiles.SetCurrent(flags.Args()[0]); err != nil {
		return err
	}
	if options.JSON {
		return writeJSON(r.Out, map[string]interface{}{"active_profile": flags.Args()[0]})
	}
	fmt.Fprintf(r.Out, "Active profile: %s\n", flags.Args()[0])
	return nil
}

func (r *Runner) cmdTokens(ctx context.Context, args []string, options globalOptions) error {
	flags, options, err := r.commandFlags("tokens", args, options, nil)
	if err != nil {
		return err
	}
	pos := flags.Args()
	sub := "list"
	if len(pos) > 0 {
		sub, pos = pos[0], pos[1:]
	}
	client, _, err := r.authenticatedClient(options)
	if err != nil {
		return err
	}
	if sub == "revoke" {
		if len(pos) != 1 {
			return &usageError{"tokens revoke requires NAME"}
		}
		var response map[string]interface{}
		if _, _, err := client.JSON(ctx, http.MethodDelete, "/api/v1/tokens/"+url.PathEscape(pos[0]), nil, &response); err != nil {
			return err
		}
		return r.outputResult(options, response, "Token revoked.")
	}
	if sub != "list" || len(pos) != 0 {
		return &usageError{"tokens subcommand must be list or revoke NAME"}
	}
	var response struct {
		Tokens []struct {
			Name       string     `json:"name"`
			CreatedAt  time.Time  `json:"created_at"`
			LastUsedAt *time.Time `json:"last_used_at"`
			ExpiresAt  *time.Time `json:"expires_at"`
		} `json:"tokens"`
	}
	if _, _, err := client.JSON(ctx, http.MethodGet, "/api/v1/tokens", nil, &response); err != nil {
		return err
	}
	if options.JSON {
		return writeJSON(r.Out, response)
	}
	writer := tabwriter.NewWriter(r.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "NAME\tCREATED\tLAST USED\tEXPIRES")
	for _, token := range response.Tokens {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", token.Name, formatTime(&token.CreatedAt), formatTime(token.LastUsedAt), formatTime(token.ExpiresAt))
	}
	return writer.Flush()
}

func formatTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "-"
	}
	return value.Local().Format("2006-01-02 15:04")
}

func (r *Runner) cmdBooks(ctx context.Context, args []string, options globalOptions) error {
	var sortName, author, series, format, output string
	var limit, offset int
	var force bool
	flags, options, err := r.commandFlags("books", args, options, func(flags *pflag.FlagSet) {
		flags.StringVar(&sortName, "sort", "modified-desc", "book sort")
		flags.StringVar(&author, "author", "", "author filter")
		flags.StringVar(&series, "series", "", "series filter")
		flags.StringVar(&format, "format", "", "format filter")
		flags.IntVar(&limit, "limit", 50, "maximum results")
		flags.IntVar(&offset, "offset", 0, "result offset")
		flags.StringVarP(&output, "output", "o", "", "download output path")
		flags.BoolVar(&force, "force", false, "overwrite download output")
	})
	if err != nil {
		return err
	}
	pos := flags.Args()
	if len(pos) == 0 {
		return &usageError{"books requires list, search, show, or download"}
	}
	client, _, err := r.authenticatedClient(options)
	if err != nil {
		return err
	}
	sub, pos := pos[0], pos[1:]
	switch sub {
	case "list", "search":
		query := ""
		if sub == "search" {
			query = strings.Join(pos, " ")
		} else if len(pos) != 0 {
			return &usageError{"books list accepts no positional arguments"}
		}
		values := url.Values{"sort": {sortName}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
		if query != "" {
			values.Set("q", query)
		}
		if author != "" {
			values.Set("author", author)
		}
		if series != "" {
			values.Set("series", series)
		}
		if format != "" {
			values.Set("format", format)
		}
		var response struct {
			Items  []cliBook `json:"items"`
			Limit  int       `json:"limit"`
			Offset int       `json:"offset"`
			Total  int       `json:"total"`
		}
		if _, _, err := client.JSON(ctx, http.MethodGet, "/api/v1/books?"+values.Encode(), nil, &response); err != nil {
			return err
		}
		return r.outputBooks(options, response.Items, response.Total, response.Limit, response.Offset)
	case "show":
		if len(pos) != 1 {
			return &usageError{"books show requires BOOK_ID"}
		}
		var response struct {
			Book cliBook `json:"book"`
		}
		if _, _, err := client.JSON(ctx, http.MethodGet, "/api/v1/books/"+url.PathEscape(pos[0]), nil, &response); err != nil {
			return err
		}
		if options.JSON {
			return writeJSON(r.Out, response)
		}
		book := response.Book
		fmt.Fprintf(r.Out, "ID: %s\nTitle: %s\nAuthor: %s\nSeries: %s\nFormat: %s\nSize: %d\nPublisher: %s\nISBN: %s\nModified: %s\n\n%s\n",
			book.ID, book.Title, book.Author, book.Series, book.Format, book.FileSize, book.Publisher, book.ISBN, book.ModifiedAt.Local().Format(time.RFC3339), book.Description)
		return nil
	case "download":
		if len(pos) != 1 {
			return &usageError{"books download requires BOOK_ID"}
		}
		return r.downloadBook(ctx, client, pos[0], output, force, options)
	default:
		return &usageError{"books subcommand must be list, search, show, or download"}
	}
}

func (r *Runner) outputBooks(options globalOptions, books []cliBook, total, limit, offset int) error {
	if options.JSON {
		return writeJSON(r.Out, map[string]interface{}{"items": books, "total": total, "limit": limit, "offset": offset})
	}
	writer := tabwriter.NewWriter(r.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "ID\tTITLE\tAUTHOR\tSERIES\tFORMAT\tSIZE\tMODIFIED")
	for _, book := range books {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n", book.ID, book.Title, book.Author, book.Series, book.Format, book.FileSize, book.ModifiedAt.Local().Format("2006-01-02"))
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(r.Out, "%d matching book(s).\n", total)
	return nil
}

func sanitizeDownloadName(value string) string {
	value = filepath.Base(strings.TrimSpace(value))
	value = strings.Map(func(ch rune) rune {
		if ch < 32 || strings.ContainsRune(`<>:"/\\|?*`, ch) {
			return '_'
		}
		return ch
	}, value)
	value = strings.Trim(value, ". ")
	if value == "" {
		return "book-download"
	}
	return value
}

func (r *Runner) downloadBook(ctx context.Context, client *Client, id, output string, force bool, options globalOptions) error {
	client.HTTP.Timeout = 10 * time.Minute
	body, suggested, err := client.Download(ctx, id)
	if err != nil {
		return err
	}
	defer body.Close()
	suggested = sanitizeDownloadName(suggested)
	target := output
	if target == "" {
		target = suggested
	} else if info, err := os.Stat(target); err == nil && info.IsDir() {
		target = filepath.Join(target, suggested)
	}
	target = filepath.Clean(target)
	if info, err := os.Stat(target); err == nil && !info.IsDir() && !force {
		return fmt.Errorf("output %s already exists; use --force to replace it", target)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".part-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := io.Copy(tmp, body); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := replaceFile(tmpName, target); err != nil {
		if !force {
			return err
		}
		return err
	}
	ok = true
	if options.JSON {
		return writeJSON(r.Out, map[string]interface{}{"downloaded": true, "book_id": id, "output": target})
	}
	fmt.Fprintf(r.Out, "Downloaded %s -> %s\n", id, target)
	return nil
}

func (r *Runner) cmdLibrary(ctx context.Context, args []string, options globalOptions) error {
	var yes bool
	flags, options, err := r.commandFlags("library", args, options, func(flags *pflag.FlagSet) {
		flags.BoolVar(&yes, "yes", false, "skip removal confirmation")
	})
	if err != nil {
		return err
	}
	pos := flags.Args()
	if len(pos) == 0 {
		return &usageError{"library requires status, upload, rescan, or remove"}
	}
	client, _, err := r.authenticatedClient(options)
	if err != nil {
		return err
	}
	sub, pos := pos[0], pos[1:]
	switch sub {
	case "status":
		if len(pos) != 0 {
			return &usageError{"library status accepts no arguments"}
		}
		var response map[string]interface{}
		if _, _, err := client.JSON(ctx, http.MethodGet, "/api/v1/library/status", nil, &response); err != nil {
			return err
		}
		if options.JSON {
			return writeJSON(r.Out, response)
		}
		fmt.Fprintf(r.Out, "Indexing: %v\nProgress: %v\nBooks: %v\nLast completed: %v\nLast error: %v\n", response["indexing"], response["progress"], response["book_count"], response["last_completed_at"], response["last_error"])
		return nil
	case "rescan":
		if len(pos) != 0 {
			return &usageError{"library rescan accepts no arguments"}
		}
		var response map[string]interface{}
		if _, _, err := client.JSON(ctx, http.MethodPost, "/api/v1/library/rescan", map[string]interface{}{}, &response); err != nil {
			return err
		}
		return r.outputResult(options, response, "Library rescan started.")
	case "remove":
		if len(pos) != 1 {
			return &usageError{"library remove requires BOOK_ID"}
		}
		if !yes {
			if file, ok := r.In.(*os.File); !ok || !term.IsTerminal(int(file.Fd())) {
				return &usageError{"library remove requires --yes when stdin is not a terminal"}
			}
			answer, err := r.prompt(fmt.Sprintf("Move book %s to recoverable trash? [y/N] ", pos[0]))
			if err != nil {
				return err
			}
			if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
				return fmt.Errorf("removal cancelled")
			}
		}
		var response map[string]interface{}
		if _, _, err := client.JSON(ctx, http.MethodDelete, "/api/v1/library/books/"+url.PathEscape(pos[0]), nil, &response); err != nil {
			return err
		}
		return r.outputResult(options, response, "Book moved to recoverable trash.")
	case "upload":
		if len(pos) != 1 {
			return &usageError{"library upload requires FILE_OR_DIRECTORY"}
		}
		return r.uploadBooks(ctx, client, pos[0], options)
	default:
		return &usageError{"library subcommand must be status, upload, rescan, or remove"}
	}
}

func supportedLocalBook(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".epub", ".pdf", ".mobi":
		return true
	default:
		return false
	}
}

func collectLocalBooks(target string) ([]string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if !supportedLocalBook(target) {
			return nil, fmt.Errorf("unsupported book file: %s", target)
		}
		return []string{target}, nil
	}
	files := make([]string, 0)
	err = filepath.Walk(target, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && supportedLocalBook(path) {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func (r *Runner) uploadBooks(ctx context.Context, client *Client, target string, options globalOptions) error {
	files, err := collectLocalBooks(target)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no supported ebook files found in %s", target)
	}
	client.HTTP.Timeout = 10 * time.Minute
	results := make([]map[string]interface{}, 0, len(files))
	failed := 0
	for _, file := range files {
		result, err := client.Upload(ctx, file)
		item := map[string]interface{}{"file": file}
		if err != nil {
			item["error"] = err.Error()
			failed++
			if !options.JSON {
				fmt.Fprintf(r.Err, "Failed %s: %v\n", file, err)
			}
		} else {
			item["result"] = result
			if !options.JSON {
				fmt.Fprintf(r.Out, "Uploaded %s\n", file)
			}
		}
		results = append(results, item)
	}
	if options.JSON {
		if err := writeJSON(r.Out, map[string]interface{}{"files": results, "uploaded": len(files) - failed, "failed": failed}); err != nil {
			return err
		}
	}
	if failed != 0 {
		err := fmt.Errorf("%d of %d uploads failed", failed, len(files))
		if options.JSON {
			return &reportedError{err: err}
		}
		return err
	}
	return nil
}

func (r *Runner) cmdUsers(ctx context.Context, args []string, options globalOptions) error {
	var role, password string
	var active, inactive bool
	flags, options, err := r.commandFlags("users", args, options, func(flags *pflag.FlagSet) {
		flags.StringVar(&role, "role", "", "reader, manager, or admin")
		flags.BoolVar(&active, "active", false, "enable user")
		flags.BoolVar(&inactive, "inactive", false, "disable user")
		flags.StringVar(&password, "password", "", "new password (visible in process arguments)")
	})
	if err != nil {
		return err
	}
	pos := flags.Args()
	if len(pos) == 0 {
		return &usageError{"users requires list, show, update, or password"}
	}
	client, _, err := r.authenticatedClient(options)
	if err != nil {
		return err
	}
	sub, pos := pos[0], pos[1:]
	switch sub {
	case "list":
		if len(pos) != 0 {
			return &usageError{"users list accepts no arguments"}
		}
		var response struct {
			Users []map[string]interface{} `json:"users"`
		}
		if _, _, err := client.JSON(ctx, http.MethodGet, "/api/v1/users", nil, &response); err != nil {
			return err
		}
		if options.JSON {
			return writeJSON(r.Out, response)
		}
		writer := tabwriter.NewWriter(r.Out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "ID\tEMAIL\tNAME\tROLE\tACTIVE")
		for _, user := range response.Users {
			fmt.Fprintf(writer, "%v\t%v\t%v\t%v\t%v\n", user["id"], user["email"], user["name"], user["role"], user["active"])
		}
		return writer.Flush()
	case "show":
		if len(pos) != 1 {
			return &usageError{"users show requires USER_ID"}
		}
		var response map[string]interface{}
		if _, _, err := client.JSON(ctx, http.MethodGet, "/api/v1/users/"+url.PathEscape(pos[0]), nil, &response); err != nil {
			return err
		}
		return r.outputResult(options, response, fmt.Sprintf("%v\n", response["user"]))
	case "update":
		if len(pos) != 1 {
			return &usageError{"users update requires USER_ID"}
		}
		if active && inactive {
			return &usageError{"--active and --inactive cannot be combined"}
		}
		input := map[string]interface{}{}
		if role != "" {
			input["role"] = role
		}
		if active {
			input["active"] = true
		}
		if inactive {
			input["active"] = false
		}
		if len(input) == 0 {
			return &usageError{"users update requires --role, --active, or --inactive"}
		}
		var response map[string]interface{}
		if _, _, err := client.JSON(ctx, http.MethodPatch, "/api/v1/users/"+url.PathEscape(pos[0]), input, &response); err != nil {
			return err
		}
		return r.outputResult(options, response, "User updated.")
	case "password":
		if len(pos) != 1 {
			return &usageError{"users password requires USER_ID"}
		}
		if password == "" {
			password, err = r.promptPassword("New password: ")
			if err != nil {
				return err
			}
		}
		var response map[string]interface{}
		_, _, err := client.JSON(ctx, http.MethodPost, "/api/v1/users/"+url.PathEscape(pos[0])+"/password", map[string]string{"password": password}, &response)
		password = ""
		if err != nil {
			return err
		}
		return r.outputResult(options, response, "Password updated; sessions and API tokens revoked.")
	default:
		return &usageError{"users subcommand must be list, show, update, or password"}
	}
}

func (r *Runner) cmdSettings(ctx context.Context, args []string, options globalOptions) error {
	var siteName, pwaName string
	var registration, anonymous bool
	flags, options, err := r.commandFlags("settings", args, options, func(flags *pflag.FlagSet) {
		flags.StringVar(&siteName, "site-name", "", "site name")
		flags.StringVar(&pwaName, "pwa-name", "", "PWA name")
		flags.BoolVar(&registration, "registration-open", false, "allow registration")
		flags.BoolVar(&anonymous, "anonymous-book-links", false, "allow anonymous book links")
	})
	if err != nil {
		return err
	}
	pos := flags.Args()
	if len(pos) == 0 {
		return &usageError{"settings requires show or set"}
	}
	client, _, err := r.authenticatedClient(options)
	if err != nil {
		return err
	}
	sub, pos := pos[0], pos[1:]
	if len(pos) != 0 {
		return &usageError{"settings command accepts no positional arguments"}
	}
	if sub == "show" {
		var response map[string]interface{}
		if _, _, err := client.JSON(ctx, http.MethodGet, "/api/v1/settings", nil, &response); err != nil {
			return err
		}
		if options.JSON {
			return writeJSON(r.Out, response)
		}
		settings, _ := response["settings"].(map[string]interface{})
		for _, key := range []string{"site_name", "pwa_name", "registration_open", "anonymous_book_links"} {
			fmt.Fprintf(r.Out, "%s: %v\n", key, settings[key])
		}
		return nil
	}
	if sub != "set" {
		return &usageError{"settings subcommand must be show or set"}
	}
	input := map[string]interface{}{}
	if flags.Changed("site-name") {
		input["site_name"] = siteName
	}
	if flags.Changed("pwa-name") {
		input["pwa_name"] = pwaName
	}
	if flags.Changed("registration-open") {
		input["registration_open"] = registration
	}
	if flags.Changed("anonymous-book-links") {
		input["anonymous_book_links"] = anonymous
	}
	if len(input) == 0 {
		return &usageError{"settings set requires at least one setting flag"}
	}
	var response map[string]interface{}
	if _, _, err := client.JSON(ctx, http.MethodPatch, "/api/v1/settings", input, &response); err != nil {
		return err
	}
	return r.outputResult(options, response, "Settings updated.")
}

func (r *Runner) outputResult(options globalOptions, result interface{}, human string) error {
	if options.JSON {
		return writeJSON(r.Out, result)
	}
	fmt.Fprintln(r.Out, strings.TrimSuffix(human, "\n"))
	return nil
}
