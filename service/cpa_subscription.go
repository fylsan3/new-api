package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// Subscription accounts: a root-only read view (plus a few confirmed write
// actions) over CLIProxyAPI v8 credentials. All CPA traffic goes through the
// CPA management API; tokens never reach New API or the browser.

const (
	cpaManagementPrefix = "/v8/management"
	cpaRequestTimeout   = 30 * time.Second
	cpaSecretPrefix     = "v1:"

	claudeUsageURL   = "https://api.anthropic.com/api/oauth/usage"
	claudeProfileURL = "https://api.anthropic.com/api/oauth/profile"
	claudeAPIOrigin  = "https://api.anthropic.com"
)

var cpaHTTPClient = &http.Client{Timeout: cpaRequestTimeout}

func claudeRequestHeaders(accept bool) map[string]string {
	headers := map[string]string{
		"User-Agent":     "claude-cli/2.1.280 (external, cli)",
		"Authorization":  "Bearer $TOKEN$",
		"Content-Type":   "application/json",
		"anthropic-beta": "oauth-2025-04-20",
	}
	if accept {
		headers["Accept"] = "application/json"
	}
	return headers
}

// ---- secret storage ----

func cpaSecretKey() []byte {
	sum := sha256.Sum256([]byte("cpa-subscription:" + common.CryptoSecret))
	return sum[:]
}

func EncryptCpaSecret(plain string) (string, error) {
	block, err := aes.NewCipher(cpaSecretKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return cpaSecretPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func DecryptCpaSecret(stored string) (string, error) {
	if !strings.HasPrefix(stored, cpaSecretPrefix) {
		return "", errors.New("unsupported secret format")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, cpaSecretPrefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(cpaSecretKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("secret is corrupted")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("secret cannot be decrypted; re-enter the management key")
	}
	return string(plain), nil
}

// NormalizeCpaBaseURL validates and trims a CPA remote management address.
func NormalizeCpaBaseURL(raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	trimmed = strings.TrimSuffix(trimmed, cpaManagementPrefix)
	trimmed = strings.TrimSuffix(trimmed, "/v0/management")
	parsed, err := url.Parse(trimmed)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("invalid CPA address, expected http(s)://host:port")
	}
	return trimmed, nil
}

// ---- CPA management client ----

type cpaClient struct {
	baseURL string
	secret  string
}

func newCpaClient(instance *model.CpaInstance) (*cpaClient, error) {
	secret, err := DecryptCpaSecret(instance.SecretEnc)
	if err != nil {
		return nil, err
	}
	return &cpaClient{baseURL: instance.BaseURL, secret: secret}, nil
}

type cpaHTTPError struct {
	Status  int
	Message string
}

func (e *cpaHTTPError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("CPA returned HTTP %d", e.Status)
	}
	return fmt.Sprintf("CPA returned HTTP %d: %s", e.Status, e.Message)
}

func (c *cpaClient) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	return c.doRaw(ctx, method, cpaManagementPrefix+path, query, body, out)
}

// doRaw calls a management path outside the v8 prefix (e.g. v0-only routes).
func (c *cpaClient) doRaw(ctx context.Context, method, fullPath string, query url.Values, body any, out any) error {
	endpoint := c.baseURL + fullPath
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		payload, err := common.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := cpaHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach CPA: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &cpaHTTPError{Status: resp.StatusCode, Message: extractErrorMessage(data)}
	}
	if out == nil {
		return nil
	}
	return common.Unmarshal(data, out)
}

func extractErrorMessage(data []byte) string {
	var parsed map[string]any
	if err := common.Unmarshal(data, &parsed); err == nil {
		if msg, ok := parsed["error"].(string); ok {
			return truncateText(msg, 200)
		}
		if inner, ok := parsed["error"].(map[string]any); ok {
			if msg, ok := inner["message"].(string); ok {
				return truncateText(msg, 200)
			}
		}
		if msg, ok := parsed["message"].(string); ok {
			return truncateText(msg, 200)
		}
	}
	return truncateText(strings.TrimSpace(string(data)), 200)
}

func truncateText(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

type cpaCredentialList struct {
	Files []map[string]any `json:"files"`
}

func (c *cpaClient) listCredentials(ctx context.Context) ([]map[string]any, error) {
	var list cpaCredentialList
	if err := c.do(ctx, http.MethodGet, "/credentials", nil, nil, &list); err != nil {
		return nil, err
	}
	return list.Files, nil
}

// credentialProxyURL reads the credential file and keeps only proxy_url; the
// rest of the file (tokens) is discarded immediately.
func (c *cpaClient) credentialProxyURL(ctx context.Context, name string) (string, error) {
	var file map[string]any
	if err := c.do(ctx, http.MethodGet, "/credentials/download", url.Values{"name": {name}}, nil, &file); err != nil {
		return "", err
	}
	proxy, _ := file["proxy_url"].(string)
	return strings.TrimSpace(proxy), nil
}

// ErrCpaProxyBlocked is returned when an account has no usable SOCKS proxy.
// CPA falls back to the global proxy or a direct connection in that case,
// which would expose the server's real IP, so the request is never sent.
var ErrCpaProxyBlocked = errors.New("blocked: account has no valid socks5/socks5h proxy_url, request not sent")

// ValidateSocksProxyURL accepts only socks5:// or socks5h:// with host and a
// numeric port in 1-65535, and nothing after the port. Anything else (empty,
// "direct", "none", http proxies, typos) is rejected because CPA would treat
// it as global/direct.
func ValidateSocksProxyURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ErrCpaProxyBlocked
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ErrCpaProxyBlocked
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "socks5" && scheme != "socks5h" {
		return ErrCpaProxyBlocked
	}
	if parsed.Hostname() == "" || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return ErrCpaProxyBlocked
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return ErrCpaProxyBlocked
	}
	return nil
}

// socksGuardTTL bounds how long one proxy check may be reused by consecutive
// calls of the same operation (e.g. usage + profile + grants in one refresh).
const socksGuardTTL = 60 * time.Second

// socksGuard proves the account's proxy_url was read from CPA and validated
// recently. apiCall only accepts a guard, so no code path can reach Anthropic
// without passing the check.
type socksGuard struct {
	name      string
	proxyURL  string
	checkedAt time.Time
}

// requireSocksProxy re-reads the credential's proxy_url from CPA right before
// any request that CPA will forward to Anthropic, so a proxy removed or broken
// after the last sync is still caught.
func (c *cpaClient) requireSocksProxy(ctx context.Context, name string) (*socksGuard, error) {
	proxy, err := c.credentialProxyURL(ctx, name)
	if err != nil {
		return nil, err
	}
	if err := ValidateSocksProxyURL(proxy); err != nil {
		return &socksGuard{name: name, proxyURL: proxy}, err
	}
	return &socksGuard{name: name, proxyURL: proxy, checkedAt: time.Now()}, nil
}

func (c *cpaClient) refreshGuard(ctx context.Context, guard *socksGuard) error {
	if guard == nil || guard.name == "" {
		return ErrCpaProxyBlocked
	}
	if !guard.checkedAt.IsZero() && time.Since(guard.checkedAt) < socksGuardTTL {
		return nil
	}
	fresh, err := c.requireSocksProxy(ctx, guard.name)
	if err != nil {
		return err
	}
	*guard = *fresh
	return nil
}

type cpaAPICallResult struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}

// apiCall asks CPA to forward one request to Anthropic using the account's own
// proxy. It refuses to send unless the account has a valid SOCKS proxy.
func (c *cpaClient) apiCall(ctx context.Context, guard *socksGuard, authIndex, method, target string, headers map[string]string, data string) (*cpaAPICallResult, error) {
	if err := c.refreshGuard(ctx, guard); err != nil {
		return nil, err
	}
	payload := map[string]any{
		"auth_index": authIndex,
		"method":     method,
		"url":        target,
		"header":     headers,
	}
	if data != "" {
		payload["data"] = data
	}
	var result cpaAPICallResult
	if err := c.do(ctx, http.MethodPost, "/requests/api-call", nil, payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// TestCpaConnection checks the address and key by listing credentials once.
func TestCpaConnection(ctx context.Context, baseURL, secret string) (int, error) {
	client := &cpaClient{baseURL: baseURL, secret: secret}
	files, err := client.listCredentials(ctx)
	if err != nil {
		return 0, err
	}
	return len(files), nil
}

// MaskProxyURL hides the proxy password while keeping scheme/user/host.
func MaskProxyURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "(invalid proxy)"
	}
	if parsed.User != nil {
		if _, has := parsed.User.Password(); has {
			parsed.User = url.UserPassword(parsed.User.Username(), "****")
		}
	}
	out := parsed.String()
	return strings.Replace(out, "%2A%2A%2A%2A", "****", 1)
}

// ---- snapshot ----

var cpaCredentialFields = []string{
	"id", "auth_index", "name", "type", "provider", "label", "status", "status_message",
	"disabled", "unavailable", "runtime_only", "source", "size", "success", "failed",
	"recent_requests", "quota", "model_quotas", "email", "account_type", "account",
	"created_at", "modtime", "updated_at", "last_refresh", "next_retry_after", "priority",
	"note", "weight", "cooldowns", "access_token_expires_at", "requires_sign_in",
	"refresh_error", "renewal_state", "recovery_stage",
}

func filterCredential(raw map[string]any) map[string]any {
	out := make(map[string]any, len(cpaCredentialFields))
	for _, key := range cpaCredentialFields {
		if v, ok := raw[key]; ok {
			out[key] = v
		}
	}
	return out
}

// CpaAccountSnapshot is the cached, browser-safe view of one credential.
type CpaAccountSnapshot struct {
	InstanceID       int            `json:"instance_id"`
	Name             string         `json:"name"`
	Credential       map[string]any `json:"credential"`
	// CredentialSyncedAt is when Credential (incl. recent_requests) was read.
	CredentialSyncedAt int64        `json:"credential_synced_at,omitempty"`
	ProxyURL         string         `json:"proxy_url"`
	ProxyError       string         `json:"proxy_error,omitempty"`
	IsClaude         bool           `json:"is_claude"`
	Usage            map[string]any `json:"usage,omitempty"`
	PlanType         string         `json:"plan_type,omitempty"`
	QuotaObservedAt  int64          `json:"quota_observed_at,omitempty"`
	QuotaCheckedAt   int64          `json:"quota_checked_at,omitempty"`
	QuotaError       string         `json:"quota_error,omitempty"`
	QuotaErrorStatus int            `json:"quota_error_status,omitempty"`
	ResetGrants      map[string]any `json:"reset_grants,omitempty"`
	ResetGrantsError string         `json:"reset_grants_error,omitempty"`
	// ProxyFailStreak counts consecutive refreshes that failed on the SOCKS
	// proxy; at cpaProxyFailDisableThreshold the account is disabled.
	ProxyFailStreak int   `json:"proxy_fail_streak,omitempty"`
	AutoDisabledAt  int64 `json:"auto_disabled_at,omitempty"`
	// LastRequestAt is the newest relay request seen in the usage queue.
	LastRequestAt int64 `json:"last_request_at,omitempty"`
	UpdatedAt     int64 `json:"updated_at"`
}

func credentialString(cred map[string]any, key string) string {
	switch v := cred[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return fmt.Sprintf("%.0f", v)
	}
	return ""
}

func credentialBool(cred map[string]any, key string) bool {
	v, _ := cred[key].(bool)
	return v
}

func isClaudeCredential(cred map[string]any) bool {
	provider := strings.ToLower(credentialString(cred, "provider"))
	if provider == "" {
		provider = strings.ToLower(credentialString(cred, "type"))
	}
	return provider == "claude"
}

func loadSnapshot(instanceID int, name string) (*CpaAccountSnapshot, error) {
	cache, err := model.GetCpaAccountCache(instanceID, name)
	if err != nil {
		return nil, err
	}
	snap := &CpaAccountSnapshot{InstanceID: instanceID, Name: name}
	if cache != nil && cache.Data != "" {
		if err := common.UnmarshalJsonStr(cache.Data, snap); err != nil {
			snap = &CpaAccountSnapshot{InstanceID: instanceID, Name: name}
		}
	}
	return snap, nil
}

func saveSnapshot(snap *CpaAccountSnapshot) error {
	snap.UpdatedAt = common.GetTimestamp()
	data, err := common.Marshal(snap)
	if err != nil {
		return err
	}
	return model.UpsertCpaAccountCache(snap.InstanceID, snap.Name, string(data))
}

// ListCpaSnapshots returns all cached accounts for the page.
func ListCpaSnapshots() ([]CpaAccountSnapshot, error) {
	caches, err := model.ListCpaAccountCaches()
	if err != nil {
		return nil, err
	}
	out := make([]CpaAccountSnapshot, 0, len(caches))
	for _, cache := range caches {
		snap := CpaAccountSnapshot{InstanceID: cache.InstanceID, Name: cache.Name}
		if cache.Data != "" {
			_ = common.UnmarshalJsonStr(cache.Data, &snap)
		}
		out = append(out, snap)
	}
	return out, nil
}
