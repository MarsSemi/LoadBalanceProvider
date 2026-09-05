package codexauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// -------------------------------------------------------------------------------------
const (
	defaultTokenPath     = "data/provider/oauth_tokens.json"
	openAIClientID       = "app_EMoamEEZ73f0CkXaXp7hrann"
	openAIAuthURL        = "https://auth.openai.com/oauth/authorize"
	openAITokenURL       = "https://auth.openai.com/oauth/token"
	openAIDeviceCodeURL  = "https://auth.openai.com/oauth/device/code"
	openAIUserInfoURL    = "https://auth.openai.com/userinfo"
	defaultCodexProvider = "openai-codex"
	oauthRedirectURI     = "http://localhost:1455/auth/callback"
	defaultOAuthScope    = "openid profile email offline_access"
)

// -------------------------------------------------------------------------------------
type Auth struct {
	AccessToken string
	AccountID   string
}

// -------------------------------------------------------------------------------------
type OAuthTokenRecord struct {
	ProviderID    string `json:"provider_id"`
	Provider      string `json:"provider"`
	TokenType     string `json:"token_type,omitempty"`
	AccessToken   string `json:"access_token,omitempty"`
	RefreshToken  string `json:"refresh_token,omitempty"`
	IDToken       string `json:"id_token,omitempty"`
	Scope         string `json:"scope,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
	AccountEmail  string `json:"account_email,omitempty"`
	AccountName   string `json:"account_name,omitempty"`
	AccountSub    string `json:"account_sub,omitempty"`
	RefreshFailed string `json:"refresh_failed,omitempty"`
}

// -------------------------------------------------------------------------------------
type Store struct {
	Path string
	mu   sync.Mutex
}

// -------------------------------------------------------------------------------------
type StartOptions struct {
	ProviderID     string
	FlowPreference string
	LaunchBrowser  bool
}

// -------------------------------------------------------------------------------------
type StartResult struct {
	ProviderID               string `json:"provider_id"`
	Flow                     string `json:"flow"`
	Status                   string `json:"status"`
	AuthorizationURL         string `json:"authorization_url,omitempty"`
	CallbackListening        bool   `json:"callback_listening,omitempty"`
	VerificationURI          string `json:"verification_uri,omitempty"`
	VerificationURIComplete  string `json:"verification_uri_complete,omitempty"`
	UserCode                 string `json:"user_code,omitempty"`
	ExpiresIn                int    `json:"expires_in,omitempty"`
	Interval                 int    `json:"interval,omitempty"`
	BrowserOpened            bool   `json:"browser_opened,omitempty"`
	Message                  string `json:"message,omitempty"`
	AccountEmail             string `json:"account_email,omitempty"`
	AccountName              string `json:"account_name,omitempty"`
	AccessTokenExpiresAt     string `json:"access_token_expires_at,omitempty"`
	ManualCompletionRequired bool   `json:"manual_completion_required,omitempty"`
}

// -------------------------------------------------------------------------------------
type Status struct {
	ProviderID              string `json:"provider_id"`
	Flow                    string `json:"flow,omitempty"`
	Status                  string `json:"status"`
	Message                 string `json:"message,omitempty"`
	LastError               string `json:"last_error,omitempty"`
	VerificationURI         string `json:"verification_uri,omitempty"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	UserCode                string `json:"user_code,omitempty"`
	ExpiresAt               string `json:"expires_at,omitempty"`
	AccountEmail            string `json:"account_email,omitempty"`
	AccountName             string `json:"account_name,omitempty"`
}

// -------------------------------------------------------------------------------------
type pendingSession struct {
	ProviderID              string
	Flow                    string
	State                   string
	Status                  string
	CodeVerifier            string
	RedirectURI             string
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	ExpiresAt               time.Time
	Interval                time.Duration
	LastError               string
	CreatedAt               time.Time
}

// -------------------------------------------------------------------------------------
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

// -------------------------------------------------------------------------------------
type openAIUserInfo struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// -------------------------------------------------------------------------------------
type cliAuthFile struct {
	AuthMode    string        `json:"auth_mode"`
	LastRefresh string        `json:"last_refresh"`
	Tokens      cliAuthTokens `json:"tokens"`
}

// -------------------------------------------------------------------------------------
type cliAuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	AccountID    string `json:"account_id"`
}

// -------------------------------------------------------------------------------------
var pendingSessions = struct {
	mu         sync.Mutex
	byState    map[string]pendingSession
	byProvider map[string]pendingSession
}{
	byState:    map[string]pendingSession{},
	byProvider: map[string]pendingSession{},
}

// -------------------------------------------------------------------------------------
func Ensure(providerID string) (Auth, error) {
	return NewStore(defaultTokenPath).Ensure(providerID)
}

func EnsureContext(ctx context.Context, providerID string) (Auth, error) {
	return NewStore(defaultTokenPath).EnsureContext(ctx, providerID)
}

var sharedStores sync.Map

// -------------------------------------------------------------------------------------
func NewStore(path string) *Store {
	if strings.TrimSpace(path) == "" {
		path = defaultTokenPath
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	store, _ := sharedStores.LoadOrStore(path, &Store{Path: path})
	return store.(*Store)
}

// -------------------------------------------------------------------------------------
func Start(options StartOptions) (StartResult, error) {
	return NewStore(defaultTokenPath).Start(options)
}

// -------------------------------------------------------------------------------------
func StatusFor(providerID string) (Status, error) {
	return NewStore(defaultTokenPath).Status(providerID)
}

// AccountStatusSnapshot 批次讀取本地帳號資訊，監看流程不更新 token 或連線上游。
func AccountStatusSnapshot() (map[string]Status, error) {
	s := NewStore(defaultTokenPath)
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	result := make(map[string]Status, len(records))
	for _, record := range records {
		status := "expired"
		if strings.TrimSpace(record.AccessToken) != "" && tokenUsable(record.ExpiresAt) {
			status = "connected"
		}
		result[record.ProviderID] = Status{
			ProviderID: record.ProviderID, Status: status, Flow: "oauth",
			AccountEmail: record.AccountEmail, AccountName: record.AccountName,
			ExpiresAt: record.ExpiresAt,
		}
	}
	return result, nil
}

// -------------------------------------------------------------------------------------
func CompleteManual(providerID string, input string) (OAuthTokenRecord, error) {
	return NewStore(defaultTokenPath).CompleteManual(providerID, input)
}

// -------------------------------------------------------------------------------------
func HandleCallback(state string, code string, callbackErr string, callbackErrDesc string) (OAuthTokenRecord, string, error) {
	return NewStore(defaultTokenPath).HandleCallback(state, code, callbackErr, callbackErrDesc)
}

// -------------------------------------------------------------------------------------
func (s *Store) Start(options StartOptions) (StartResult, error) {
	providerID := strings.TrimSpace(options.ProviderID)
	if providerID == "" {
		return StartResult{}, fmt.Errorf("provider id is required")
	}
	flow := resolveOAuthFlow(options.FlowPreference)
	if flow == "device" {
		result, err := s.startDeviceFlow(providerID)
		if err == nil {
			return result, nil
		}
		result, browserErr := s.startBrowserFlow(providerID, options.LaunchBrowser)
		if browserErr != nil {
			return StartResult{}, err
		}
		result.Message = "device oauth is unavailable; browser oauth started"
		return result, nil
	}
	return s.startBrowserFlow(providerID, options.LaunchBrowser)
}

// -------------------------------------------------------------------------------------
func (s *Store) Status(providerID string) (Status, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return Status{}, fmt.Errorf("provider id is required")
	}
	if record, err := s.Get(providerID); err == nil && strings.TrimSpace(record.AccessToken) != "" && tokenUsable(record.ExpiresAt) {
		return Status{
			ProviderID:   providerID,
			Status:       "connected",
			Flow:         "oauth",
			AccountEmail: record.AccountEmail,
			AccountName:  record.AccountName,
			ExpiresAt:    record.ExpiresAt,
		}, nil
	} else if err == nil && strings.TrimSpace(record.RefreshToken) != "" {
		if _, ensureErr := s.Ensure(providerID); ensureErr == nil {
			refreshed, _ := s.Get(providerID)
			return Status{
				ProviderID:   providerID,
				Status:       "connected",
				Flow:         "oauth",
				AccountEmail: refreshed.AccountEmail,
				AccountName:  refreshed.AccountName,
				ExpiresAt:    refreshed.ExpiresAt,
			}, nil
		}
	}
	if session, ok := getPendingSessionByProvider(providerID); ok {
		status := Status{
			ProviderID:              providerID,
			Flow:                    session.Flow,
			Status:                  firstNonEmpty(session.Status, "pending"),
			LastError:               session.LastError,
			Message:                 session.LastError,
			VerificationURI:         session.VerificationURI,
			VerificationURIComplete: session.VerificationURIComplete,
			UserCode:                session.UserCode,
		}
		if !session.ExpiresAt.IsZero() {
			status.ExpiresAt = session.ExpiresAt.Format(time.RFC3339Nano)
		}
		if session.Status == "pending" && !session.ExpiresAt.IsZero() && time.Now().After(session.ExpiresAt) {
			status.Status = "failed"
			status.LastError = "oauth session expired"
			status.Message = status.LastError
			updatePendingSessionFailure(providerID, status.LastError)
		}
		return status, nil
	}
	return Status{ProviderID: providerID, Status: "idle"}, nil
}

// -------------------------------------------------------------------------------------
func (s *Store) CompleteManual(providerID string, input string) (OAuthTokenRecord, error) {
	session, ok := getPendingSessionByProvider(strings.TrimSpace(providerID))
	if !ok {
		return OAuthTokenRecord{}, fmt.Errorf("oauth session not found or expired")
	}
	code, state, err := parseManualOAuthInput(input)
	if err != nil {
		return OAuthTokenRecord{}, err
	}
	if state != "" && state != session.State {
		return OAuthTokenRecord{}, fmt.Errorf("oauth state mismatch")
	}
	record, _, err := s.HandleCallback(session.State, code, "", "")
	return record, err
}

// -------------------------------------------------------------------------------------
func (s *Store) HandleCallback(state string, code string, callbackErr string, callbackErrDesc string) (OAuthTokenRecord, string, error) {
	session, ok := getPendingSessionByState(strings.TrimSpace(state))
	if !ok {
		return OAuthTokenRecord{}, "oauth session not found or expired", fmt.Errorf("oauth session not found or expired")
	}
	if strings.TrimSpace(callbackErr) != "" {
		reason := "callback failed: " + firstNonEmpty(callbackErrDesc, callbackErr)
		updatePendingSessionFailure(session.ProviderID, reason)
		return OAuthTokenRecord{}, reason, fmt.Errorf("%s", reason)
	}
	if strings.TrimSpace(code) == "" {
		reason := "callback is missing authorization code"
		updatePendingSessionFailure(session.ProviderID, reason)
		return OAuthTokenRecord{}, reason, fmt.Errorf("%s", reason)
	}
	token, userInfo, err := exchangeAuthorizationCode(strings.TrimSpace(code), session)
	if err != nil {
		updatePendingSessionFailure(session.ProviderID, err.Error())
		return OAuthTokenRecord{}, err.Error(), err
	}
	record, err := s.persistToken(session.ProviderID, session.Flow, token, userInfo, "")
	if err != nil {
		updatePendingSessionFailure(session.ProviderID, err.Error())
		return OAuthTokenRecord{}, err.Error(), err
	}
	updatePendingSessionCompleted(session.ProviderID)
	return record, "", nil
}

// -------------------------------------------------------------------------------------
func (s *Store) startBrowserFlow(providerID string, launchBrowser bool) (StartResult, error) {
	state, err := randomURLSafe(32)
	if err != nil {
		return StartResult{}, err
	}
	verifier, err := randomURLSafe(48)
	if err != nil {
		return StartResult{}, err
	}
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", openAIClientID)
	values.Set("redirect_uri", oauthRedirectURI)
	values.Set("scope", defaultOAuthScope)
	values.Set("state", state)
	values.Set("code_challenge", pkceS256Challenge(verifier))
	values.Set("code_challenge_method", "S256")
	values.Set("id_token_add_organizations", "true")
	values.Set("codex_cli_simplified_flow", "true")
	values.Set("originator", "pi")
	authorizationURL := openAIAuthURL + "?" + values.Encode()

	setPendingSession(pendingSession{
		ProviderID:   providerID,
		Flow:         "browser",
		State:        state,
		Status:       "pending",
		CodeVerifier: verifier,
		RedirectURI:  oauthRedirectURI,
		CreatedAt:    time.Now(),
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	})

	callbackListening := startCallbackListener()
	browserOpened := false
	if launchBrowser {
		browserOpened = tryOpenBrowser(authorizationURL)
	}
	return StartResult{
		ProviderID:               providerID,
		Flow:                     "browser",
		Status:                   "pending",
		AuthorizationURL:         authorizationURL,
		CallbackListening:        callbackListening,
		BrowserOpened:            browserOpened,
		ManualCompletionRequired: !callbackListening,
		Message:                  "browser oauth started",
	}, nil
}

// -------------------------------------------------------------------------------------
func (s *Store) startDeviceFlow(providerID string) (StartResult, error) {
	values := url.Values{}
	values.Set("client_id", openAIClientID)
	values.Set("scope", defaultOAuthScope)
	values.Set("id_token_add_organizations", "true")
	values.Set("codex_cli_simplified_flow", "true")
	values.Set("originator", "pi")
	body, err := postOpenAIForm(openAIDeviceCodeURL, values, "device code request")
	if err != nil {
		return StartResult{}, err
	}
	var payload struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
		Error                   string `json:"error"`
		ErrorDescription        string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return StartResult{}, err
	}
	if payload.Error != "" {
		return StartResult{}, fmt.Errorf("%s", firstNonEmpty(payload.ErrorDescription, payload.Error))
	}
	interval := payload.Interval
	if interval <= 0 {
		interval = 5
	}
	expiresIn := payload.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 900
	}
	session := pendingSession{
		ProviderID:              providerID,
		Flow:                    "device",
		Status:                  "pending",
		DeviceCode:              strings.TrimSpace(payload.DeviceCode),
		UserCode:                strings.TrimSpace(payload.UserCode),
		VerificationURI:         strings.TrimSpace(payload.VerificationURI),
		VerificationURIComplete: strings.TrimSpace(payload.VerificationURIComplete),
		CreatedAt:               time.Now(),
		ExpiresAt:               time.Now().Add(time.Duration(expiresIn) * time.Second),
		Interval:                time.Duration(interval) * time.Second,
	}
	setPendingSession(session)
	go s.pollDeviceFlow(session)
	return StartResult{
		ProviderID:               providerID,
		Flow:                     "device",
		Status:                   "pending",
		VerificationURI:          session.VerificationURI,
		VerificationURIComplete:  session.VerificationURIComplete,
		UserCode:                 session.UserCode,
		ExpiresIn:                expiresIn,
		Interval:                 interval,
		ManualCompletionRequired: false,
		Message:                  "device oauth started",
	}, nil
}

// -------------------------------------------------------------------------------------
func (s *Store) Ensure(providerID string) (Auth, error) {
	return s.EnsureContext(context.Background(), providerID)
}

func (s *Store) ensureLocked(ctx context.Context, providerID string) (Auth, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return Auth{}, fmt.Errorf("provider id is required")
	}

	record, err := s.Get(providerID)
	if err == nil && tokenUsable(record.ExpiresAt) && strings.TrimSpace(record.AccessToken) != "" {
		return Auth{AccessToken: strings.TrimSpace(record.AccessToken), AccountID: accountID(record)}, nil
	}
	if err == nil && strings.TrimSpace(record.RefreshToken) != "" {
		if refreshed, refreshErr := refreshTokenContext(ctx, record); refreshErr == nil {
			next := mergeToken(record, refreshed)
			if saveErr := s.Save(next); saveErr != nil {
				return Auth{}, saveErr
			}
			return Auth{AccessToken: strings.TrimSpace(next.AccessToken), AccountID: accountID(next)}, nil
		} else {
			if ctx.Err() != nil {
				return Auth{}, ctx.Err()
			}
			record.RefreshFailed = refreshErr.Error()
			_ = s.Save(record)
		}
	}

	imported, importErr := importCodexCLIAuth(providerID)
	if importErr != nil {
		if err != nil {
			return Auth{}, err
		}
		return Auth{}, importErr
	}
	if tokenUsable(imported.ExpiresAt) {
		if saveErr := s.Save(imported); saveErr != nil {
			return Auth{}, saveErr
		}
		return Auth{AccessToken: strings.TrimSpace(imported.AccessToken), AccountID: accountID(imported)}, nil
	}
	if strings.TrimSpace(imported.RefreshToken) == "" {
		return Auth{}, fmt.Errorf("codex oauth token expired and refresh token is missing")
	}
	refreshed, refreshErr := refreshTokenContext(ctx, imported)
	if refreshErr != nil {
		return Auth{}, refreshErr
	}
	next := mergeToken(imported, refreshed)
	if saveErr := s.Save(next); saveErr != nil {
		return Auth{}, saveErr
	}
	return Auth{AccessToken: strings.TrimSpace(next.AccessToken), AccountID: accountID(next)}, nil
}

// -------------------------------------------------------------------------------------
func (s *Store) Get(providerID string) (OAuthTokenRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.loadLocked()
	if err != nil {
		return OAuthTokenRecord{}, err
	}
	providerID = strings.TrimSpace(providerID)
	for _, item := range items {
		if strings.TrimSpace(item.ProviderID) == providerID {
			return item, nil
		}
	}
	return OAuthTokenRecord{}, fmt.Errorf("oauth token not found")
}

// -------------------------------------------------------------------------------------
func (s *Store) Save(record OAuthTokenRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := s.loadLocked()
	if err != nil {
		return err
	}
	record.ProviderID = strings.TrimSpace(record.ProviderID)
	record.Provider = strings.TrimSpace(firstNonEmpty(record.Provider, defaultCodexProvider))
	record.UpdatedAt = time.Now().Format(time.RFC3339Nano)
	if record.ProviderID == "" {
		return fmt.Errorf("provider id is required")
	}
	replaced := false
	for idx := range items {
		if strings.TrimSpace(items[idx].ProviderID) == record.ProviderID {
			items[idx] = record
			replaced = true
			break
		}
	}
	if !replaced {
		items = append(items, record)
	}
	return s.saveLocked(items)
}

// -------------------------------------------------------------------------------------
func (s *Store) loadLocked() ([]OAuthTokenRecord, error) {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return []OAuthTokenRecord{}, nil
		}
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return []OAuthTokenRecord{}, nil
	}
	var items []OAuthTokenRecord
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// -------------------------------------------------------------------------------------
func (s *Store) saveLocked(items []OAuthTokenRecord) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(s.Path, data, 0600)
}

// -------------------------------------------------------------------------------------
func importCodexCLIAuth(providerID string) (OAuthTokenRecord, error) {
	auth, err := loadCodexCLIAuth()
	if err != nil {
		return OAuthTokenRecord{}, err
	}
	if strings.TrimSpace(auth.Tokens.AccessToken) == "" || strings.TrimSpace(auth.Tokens.RefreshToken) == "" {
		return OAuthTokenRecord{}, fmt.Errorf("codex cli auth is missing access_token or refresh_token")
	}
	expiresAt := jwtExpiresAt(auth.Tokens.AccessToken)
	if expiresAt.IsZero() {
		expiresAt = codexLastRefreshExpiry(auth.LastRefresh)
	}
	return OAuthTokenRecord{
		ProviderID:   providerID,
		Provider:     defaultCodexProvider,
		TokenType:    "Bearer",
		AccessToken:  strings.TrimSpace(auth.Tokens.AccessToken),
		RefreshToken: strings.TrimSpace(auth.Tokens.RefreshToken),
		IDToken:      strings.TrimSpace(auth.Tokens.IDToken),
		Scope:        "openid profile email offline_access",
		ExpiresAt:    formatTime(expiresAt),
		AccountSub:   firstNonEmpty(strings.TrimSpace(auth.Tokens.AccountID), stringClaim(jwtClaims(auth.Tokens.IDToken), "sub")),
		AccountEmail: stringClaim(jwtClaims(auth.Tokens.IDToken), "email"),
		AccountName:  stringClaim(jwtClaims(auth.Tokens.IDToken), "name"),
	}, nil
}

// -------------------------------------------------------------------------------------
func loadCodexCLIAuth() (cliAuthFile, error) {
	path, err := codexCLIAuthPath()
	if err != nil {
		return cliAuthFile{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cliAuthFile{}, err
	}
	var auth cliAuthFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return cliAuthFile{}, err
	}
	return auth, nil
}

// -------------------------------------------------------------------------------------
func codexCLIAuthPath() (string, error) {
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		return filepath.Join(codexHome, "auth.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "auth.json"), nil
}

// -------------------------------------------------------------------------------------
func (s *Store) pollDeviceFlow(session pendingSession) {
	interval := session.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if time.Now().After(session.ExpiresAt) {
			updatePendingSessionFailure(session.ProviderID, "device code expired")
			return
		}
		token, userInfo, wait, err := exchangeDeviceCode(session.DeviceCode)
		if err == nil {
			_, persistErr := s.persistToken(session.ProviderID, session.Flow, token, userInfo, "")
			if persistErr != nil {
				updatePendingSessionFailure(session.ProviderID, persistErr.Error())
				return
			}
			updatePendingSessionCompleted(session.ProviderID)
			return
		}
		if wait {
			<-ticker.C
			continue
		}
		updatePendingSessionFailure(session.ProviderID, err.Error())
		return
	}
}

// -------------------------------------------------------------------------------------
func exchangeAuthorizationCode(code string, session pendingSession) (tokenResponse, openAIUserInfo, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("client_id", openAIClientID)
	values.Set("code", strings.TrimSpace(code))
	values.Set("code_verifier", session.CodeVerifier)
	values.Set("redirect_uri", session.RedirectURI)
	token, err := postOpenAIToken(openAITokenURL, values, "authorization code token exchange")
	if err != nil {
		return tokenResponse{}, openAIUserInfo{}, err
	}
	userInfo := userInfoFromToken(token)
	if fetched, fetchErr := fetchUserInfo(token.AccessToken); fetchErr == nil {
		userInfo = mergeUserInfo(userInfo, fetched)
	}
	return token, userInfo, nil
}

// -------------------------------------------------------------------------------------
func exchangeDeviceCode(deviceCode string) (tokenResponse, openAIUserInfo, bool, error) {
	values := url.Values{}
	values.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	values.Set("device_code", strings.TrimSpace(deviceCode))
	values.Set("client_id", openAIClientID)
	token, err := postOpenAIToken(openAITokenURL, values, "device code token exchange")
	if err != nil {
		if strings.Contains(err.Error(), "authorization_pending") || strings.Contains(err.Error(), "slow_down") {
			return tokenResponse{}, openAIUserInfo{}, true, nil
		}
		return tokenResponse{}, openAIUserInfo{}, false, err
	}
	userInfo := userInfoFromToken(token)
	if fetched, fetchErr := fetchUserInfo(token.AccessToken); fetchErr == nil {
		userInfo = mergeUserInfo(userInfo, fetched)
	}
	return token, userInfo, false, nil
}

// -------------------------------------------------------------------------------------
func (s *Store) persistToken(providerID string, flow string, token tokenResponse, userInfo openAIUserInfo, lastError string) (OAuthTokenRecord, error) {
	expiresAt := ""
	if token.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).Format(time.RFC3339Nano)
	} else if parsed := jwtExpiresAt(token.AccessToken); !parsed.IsZero() {
		expiresAt = parsed.Format(time.RFC3339Nano)
	}
	existing, _ := s.Get(providerID)
	refreshToken := firstNonEmpty(token.RefreshToken, existing.RefreshToken)
	record := OAuthTokenRecord{
		ProviderID:    strings.TrimSpace(providerID),
		Provider:      defaultCodexProvider,
		TokenType:     firstNonEmpty(token.TokenType, existing.TokenType, "Bearer"),
		AccessToken:   strings.TrimSpace(token.AccessToken),
		RefreshToken:  refreshToken,
		IDToken:       firstNonEmpty(token.IDToken, existing.IDToken),
		Scope:         firstNonEmpty(token.Scope, existing.Scope, defaultOAuthScope),
		ExpiresAt:     firstNonEmpty(expiresAt, existing.ExpiresAt),
		AccountEmail:  firstNonEmpty(userInfo.Email, existing.AccountEmail),
		AccountName:   firstNonEmpty(userInfo.Name, existing.AccountName),
		AccountSub:    firstNonEmpty(userInfo.Sub, existing.AccountSub),
		RefreshFailed: strings.TrimSpace(lastError),
	}
	if err := s.Save(record); err != nil {
		return OAuthTokenRecord{}, err
	}
	return record, nil
}

// -------------------------------------------------------------------------------------
func postOpenAIToken(endpoint string, values url.Values, stage string) (tokenResponse, error) {
	body, err := postOpenAIForm(endpoint, values, stage)
	if err != nil {
		return tokenResponse{}, err
	}
	var token tokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return tokenResponse{}, fmt.Errorf("%s failed: token endpoint returned non-json response: %s", stage, summarizeOAuthResponseBody(body))
	}
	if token.Error != "" {
		return tokenResponse{}, fmt.Errorf("%s failed: %s", stage, firstNonEmpty(token.Description, token.Error))
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return tokenResponse{}, fmt.Errorf("%s failed: token response missing access_token", stage)
	}
	return token, nil
}

// -------------------------------------------------------------------------------------
func postOpenAIForm(endpoint string, values url.Values, stage string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w", stage, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", "codex_cli_rs/0.129.0 (Mac OS; arm64)")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w", stage, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode >= 400 {
		return data, fmt.Errorf("%s failed: token endpoint returned %d: %s", stage, resp.StatusCode, summarizeOAuthResponseBody(data))
	}
	return data, nil
}

// -------------------------------------------------------------------------------------
func fetchUserInfo(accessToken string) (openAIUserInfo, error) {
	req, err := http.NewRequest(http.MethodGet, openAIUserInfoURL, nil)
	if err != nil {
		return openAIUserInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return openAIUserInfo{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if resp.StatusCode >= 400 {
		return openAIUserInfo{}, fmt.Errorf("userinfo failed: %s", strings.TrimSpace(string(data)))
	}
	var info openAIUserInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return openAIUserInfo{}, err
	}
	return info, nil
}

// -------------------------------------------------------------------------------------
func refreshToken(record OAuthTokenRecord) (tokenResponse, error) {
	return refreshTokenContext(context.Background(), record)
}

func refreshTokenContext(ctx context.Context, record OAuthTokenRecord) (tokenResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("client_id", openAIClientID)
	values.Set("refresh_token", strings.TrimSpace(record.RefreshToken))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAITokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "codex_cli_rs/0.129.0 (Mac OS; arm64)")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if resp.StatusCode >= 400 {
		return tokenResponse{}, fmt.Errorf("refresh token exchange failed: status %d: %s", resp.StatusCode, summarize(data))
	}
	var token tokenResponse
	if err := json.Unmarshal(data, &token); err != nil {
		return tokenResponse{}, err
	}
	if token.Error != "" {
		return tokenResponse{}, fmt.Errorf("refresh token exchange failed: %s", firstNonEmpty(token.Description, token.Error))
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return tokenResponse{}, fmt.Errorf("refresh token exchange failed: missing access_token")
	}
	return token, nil
}

// -------------------------------------------------------------------------------------
func mergeToken(record OAuthTokenRecord, token tokenResponse) OAuthTokenRecord {
	expiresAt := ""
	if token.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second).Format(time.RFC3339Nano)
	} else if parsed := jwtExpiresAt(token.AccessToken); !parsed.IsZero() {
		expiresAt = parsed.Format(time.RFC3339Nano)
	}
	claims := jwtClaims(token.IDToken)
	return OAuthTokenRecord{
		ProviderID:   record.ProviderID,
		Provider:     firstNonEmpty(record.Provider, defaultCodexProvider),
		TokenType:    firstNonEmpty(strings.TrimSpace(token.TokenType), record.TokenType, "Bearer"),
		AccessToken:  strings.TrimSpace(token.AccessToken),
		RefreshToken: firstNonEmpty(strings.TrimSpace(token.RefreshToken), record.RefreshToken),
		IDToken:      firstNonEmpty(strings.TrimSpace(token.IDToken), record.IDToken),
		Scope:        firstNonEmpty(strings.TrimSpace(token.Scope), record.Scope),
		ExpiresAt:    firstNonEmpty(expiresAt, record.ExpiresAt),
		AccountEmail: firstNonEmpty(stringClaim(claims, "email"), record.AccountEmail),
		AccountName:  firstNonEmpty(stringClaim(claims, "name"), record.AccountName),
		AccountSub:   firstNonEmpty(record.AccountSub, accountID(record)),
	}
}

// -------------------------------------------------------------------------------------
func tokenUsable(expiresAt string) bool {
	expiresAt = strings.TrimSpace(expiresAt)
	if expiresAt == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, expiresAt)
	}
	if err != nil {
		return false
	}
	return time.Until(parsed) > 90*time.Second
}

// -------------------------------------------------------------------------------------
func accountID(record OAuthTokenRecord) string {
	claims := jwtClaims(record.AccessToken)
	authClaims, _ := claims["https://api.openai.com/auth"].(map[string]interface{})
	return firstNonEmpty(strings.TrimSpace(record.AccountSub), stringClaim(authClaims, "chatgpt_account_id"))
}

// -------------------------------------------------------------------------------------
func jwtExpiresAt(token string) time.Time {
	claims := jwtClaims(token)
	exp, ok := claims["exp"].(float64)
	if !ok || exp <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(exp), 0)
}

// -------------------------------------------------------------------------------------
func jwtClaims(token string) map[string]interface{} {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return map[string]interface{}{}
	}
	payload := parts[1]
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return map[string]interface{}{}
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(data, &claims); err != nil {
		return map[string]interface{}{}
	}
	return claims
}

// -------------------------------------------------------------------------------------
func stringClaim(claims map[string]interface{}, key string) string {
	if strings.Contains(key, ".") {
		parts := strings.Split(key, ".")
		var current interface{} = claims
		for _, part := range parts {
			mapped, ok := current.(map[string]interface{})
			if !ok {
				return ""
			}
			current = mapped[part]
		}
		if text, ok := current.(string); ok {
			return strings.TrimSpace(text)
		}
		return ""
	}
	if text, ok := claims[key].(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

// -------------------------------------------------------------------------------------
func codexLastRefreshExpiry(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, strings.TrimSpace(value))
	}
	if err != nil {
		return time.Time{}
	}
	return parsed.Add(28 * 24 * time.Hour)
}

// -------------------------------------------------------------------------------------
func userInfoFromToken(token tokenResponse) openAIUserInfo {
	idClaims := jwtClaims(token.IDToken)
	accessClaims := jwtClaims(token.AccessToken)
	authClaims, _ := idClaims["https://api.openai.com/auth"].(map[string]interface{})
	if len(authClaims) == 0 {
		authClaims, _ = accessClaims["https://api.openai.com/auth"].(map[string]interface{})
	}
	profileClaims, _ := accessClaims["https://api.openai.com/profile"].(map[string]interface{})
	return openAIUserInfo{
		Sub:   firstNonEmpty(stringClaim(authClaims, "chatgpt_account_id"), stringClaim(accessClaims, "sub"), stringClaim(idClaims, "sub")),
		Email: firstNonEmpty(stringClaim(idClaims, "email"), stringClaim(profileClaims, "email")),
		Name:  stringClaim(idClaims, "name"),
	}
}

// -------------------------------------------------------------------------------------
func mergeUserInfo(base openAIUserInfo, next openAIUserInfo) openAIUserInfo {
	return openAIUserInfo{
		Sub:   firstNonEmpty(base.Sub, next.Sub),
		Email: firstNonEmpty(base.Email, next.Email),
		Name:  firstNonEmpty(base.Name, next.Name),
	}
}

// -------------------------------------------------------------------------------------
func parseManualOAuthInput(input string) (string, string, error) {
	text := strings.TrimSpace(input)
	if text == "" {
		return "", "", fmt.Errorf("oauth input is required")
	}
	if parsed, err := url.Parse(text); err == nil && strings.TrimSpace(parsed.Scheme) != "" {
		query := parsed.Query()
		code := strings.TrimSpace(query.Get("code"))
		if code == "" {
			return "", "", fmt.Errorf("oauth redirect url is missing code")
		}
		return code, strings.TrimSpace(query.Get("state")), nil
	}
	return text, "", nil
}

// -------------------------------------------------------------------------------------
func startCallbackListener() bool {
	listener, err := net.Listen("tcp", "localhost:1455")
	if err != nil {
		return false
	}
	mux := http.NewServeMux()
	server := &http.Server{Handler: mux}
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		record, reason, callbackErr := HandleCallback(
			strings.TrimSpace(query.Get("state")),
			strings.TrimSpace(query.Get("code")),
			strings.TrimSpace(query.Get("error")),
			strings.TrimSpace(query.Get("error_description")),
		)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if callbackErr != nil {
			_, _ = w.Write(RenderCallbackHTML(false, reason))
		} else {
			_, _ = w.Write(RenderCallbackHTML(true, "OpenAI Codex OAuth 已連線："+firstNonEmpty(record.AccountEmail, record.AccountName, record.ProviderID)))
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
		}()
	})
	go func() {
		_ = server.Serve(listener)
	}()
	return true
}

// -------------------------------------------------------------------------------------
func RenderCallbackHTML(success bool, message string) []byte {
	message = strings.TrimSpace(message)
	if message == "" {
		if success {
			message = "OpenAI Codex OAuth 已完成，您可以回到原視窗繼續。"
		} else {
			message = "OpenAI Codex OAuth 失敗，請回到原視窗檢查錯誤。"
		}
	}
	status := "失敗"
	if success {
		status = "成功"
	}
	var builder strings.Builder
	builder.WriteString("<!DOCTYPE html><html lang=\"zh-Hant\"><head><meta charset=\"UTF-8\"><title>OpenAI Codex OAuth</title></head><body style=\"font-family:sans-serif;padding:24px;\">")
	builder.WriteString("<h2>OpenAI Codex OAuth " + status + "</h2>")
	builder.WriteString("<p>" + html.EscapeString(message) + "</p>")
	builder.WriteString("<script>try{if(window.opener){window.opener.postMessage({type:'provider-oauth-complete',success:")
	if success {
		builder.WriteString("true")
	} else {
		builder.WriteString("false")
	}
	builder.WriteString(",message:")
	encoded, _ := json.Marshal(message)
	builder.Write(encoded)
	builder.WriteString("},'*');}}catch(e){}")
	if success {
		builder.WriteString(" setTimeout(function(){window.close();}, 500);")
	} else {
		builder.WriteString(" document.body.insertAdjacentHTML('beforeend','<p style=\"color:#666;\">請保留此頁網址，回到原視窗貼上網址列中的 redirect URL 或 code。</p>');")
	}
	builder.WriteString("</script></body></html>")
	return []byte(builder.String())
}

// -------------------------------------------------------------------------------------
func resolveOAuthFlow(preference string) string {
	switch strings.ToLower(strings.TrimSpace(preference)) {
	case "device", "cli":
		return "device"
	case "browser", "web":
		return "browser"
	default:
		return "browser"
	}
}

// -------------------------------------------------------------------------------------
func randomURLSafe(length int) (string, error) {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(buf), "="), nil
}

// -------------------------------------------------------------------------------------
func pkceS256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(sum[:]), "=")
}

// -------------------------------------------------------------------------------------
func tryOpenBrowser(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	if browser := strings.TrimSpace(os.Getenv("BROWSER")); browser != "" {
		return exec.Command(browser, target).Start() == nil
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target).Start() == nil
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start() == nil
	default:
		return exec.Command("xdg-open", target).Start() == nil
	}
}

// -------------------------------------------------------------------------------------
func setPendingSession(session pendingSession) {
	pendingSessions.mu.Lock()
	defer pendingSessions.mu.Unlock()
	if existing, ok := pendingSessions.byProvider[session.ProviderID]; ok && existing.State != "" {
		delete(pendingSessions.byState, existing.State)
	}
	if session.State != "" {
		pendingSessions.byState[session.State] = session
	}
	pendingSessions.byProvider[session.ProviderID] = session
}

// -------------------------------------------------------------------------------------
func getPendingSessionByState(state string) (pendingSession, bool) {
	pendingSessions.mu.Lock()
	defer pendingSessions.mu.Unlock()
	item, ok := pendingSessions.byState[strings.TrimSpace(state)]
	return item, ok
}

// -------------------------------------------------------------------------------------
func getPendingSessionByProvider(providerID string) (pendingSession, bool) {
	pendingSessions.mu.Lock()
	defer pendingSessions.mu.Unlock()
	item, ok := pendingSessions.byProvider[strings.TrimSpace(providerID)]
	return item, ok
}

// -------------------------------------------------------------------------------------
func updatePendingSessionFailure(providerID string, reason string) {
	pendingSessions.mu.Lock()
	defer pendingSessions.mu.Unlock()
	session, ok := pendingSessions.byProvider[strings.TrimSpace(providerID)]
	if !ok {
		return
	}
	session.Status = "failed"
	session.LastError = strings.TrimSpace(reason)
	if session.State != "" {
		pendingSessions.byState[session.State] = session
	}
	pendingSessions.byProvider[providerID] = session
}

// -------------------------------------------------------------------------------------
func updatePendingSessionCompleted(providerID string) {
	pendingSessions.mu.Lock()
	defer pendingSessions.mu.Unlock()
	session, ok := pendingSessions.byProvider[strings.TrimSpace(providerID)]
	if !ok {
		return
	}
	session.Status = "connected"
	session.LastError = ""
	session.ExpiresAt = time.Now().Add(5 * time.Minute)
	if session.State != "" {
		pendingSessions.byState[session.State] = session
	}
	pendingSessions.byProvider[providerID] = session
}

// -------------------------------------------------------------------------------------
func summarizeOAuthResponseBody(data []byte) string {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return "empty response"
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "cf-mitigated") || strings.Contains(lower, "challenge-error-text") || strings.Contains(lower, "enable javascript and cookies"):
		return "auth.openai.com returned a Cloudflare/browser challenge page; retry with a different network/proxy"
	case strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html"):
		return "auth.openai.com returned an HTML page instead of OAuth JSON"
	}
	if len(text) > 500 {
		text = text[:500] + "..."
	}
	return text
}

// -------------------------------------------------------------------------------------
func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}

// -------------------------------------------------------------------------------------
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// -------------------------------------------------------------------------------------
func summarize(data []byte) string {
	text := strings.TrimSpace(string(data))
	if len(text) > 500 {
		return text[:500] + "..."
	}
	if text == "" {
		return "empty response"
	}
	return text
}
