package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCpaCycleDB(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	t.Cleanup(func() { model.DB = previousDB })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.CpaInstance{}, &model.CpaAccountCache{}, &model.CpaUsageHourly{},
		&model.CpaUsageFailHourly{}, &model.CpaQuotaCycle{}, &model.CpaQuotaPoint{}))
	model.DB = db
	cpaCycleSeen.Range(func(k, _ any) bool { cpaCycleSeen.Delete(k); return true })
}

func TestAggregateCpaFailures(t *testing.T) {
	base := time.Unix(1_800_000_000, 0).UTC()
	var records []cpaUsageQueueRecord
	add := func(at time.Time, failed bool, status int, code string) {
		rec := cpaUsageQueueRecord{Timestamp: at, AuthIndex: "a1", Failed: failed}
		rec.Fail.StatusCode, rec.Fail.Code = status, code
		records = append(records, rec)
	}
	add(base, false, 200, "")
	add(base, true, 429, "rate_limit_error")
	add(base.Add(time.Minute), true, 429, "overloaded")
	add(base, true, 502, "")
	rows := aggregateCpaFailures(3, records)
	require.Len(t, rows, 2)
	bucket := base.Unix() - base.Unix()%3600
	r429 := rows[cpaFailKey{authIndex: "a1", bucket: bucket, status: 429}]
	require.NotNil(t, r429)
	assert.EqualValues(t, 2, r429.Count)
	assert.Equal(t, "overloaded", r429.LastCode)
	assert.Equal(t, base.Add(time.Minute).Unix(), r429.LastAt)
}

func TestCpaFailParsedFromQueueJSON(t *testing.T) {
	raw := `[{"timestamp":"2026-11-11T01:00:00Z","auth_index":"x","failed":true,"fail":{"status_code":401,"body":"no","code":"authentication_error"}}]`
	var records []cpaUsageQueueRecord
	require.NoError(t, json.Unmarshal([]byte(raw), &records))
	assert.Equal(t, 401, records[0].Fail.StatusCode)
	assert.Equal(t, "authentication_error", records[0].Fail.Code)
}

func TestCardUsageSevenDayAndReasons(t *testing.T) {
	setupCpaCycleDB(t)
	now := time.Now().Unix()
	hour := now - now%3600
	require.NoError(t, model.AddCpaUsageHourly(&model.CpaUsageHourly{InstanceID: 1, AuthIndex: "a", ModelName: "m", BucketTs: hour, Requests: 10, Failed: 3}))
	require.NoError(t, model.AddCpaUsageHourly(&model.CpaUsageHourly{InstanceID: 1, AuthIndex: "a", ModelName: "m", BucketTs: hour - 10*86400, Requests: 5, Failed: 5}))
	require.NoError(t, model.AddCpaUsageFailHourly(&model.CpaUsageFailHourly{InstanceID: 1, AuthIndex: "a", BucketTs: hour, StatusCode: 429, Count: 2, LastCode: "rate_limit_error", LastAt: now - 10}))
	require.NoError(t, model.AddCpaUsageFailHourly(&model.CpaUsageFailHourly{InstanceID: 1, AuthIndex: "a", BucketTs: hour, StatusCode: 502, Count: 1, LastAt: now - 5}))
	require.NoError(t, model.AddCpaUsageFailHourly(&model.CpaUsageFailHourly{InstanceID: 1, AuthIndex: "a", BucketTs: hour, StatusCode: 429, Count: 1, LastCode: "x", LastAt: now - 20}))

	usage, err := ListCpaCardUsage()
	require.NoError(t, err)
	got := usage["1/a"]
	assert.EqualValues(t, 15, got.Requests)
	assert.EqualValues(t, 10, got.Requests7d)
	assert.EqualValues(t, 3, got.Failed7d)
	require.Len(t, got.FailReasons7d, 2)
	assert.Equal(t, CpaFailReason{StatusCode: 429, Count: 3}, got.FailReasons7d[0])
	require.NotNil(t, got.LastFail)
	assert.Equal(t, 502, got.LastFail.StatusCode)
}

func usageWith(pct float64, reset int64) map[string]any {
	return map[string]any{"seven_day": map[string]any{"utilization": pct, "resets_at": time.Unix(reset, 0).UTC().Format(time.RFC3339)}}
}

func TestCpaCycleLifecycle(t *testing.T) {
	setupCpaCycleDB(t)
	day := int64(86400)
	end1 := int64(1_800_000_000)
	start1 := end1 - 7*day
	cred := map[string]any{"auth_index": "a"}
	snap := &CpaAccountSnapshot{InstanceID: 1, Name: "a.json", IsClaude: true, Credential: cred, PlanType: "plan_max20"}

	// Tokens: 1000 in cycle 1 (input+output+cache write), cache read separate.
	require.NoError(t, model.AddCpaUsageHourly(&model.CpaUsageHourly{InstanceID: 1, AuthIndex: "a", ModelName: "claude-opus-5", BucketTs: start1 + day,
		Requests: 1, InputTokens: 100, OutputTokens: 400, CacheCreationTokens: 500, CacheReadTokens: 9000}))

	snap.Usage, snap.QuotaObservedAt = usageWith(30, end1), start1+2*day
	observeCpaCycles(snap)
	snap.Usage, snap.QuotaObservedAt = usageWith(50, end1+60), start1+3*day // small drift: same cycle
	observeCpaCycles(snap)

	cycle, err := model.GetLatestCpaQuotaCycle(1, "a.json", "seven_day")
	require.NoError(t, err)
	require.NotNil(t, cycle)
	assert.Equal(t, start1, cycle.StartTs)
	assert.Empty(t, cycle.EndReason)
	assert.InDelta(t, 50, *cycle.LastPercent, 0.01)

	// Natural roll-over to cycle 2.
	end2 := end1 + 7*day
	snap.Usage, snap.QuotaObservedAt = usageWith(10, end2), end1+day
	observeCpaCycles(snap)
	// Reset grant used two days into cycle 2: window jumps early.
	end3 := end1 + 2*day + 7*day
	snap.Usage, snap.QuotaObservedAt = usageWith(1, end3), end1+2*day+60
	observeCpaCycles(snap)

	report, err := BuildCpaCycleReport()
	require.NoError(t, err)
	require.Len(t, report.Accounts, 1)
	require.Len(t, report.Accounts[0].Windows, 1)
	rows := report.Accounts[0].Windows[0].Cycles
	require.Len(t, rows, 3)
	assert.True(t, rows[0].Open)
	assert.Equal(t, "manual_reset", rows[1].EndReason)
	assert.Equal(t, end1+2*day, rows[1].EndTs)
	assert.Nil(t, rows[1].EstimatedFull, "10% used is below the 20% floor")
	assert.Equal(t, "natural", rows[2].EndReason)
	assert.EqualValues(t, 1000, rows[2].Tokens)
	assert.EqualValues(t, 9000, rows[2].CacheReadTokens)
	require.NotNil(t, rows[2].EstimatedFull)
	assert.EqualValues(t, 2000, *rows[2].EstimatedFull)
}

func TestCpaCycleHitLimitAndPlanChange(t *testing.T) {
	setupCpaCycleDB(t)
	day := int64(86400)
	end1 := int64(1_800_000_000)
	start1 := end1 - 7*day
	snap := &CpaAccountSnapshot{InstanceID: 1, Name: "b.json", IsClaude: true, Credential: map[string]any{"auth_index": "b"}, PlanType: "plan_max5"}
	require.NoError(t, model.AddCpaUsageHourly(&model.CpaUsageHourly{InstanceID: 1, AuthIndex: "b", ModelName: "claude-sonnet-5", BucketTs: start1 + 3600, Requests: 1, InputTokens: 600}))
	require.NoError(t, model.AddCpaUsageHourly(&model.CpaUsageHourly{InstanceID: 1, AuthIndex: "b", ModelName: "claude-sonnet-5", BucketTs: end1 + 3600, Requests: 1, InputTokens: 300}))
	require.NoError(t, model.AddCpaUsageHourly(&model.CpaUsageHourly{InstanceID: 1, AuthIndex: "b", ModelName: "claude-sonnet-5", BucketTs: end1 + 7*day + 3600, Requests: 1, InputTokens: 500}))

	// Chat says rejected at 60% (stale %): counts as 100% (429 wins).
	snap.Credential = map[string]any{"auth_index": "b", "quota": map[string]any{
		"observed_at": time.Unix(start1+day, 0).UTC().Format(time.RFC3339),
		"signals": map[string]any{
			"Anthropic-Ratelimit-Unified-7d-Utilization": "0.6",
			"Anthropic-Ratelimit-Unified-7d-Reset":       "1800000000",
			"Anthropic-Ratelimit-Unified-7d-Status":      "rejected",
		},
	}}
	observeCpaCycles(snap)
	// Next cycle, same plan, 30% at 300 tokens -> 1000 full.
	snap.Credential = map[string]any{"auth_index": "b"}
	snap.Usage, snap.QuotaObservedAt = usageWith(30, end1+7*day), end1+2*day
	observeCpaCycles(snap)
	// Plan upgraded within that cycle -> closes with plan_change.
	snap.PlanType = "plan_max20"
	snap.Usage, snap.QuotaObservedAt = usageWith(31, end1+7*day), end1+3*day
	observeCpaCycles(snap)
	closeExpiredCpaQuotaCycles(end1 + 30*day)

	report, err := BuildCpaCycleReport()
	require.NoError(t, err)
	rows := report.Accounts[0].Windows[0].Cycles
	require.Len(t, rows, 3)
	assert.Equal(t, "natural", rows[0].EndReason)
	assert.Equal(t, "plan_max20", rows[0].PlanType)
	assert.Nil(t, rows[0].ChangePercent, "never compared across a plan change")
	assert.Equal(t, "plan_change", rows[1].EndReason)
	require.NotNil(t, rows[1].EstimatedFull)
	assert.EqualValues(t, 1000, *rows[1].EstimatedFull)
	require.NotNil(t, rows[1].ChangePercent)
	assert.InDelta(t, 66.67, *rows[1].ChangePercent, 0.1) // 1000 vs 600
	assert.True(t, rows[2].HitLimit)
	assert.InDelta(t, 100, *rows[2].UsedPercent, 0.01)
	assert.EqualValues(t, 600, *rows[2].EstimatedFull)
}

func TestCpaCycleModelMatchesAndFable(t *testing.T) {
	assert.True(t, cpaCycleModelMatches("seven_day", "anything"))
	assert.True(t, cpaCycleModelMatches("seven_day_opus", "claude-opus-5"))
	assert.False(t, cpaCycleModelMatches("seven_day_opus", "claude-sonnet-5"))
	assert.True(t, cpaCycleModelMatches("seven_day_fable", "claude-fable-5-1"))

	usage := map[string]any{
		"limits": []any{map[string]any{"kind": "weekly_scoped", "percent": 42.0, "resets_at": "2026-11-20T00:00:00Z",
			"is_active": true, "scope": map[string]any{"model": map[string]any{"display_name": "Fable"}}}},
		"iguana_necktie": map[string]any{"utilization": 5.0, "resets_at": "2026-11-20T00:00:00Z", "limit_dollars": 10.0},
	}
	obs := cpaCycleObservationsFromUsage(usage, 100)
	require.Len(t, obs, 1)
	assert.Equal(t, "seven_day_fable", obs[0].Window)
	assert.InDelta(t, 42, obs[0].Percent, 0.01)
}

func TestBackfillPreviousCycle(t *testing.T) {
	setupCpaCycleDB(t)
	day := int64(86400)
	end1 := int64(1_800_000_000)
	start1 := end1 - 7*day
	snap := CpaAccountSnapshot{InstanceID: 1, Name: "c.json", IsClaude: true, Credential: map[string]any{"auth_index": "c"},
		Usage: usageWith(10, end1+7*day), QuotaObservedAt: end1 + day}
	data, err := json.Marshal(snap)
	require.NoError(t, err)
	require.NoError(t, model.UpsertCpaAccountCache(1, "c.json", string(data)))
	pct := 80.0
	require.NoError(t, model.AddCpaQuotaPoint(&model.CpaQuotaPoint{InstanceID: 1, Name: "c.json", Ts: end1 - day, SevenDay: &pct}))
	require.NoError(t, model.AddCpaUsageHourly(&model.CpaUsageHourly{InstanceID: 1, AuthIndex: "c", ModelName: "m", BucketTs: start1 + day, Requests: 1, OutputTokens: 800}))

	backfillCpaQuotaCycles()
	backfillCpaQuotaCycles() // idempotent

	report, err := BuildCpaCycleReport()
	require.NoError(t, err)
	rows := report.Accounts[0].Windows[0].Cycles
	require.Len(t, rows, 2)
	assert.True(t, rows[1].Backfilled)
	assert.EqualValues(t, 1000, *rows[1].EstimatedFull)
}
