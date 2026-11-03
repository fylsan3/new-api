package service

import (
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
