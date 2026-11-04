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

	_, err = DecryptCpaSecret("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未保存管理密钥")
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

func TestIsCpaProxyFailure(t *testing.T) {
	assert.False(t, isCpaProxyFailure(nil))
	assert.False(t, isCpaProxyFailure(ErrCpaProxyBlocked), "no proxy configured is blocked, not failed")
	assert.True(t, isCpaProxyFailure(&cpaHTTPError{Status: http.StatusBadGateway, Message: "request failed"}))
	assert.True(t, isCpaProxyFailure(&cpaHTTPError{Status: http.StatusBadRequest, Message: "invalid proxy_url"}))
	assert.False(t, isCpaProxyFailure(&cpaHTTPError{Status: http.StatusUnauthorized}))
	assert.False(t, isCpaProxyFailure(context.DeadlineExceeded))
}

func TestTrackCpaProxyHealthStreak(t *testing.T) {
	disableCalls := 0
	origDisable := cpaAutoDisableAccount
	cpaAutoDisableAccount = func(int, string, int, string) { disableCalls++ }
	t.Cleanup(func() { cpaAutoDisableAccount = origDisable })

	badGateway := &cpaHTTPError{Status: http.StatusBadGateway, Message: "request failed"}
	snap := &CpaAccountSnapshot{InstanceID: 1, Name: "a", Credential: map[string]any{}}
	// A missing proxy never counts, however often it is seen.
	trackCpaProxyHealth(snap, ErrCpaProxyBlocked)
	trackCpaProxyHealth(snap, ErrCpaProxyBlocked)
	assert.Equal(t, 0, snap.ProxyFailStreak)
	assert.Zero(t, snap.AutoDisabledAt)

	// Errors unrelated to the proxy never count.
	trackCpaProxyHealth(snap, &cpaHTTPError{Status: http.StatusUnauthorized})
	assert.Equal(t, 0, snap.ProxyFailStreak)
	assert.Zero(t, snap.AutoDisabledAt)

	// The very first SOCKS failure disables the account.
	trackCpaProxyHealth(snap, badGateway)
	assert.Equal(t, 1, snap.ProxyFailStreak)
	assert.NotZero(t, snap.AutoDisabledAt, "first SOCKS failure must disable")
	assert.Equal(t, 1, disableCalls)

	// Still not disabled in CPA (e.g. the previous disable call failed):
	// every further SOCKS failure retries the disable.
	trackCpaProxyHealth(snap, badGateway)
	assert.Equal(t, 2, disableCalls)

	trackCpaProxyHealth(snap, nil)
	assert.Equal(t, 0, snap.ProxyFailStreak)
	assert.Zero(t, snap.AutoDisabledAt)

	// Already disabled accounts are counted but never disabled again.
	disabled := &CpaAccountSnapshot{Credential: map[string]any{"disabled": true}, ProxyFailStreak: 5}
	trackCpaProxyHealth(disabled, badGateway)
	assert.Equal(t, 6, disabled.ProxyFailStreak)
	assert.Zero(t, disabled.AutoDisabledAt)
	assert.Equal(t, 2, disableCalls)
}

func TestAggregateCpaUsage(t *testing.T) {
	oneHour := int64(300)
	base := time.Unix(1_800_000_000, 0).UTC()
	records := []cpaUsageQueueRecord{
		{Timestamp: base, AuthIndex: "abc", Model: "no-such-model-xyz", LatencyMs: 100},
		{Timestamp: base.Add(time.Minute), AuthIndex: "abc", Model: "no-such-model-xyz", Failed: true, LatencyMs: 300},
		{Timestamp: base, AuthIndex: "", Model: "ignored"},
		{Timestamp: base.Add(2 * time.Hour), AuthIndex: "def", Alias: "alias-only"},
	}
	records[0].Tokens.InputTokens = 10
	records[0].Tokens.OutputTokens = 20
	records[0].Tokens.CacheReadTokens = 30
	records[0].Tokens.CacheCreationTokens = 400
	records[0].Tokens.CacheCreation1hTokens = &oneHour
	records[1].Tokens.InputTokens = -5 // negative values are clamped

	rows, latest := aggregateCpaUsage(7, records)
	require.Len(t, rows, 2)
	bucket := base.Unix() - base.Unix()%3600
	row := rows[cpaUsageKey{authIndex: "abc", model: "no-such-model-xyz", bucket: bucket}]
	require.NotNil(t, row)
	assert.Equal(t, 7, row.InstanceID)
	assert.EqualValues(t, 2, row.Requests)
	assert.EqualValues(t, 1, row.Failed)
	assert.EqualValues(t, 10, row.InputTokens)
	assert.EqualValues(t, 400, row.CacheCreationTokens)
	assert.EqualValues(t, 300, row.CacheCreation1hTokens)
	assert.EqualValues(t, 460, row.TotalTokens())
	assert.EqualValues(t, 400, row.LatencySumMs)
	assert.Equal(t, base.Add(2*time.Hour).Unix(), latest["def"])
	assert.Equal(t, base.Add(time.Minute).Unix(), latest["abc"])

	aliasBucket := base.Add(2*time.Hour).Unix() - base.Add(2*time.Hour).Unix()%3600
	assert.NotNil(t, rows[cpaUsageKey{authIndex: "def", model: "alias-only", bucket: aliasBucket}])
}

func TestCpaCountsCapsOneHourWrites(t *testing.T) {
	big := int64(999)
	rec := cpaUsageQueueRecord{}
	rec.Tokens.CacheCreationTokens = 100
	rec.Tokens.CacheCreation1hTokens = &big
	assert.EqualValues(t, 100, rec.counts().CacheCreation1h)
}

func TestEstimateCpaCostUSDUnknownModel(t *testing.T) {
	_, ok := estimateCpaCostUSD("", cpaTokenCounts{Input: 1})
	assert.False(t, ok)
}

func TestUsageWindowPercent(t *testing.T) {
	usage := map[string]any{
		"five_hour": map[string]any{"utilization": 42.5},
		"seven_day": map[string]any{"utilization": "7"},
		"bad":       "x",
	}
	require.NotNil(t, usageWindowPercent(usage, "five_hour"))
	assert.InDelta(t, 42.5, *usageWindowPercent(usage, "five_hour"), 1e-9)
	assert.InDelta(t, 7, *usageWindowPercent(usage, "seven_day"), 1e-9)
	assert.Nil(t, usageWindowPercent(usage, "bad"))
	assert.Nil(t, usageWindowPercent(nil, "five_hour"))
}

func TestParseChannelIDsAndHost(t *testing.T) {
	assert.Equal(t, []int{3, 5}, parseChannelIDs(" 3, x,5,-1,"))
	assert.Equal(t, "10.0.19.81:8317", hostOf("http://10.0.19.81:8317/v1"))
	assert.Equal(t, "", hostOf("::bad"))
}
