package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Plans hold 1 or 2 moments inside the rest of today, 2 at least 4h apart,
// and both counts show up (50/50).
func TestDrawCpaDailyPlan(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 11, 6, 0, 5, 0, 0, loc)
	end := time.Date(2026, 11, 7, 0, 0, 0, 0, loc).Unix()
	seen := map[int]int{}
	for range 400 {
		plan := drawCpaDailyPlan(now)
		require.True(t, len(plan) == 1 || len(plan) == 2, "plan %v", plan)
		seen[len(plan)]++
		for _, ts := range plan {
			assert.GreaterOrEqual(t, ts, now.Unix())
			assert.Less(t, ts, end)
		}
		if len(plan) == 2 {
			assert.GreaterOrEqual(t, plan[1]-plan[0], int64(cpaDailyRefreshMinGap/time.Second))
		}
	}
	assert.Positive(t, seen[1])
	assert.Positive(t, seen[2])

	// Less than the gap left today: only one slot.
	late := time.Date(2026, 11, 6, 21, 0, 0, 0, loc)
	for range 50 {
		assert.Len(t, drawCpaDailyPlan(late), 1)
	}
	assert.Empty(t, drawCpaDailyPlan(time.Date(2026, 11, 6, 23, 59, 30, 0, loc)))
}

// A new day draws a new plan; a slot missed by more than the grace period is
// dropped (no make-up); a due slot is reported; disabled accounts never run.
func TestPlanCpaDailyRefresh(t *testing.T) {
	instance := setupCpaCredentialSyncDB(t, "http://127.0.0.1:1")
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.Local)
	snap := &CpaAccountSnapshot{
		InstanceID: instance.ID, Name: "a.json", IsClaude: true,
		Credential:      map[string]any{"name": "a.json"},
		AutoRefreshDay:  "20261106",
		AutoRefreshPlan: []int64{now.Add(-time.Hour).Unix(), now.Add(-time.Minute).Unix()},
	}
	require.NoError(t, saveSnapshot(snap))

	assert.True(t, planCpaDailyRefresh(instance.ID, "a.json", now))
	got, err := loadSnapshot(instance.ID, "a.json")
	require.NoError(t, err)
	assert.Equal(t, 1, got.AutoRefreshDone, "the slot missed by 1h is skipped")

	// Next day: fresh plan, nothing done yet.
	tomorrow := time.Date(2026, 11, 7, 0, 0, 30, 0, time.Local)
	planCpaDailyRefresh(instance.ID, "a.json", tomorrow)
	got, err = loadSnapshot(instance.ID, "a.json")
	require.NoError(t, err)
	assert.Equal(t, "20261107", got.AutoRefreshDay)
	assert.Equal(t, 0, got.AutoRefreshDone)
	assert.NotEmpty(t, got.AutoRefreshPlan)

	// Disabled: never due.
	got.Credential["disabled"] = true
	got.AutoRefreshPlan = []int64{tomorrow.Unix()}
	require.NoError(t, saveSnapshot(got))
	assert.False(t, planCpaDailyRefresh(instance.ID, "a.json", tomorrow))
}

// The daily refresh sends only profile + cedar_ember (never the quota usage
// query) and sends nothing without a valid SOCKS proxy.
func TestRefreshCpaPlanAndGrants(t *testing.T) {
	var mu sync.Mutex
	proxy := ""
	var targets []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v8/management/credentials/download":
			_, _ = w.Write([]byte(`{"proxy_url":"` + proxy + `"}`))
		case "/v8/management/requests/api-call":
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			_ = common.Unmarshal(raw, &body)
			target, _ := body["url"].(string)
			mu.Lock()
			targets = append(targets, target)
			mu.Unlock()
			if strings.Contains(target, "/profile") {
				_, _ = w.Write([]byte(`{"status_code":200,"body":"{\"organization\":{\"rate_limit_tier\":\"default_claude_max_20x\"}}"}`))
				return
			}
			_, _ = w.Write([]byte(`{"status_code":200,"body":"{\"cedar_ember\":{\"eligible\":true,\"grants\":[]}}"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	instance := setupCpaCredentialSyncDB(t, server.URL)
	snap := &CpaAccountSnapshot{
		InstanceID: instance.ID, Name: "a.json", IsClaude: true,
		Credential:      map[string]any{"name": "a.json", "auth_index": "1"},
		Usage:           map[string]any{"five_hour": map[string]any{"utilization": 10.0}},
		AutoRefreshPlan: []int64{1}, AutoRefreshDay: "x",
	}
	require.NoError(t, saveSnapshot(snap))
	ctx := context.Background()

	// No proxy_url: skipped, nothing sent, slot still counted.
	require.NoError(t, RefreshCpaPlanAndGrants(ctx, instance.ID, "a.json"))
	assert.Empty(t, targets)
	got, err := loadSnapshot(instance.ID, "a.json")
	require.NoError(t, err)
	assert.Equal(t, 1, got.AutoRefreshDone)
	assert.NotEmpty(t, got.AutoRefreshError)

	// Valid SOCKS: profile + cedar_ember only.
	proxy = "socks5h://u:p@1.2.3.4:1080"
	require.NoError(t, RefreshCpaPlanAndGrants(ctx, instance.ID, "a.json"))
	require.Len(t, targets, 2)
	for _, target := range targets {
		assert.NotEqual(t, claudeUsageURL, target, "quota usage must not be queried")
	}
	got, err = loadSnapshot(instance.ID, "a.json")
	require.NoError(t, err)
	assert.Equal(t, "plan_max20", got.PlanType)
	assert.Equal(t, true, got.ResetGrants["eligible"])
	assert.Empty(t, got.AutoRefreshError)
	assert.EqualValues(t, 10.0, got.Usage["five_hour"].(map[string]any)["utilization"], "quota is left untouched")
}
