package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCpaSecretRoundTrip(t *testing.T) {
	previous := common.CryptoSecret
	common.CryptoSecret = "cpa-test-secret"
	t.Cleanup(func() { common.CryptoSecret = previous })

	enc, err := EncryptCpaSecret("my-key")
	require.NoError(t, err)
	assert.NotContains(t, enc, "my-key")
	plain, err := DecryptCpaSecret(enc)
	require.NoError(t, err)
	assert.Equal(t, "my-key", plain)

	common.CryptoSecret = "other"
	_, err = DecryptCpaSecret(enc)
	assert.Error(t, err)
}

func TestNormalizeCpaBaseURL(t *testing.T) {
	got, err := NormalizeCpaBaseURL(" http://127.0.0.1:8317/v8/management/ ")
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:8317", got)
	_, err = NormalizeCpaBaseURL("ftp://x")
	assert.Error(t, err)
}

func TestMaskProxyURL(t *testing.T) {
	assert.Equal(t, "socks5://u:****@1.2.3.4:1080", MaskProxyURL("socks5://u:pass@1.2.3.4:1080"))
	assert.Equal(t, "http://1.2.3.4:8080", MaskProxyURL("http://1.2.3.4:8080"))
	assert.Equal(t, "", MaskProxyURL(""))
}

func TestValidateSocksProxyURL(t *testing.T) {
	ok := []string{
		"socks5://1.2.3.4:1080",
		"socks5h://user:pass@proxy.example.com:443",
		" SOCKS5H://u:p@1.2.3.4:65535/ ",
	}
	for _, raw := range ok {
		assert.NoError(t, ValidateSocksProxyURL(raw), raw)
	}
	bad := []string{
		"", "   ", "direct", "none", "DIRECT",
		"http://1.2.3.4:8080", "https://1.2.3.4:8443",
		"sock5://1.2.3.4:1080", "socks4://1.2.3.4:1080",
		"socks5://1.2.3.4", "socks5://1.2.3.4:", "socks5://1.2.3.4:0", "socks5://1.2.3.4:70000",
		"socks5://:1080", "socks5://1.2.3.4:1080/path", "socks5://1.2.3.4:1080?x=1",
		"1.2.3.4:1080", "socks5:1.2.3.4:1080",
	}
	for _, raw := range bad {
		assert.ErrorIs(t, ValidateSocksProxyURL(raw), ErrCpaProxyBlocked, raw)
	}
}

func TestAPICallBlockedWithoutSocks(t *testing.T) {
	var apiCalls atomic.Int32
	proxy := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v8/management/credentials/download":
			_, _ = w.Write([]byte(`{"proxy_url":"` + proxy + `","access_token":"secret"}`))
		case "/v8/management/requests/api-call":
			apiCalls.Add(1)
			_, _ = w.Write([]byte(`{"status_code":200,"body":"{}"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := &cpaClient{baseURL: server.URL, secret: "k"}
	ctx := context.Background()

	for _, p := range []string{"", "direct", "http://1.2.3.4:8080", "socks5://1.2.3.4"} {
		proxy = p
		guard, err := client.requireSocksProxy(ctx, "a.json")
		assert.ErrorIs(t, err, ErrCpaProxyBlocked, p)
		_, err = client.apiCall(ctx, guard, "1", http.MethodGet, claudeUsageURL, nil, "")
		assert.ErrorIs(t, err, ErrCpaProxyBlocked, p)
	}
	_, err := client.apiCall(ctx, nil, "1", http.MethodGet, claudeUsageURL, nil, "")
	assert.ErrorIs(t, err, ErrCpaProxyBlocked)
	assert.Equal(t, int32(0), apiCalls.Load(), "no request may reach api-call without a valid socks proxy")

	proxy = "socks5h://u:p@1.2.3.4:1080"
	guard, err := client.requireSocksProxy(ctx, "a.json")
	require.NoError(t, err)
	_, err = client.apiCall(ctx, guard, "1", http.MethodGet, claudeUsageURL, nil, "")
	require.NoError(t, err)
	assert.Equal(t, int32(1), apiCalls.Load())

	// An expired guard is re-checked; a proxy removed meanwhile blocks the call.
	guard.checkedAt = time.Now().Add(-2 * socksGuardTTL)
	proxy = ""
	_, err = client.apiCall(ctx, guard, "1", http.MethodGet, claudeUsageURL, nil, "")
	assert.ErrorIs(t, err, ErrCpaProxyBlocked)
	assert.Equal(t, int32(1), apiCalls.Load())
}

func TestStaggerDelaysWithinWindow(t *testing.T) {
	window := 7 * time.Second
	delays := staggerDelays(20, window)
	require.Len(t, delays, 20)
	for _, d := range delays {
		assert.GreaterOrEqual(t, d, time.Duration(0))
		assert.Less(t, d, window)
	}
}

func TestResolveClaudePlanType(t *testing.T) {
	assert.Equal(t, "plan_max20", resolveClaudePlanType(map[string]any{
		"organization": map[string]any{"rate_limit_tier": "default_claude_max_20x"},
	}))
	assert.Equal(t, "plan_team", resolveClaudePlanType(map[string]any{
		"organization": map[string]any{"organization_type": "claude_team", "subscription_status": "active"},
	}))
	assert.Equal(t, "plan_free", resolveClaudePlanType(map[string]any{
		"account": map[string]any{"has_claude_max": false, "has_claude_pro": false},
	}))
}

func TestSelectCpaGrant(t *testing.T) {
	now := time.Now()
	block := map[string]any{
		"eligible": true, "at_limit": true, "next_grant_id": "b",
		"grants": []any{
			map[string]any{"id": "a", "resets_left": 1.0, "usable_now": true},
			map[string]any{"id": "b", "resets_left": 2.0, "usable_now": true},
			map[string]any{"id": "c", "resets_left": 0.0, "usable_now": true},
		},
	}
	id, reason := selectCpaGrant(block, now)
	assert.Equal(t, "b", id)
	assert.Empty(t, reason)

	block["at_limit"] = false
	id, reason = selectCpaGrant(block, now)
	assert.Empty(t, id)
	assert.Equal(t, "not_limited", reason)

	block["eligible"] = false
	_, reason = selectCpaGrant(block, now)
	assert.Equal(t, "ineligible", reason)
}
