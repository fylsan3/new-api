package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
)

// Usage collection: for CPAs with UsageCollectEnabled, New API drains the
// CPA usage queue (GET /observability/usage/queue, destructive) and keeps an
// hourly aggregate per account. Costs are estimates at New API base prices.

const (
	cpaUsagePollInterval   = 5 * time.Second // user decision 2026-11-06
	cpaUsagePageSize       = 500
	cpaUsageMaxPages       = 20
	cpaUsageRemoteCheckTTL = 5 * time.Minute
	cpaUsageRetention      = 90 * 24 * time.Hour
	cpaQuotaRetention      = 14 * 24 * time.Hour
	cpaQuotaPointMinGap    = 10 * 60 // seconds
	cpaPruneInterval       = 6 * time.Hour

	// cpaProxyFailDisableThreshold: consecutive SOCKS failures before the
	// account is disabled in CPA. User decision (2026-11-04): disable on the
	// very first SOCKS failure, account safety outranks availability.
	cpaProxyFailDisableThreshold = 1

	CpaAlertKindSocksDisabled   = "socks_auto_disabled"
	CpaAlertKindSocksDisableErr = "socks_auto_disable_failed"
	CpaAlertKindUsageStatsOff   = "usage_statistics_off"
)

// cpaUsageQueueRecord is the subset of the CPA queue payload New API reads.
type cpaUsageQueueRecord struct {
	Timestamp time.Time `json:"timestamp"`
	LatencyMs int64     `json:"latency_ms"`
	AuthIndex string    `json:"auth_index"`
	Failed    bool      `json:"failed"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	Alias     string    `json:"alias"`
	// Fail is set by CPA on failed requests (status_code defaults to 500
	// when CPA cannot tell the upstream status).
	Fail struct {
		StatusCode int    `json:"status_code"`
		Code       string `json:"code"`
	} `json:"fail"`
	Tokens struct {
		InputTokens           int64  `json:"input_tokens"`
		OutputTokens          int64  `json:"output_tokens"`
		CacheReadTokens       int64  `json:"cache_read_tokens"`
		CacheCreationTokens   int64  `json:"cache_creation_tokens"`
		CacheCreation5mTokens *int64 `json:"cache_creation_5m_tokens"`
		CacheCreation1hTokens *int64 `json:"cache_creation_1h_tokens"`
	} `json:"tokens"`
}

// cpaTokenCounts are Claude-semantic counts: input, cache read and cache
// creation are independent of each other.
type cpaTokenCounts struct {
	Input, Output, CacheRead, CacheCreation, CacheCreation1h int64
}

func (r *cpaUsageQueueRecord) counts() cpaTokenCounts {
	t := r.Tokens
	c := cpaTokenCounts{
		Input: max(t.InputTokens, 0), Output: max(t.OutputTokens, 0),
		CacheRead: max(t.CacheReadTokens, 0), CacheCreation: max(t.CacheCreationTokens, 0),
	}
	if t.CacheCreation1hTokens != nil && *t.CacheCreation1hTokens > 0 {
		c.CacheCreation1h = min(*t.CacheCreation1hTokens, c.CacheCreation)
	}
	return c
}

// estimateCpaCostUSD prices one request with New API's configured base price
// (no group ratio). ok=false means the model has no price configured.
func estimateCpaCostUSD(modelName string, c cpaTokenCounts) (float64, bool) {
	if modelName == "" {
		return 0, false
	}
	if billing_setting.GetBillingMode(modelName) == billing_setting.BillingModeTieredExpr {
		expr, ok := billing_setting.GetBillingExpr(modelName)
		if !ok {
			return 0, false
		}
		cc5m := c.CacheCreation - c.CacheCreation1h
		usage := &dto.Usage{
			PromptTokens:                int(c.Input),
			CompletionTokens:            int(c.Output),
			UsageSemantic:               "anthropic",
			ClaudeCacheCreation5mTokens: int(cc5m),
			ClaudeCacheCreation1hTokens: int(c.CacheCreation1h),
		}
		usage.PromptTokensDetails.CachedTokens = int(c.CacheRead)
		usage.PromptTokensDetails.CachedCreationTokens = int(c.CacheCreation)
		params := BuildTieredTokenParams(usage, true, billingexpr.UsedVars(expr))
		cost, _, err := billingexpr.RunExpr(expr, params)
		if err != nil {
			return 0, false
		}
		// v1 expressions use $/1M-token coefficients (fixed() is pre-scaled).
		return cost / 1_000_000, true
	}
	if price, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		return price, true
	}
	modelRatio, ok, _ := ratio_setting.GetModelRatio(modelName)
	if !ok {
		return 0, false
	}
	cacheRatio, _ := ratio_setting.GetCacheRatio(modelName)
	createRatio, _ := ratio_setting.GetCreateCacheRatio(modelName)
	completionRatio := ratio_setting.GetCompletionRatio(modelName)
	cc5m := float64(c.CacheCreation - c.CacheCreation1h)
	weighted := float64(c.Input) +
		float64(c.CacheRead)*cacheRatio +
		cc5m*createRatio +
		float64(c.CacheCreation1h)*createRatio*hosttypes.ClaudeCacheCreation1hMultiplier +
		float64(c.Output)*completionRatio
	return weighted * modelRatio / common.QuotaPerUnit, true
}

type cpaUsageKey struct {
	authIndex string
	model     string
	bucket    int64
}

type cpaFailKey struct {
	authIndex string
	bucket    int64
	status    int
}

// aggregateCpaFailures folds failed queue records into hourly counts per
// upstream status code (design draft 2026-11-11, "failure reasons").
func aggregateCpaFailures(instanceID int, records []cpaUsageQueueRecord) map[cpaFailKey]*model.CpaUsageFailHourly {
	rows := make(map[cpaFailKey]*model.CpaUsageFailHourly)
	for i := range records {
		rec := &records[i]
		authIndex := strings.TrimSpace(rec.AuthIndex)
		if !rec.Failed || authIndex == "" {
			continue
		}
		ts := rec.Timestamp.Unix()
		if rec.Timestamp.IsZero() || ts <= 0 {
			ts = common.GetTimestamp()
		}
		status := rec.Fail.StatusCode
		if status < 0 || status > 999 {
			status = 0
		}
		key := cpaFailKey{authIndex: authIndex, bucket: ts - ts%3600, status: status}
		row := rows[key]
		if row == nil {
			row = &model.CpaUsageFailHourly{InstanceID: instanceID, AuthIndex: authIndex, BucketTs: key.bucket, StatusCode: status}
			rows[key] = row
		}
		row.Count++
		if ts >= row.LastAt {
			row.LastAt = ts
			row.LastCode = truncateText(strings.TrimSpace(rec.Fail.Code), 64)
		}
	}
	return rows
}

// aggregateCpaUsage folds queue records into hourly rows plus the newest
// request time per auth_index. Non-Claude records are kept too (they still
// consume the account), but priced only if New API knows the model.
func aggregateCpaUsage(instanceID int, records []cpaUsageQueueRecord) (map[cpaUsageKey]*model.CpaUsageHourly, map[string]int64) {
	rows := make(map[cpaUsageKey]*model.CpaUsageHourly)
	latest := make(map[string]int64)
	for i := range records {
		rec := &records[i]
		authIndex := strings.TrimSpace(rec.AuthIndex)
		if authIndex == "" {
			continue
		}
		ts := rec.Timestamp.Unix()
		if rec.Timestamp.IsZero() || ts <= 0 {
			ts = common.GetTimestamp()
		}
		modelName := strings.TrimSpace(rec.Model)
		if modelName == "" || modelName == "unknown" {
			modelName = strings.TrimSpace(rec.Alias)
		}
		if modelName == "" {
			modelName = "unknown"
		}
		if len(modelName) > 128 {
			modelName = modelName[:128]
		}
		key := cpaUsageKey{authIndex: authIndex, model: modelName, bucket: ts - ts%3600}
		row := rows[key]
		if row == nil {
			row = &model.CpaUsageHourly{InstanceID: instanceID, AuthIndex: authIndex, ModelName: modelName, BucketTs: key.bucket}
			rows[key] = row
		}
		c := rec.counts()
		row.Requests++
		if rec.Failed {
			row.Failed++
		}
		row.InputTokens += c.Input
		row.OutputTokens += c.Output
		row.CacheReadTokens += c.CacheRead
		row.CacheCreationTokens += c.CacheCreation
		row.CacheCreation1hTokens += c.CacheCreation1h
		row.LatencySumMs += max(rec.LatencyMs, 0)
		if cost, ok := estimateCpaCostUSD(modelName, c); ok {
			row.CostUSD += cost
		} else {
			row.UnpricedRequests++
		}
		if ts > latest[authIndex] {
			latest[authIndex] = ts
		}
	}
	return rows, latest
}

func (c *cpaClient) popUsageQueue(ctx context.Context, count int) ([]cpaUsageQueueRecord, error) {
	var records []cpaUsageQueueRecord
	query := url.Values{"count": {strconv.Itoa(count)}}
	if err := c.do(ctx, http.MethodGet, "/observability/usage/queue", query, nil, &records); err != nil {
		return nil, err
	}
	return records, nil
}

// usageStatisticsEnabled reads CPA's switch (read-only, v0 route; v8 has no
// dedicated path). Returns 1 on, 2 off, 0 unknown.
func (c *cpaClient) usageStatisticsEnabled(ctx context.Context) int {
	var out map[string]any
	if err := c.doRaw(ctx, http.MethodGet, "/v0/management/usage-statistics-enabled", nil, nil, &out); err != nil {
		return 0
	}
	if on, ok := out["usage-statistics-enabled"].(bool); ok {
		if on {
			return 1
		}
		return 2
	}
	return 0
}

var (
	cpaUsageBusy        sync.Map // instanceID -> struct{}
	cpaUsageRemoteCheck sync.Map // instanceID -> time.Time
)

// CollectCpaUsage drains one CPA queue page by page and stores aggregates.
// It returns the number of records consumed.
func CollectCpaUsage(ctx context.Context, instance *model.CpaInstance) (int, error) {
	if !instance.UsageCollectEnabled {
		return 0, nil
	}
	if _, busy := cpaUsageBusy.LoadOrStore(instance.ID, struct{}{}); busy {
		return 0, nil
	}
	defer cpaUsageBusy.Delete(instance.ID)

	client, err := newCpaClient(instance)
	if err != nil {
		return 0, err
	}
	state := map[string]any{"usage_last_poll_at": common.GetTimestamp()}
	if last, ok := cpaUsageRemoteCheck.Load(instance.ID); !ok || time.Since(last.(time.Time)) > cpaUsageRemoteCheckTTL {
		remote := client.usageStatisticsEnabled(ctx)
		cpaUsageRemoteCheck.Store(instance.ID, time.Now())
		state["usage_remote_state"] = remote
		if remote == 2 && instance.UsageRemoteState != 2 {
			_ = model.RaiseCpaAlert(instance.ID, "", CpaAlertKindUsageStatsOff,
				"CPA usage-statistics-enabled is false, so no usage records are produced")
		}
	}

	total := 0
	var pollErr error
	for page := 0; page < cpaUsageMaxPages; page++ {
		records, popErr := client.popUsageQueue(ctx, cpaUsagePageSize)
		if popErr != nil {
			pollErr = popErr
			break
		}
		if len(records) == 0 {
			break
		}
		total += len(records)
		if storeErr := storeCpaUsage(instance.ID, records); storeErr != nil {
			// Records are already popped; log loudly, they cannot be re-read.
			common.SysError(fmt.Sprintf("cpa usage store failed for instance %d (%d records lost): %v", instance.ID, len(records), storeErr))
			pollErr = storeErr
			break
		}
		if len(records) < cpaUsagePageSize {
			break
		}
	}
	if total > 0 {
		state["usage_last_record_at"] = common.GetTimestamp()
	}
	state["usage_last_error"] = ""
	if pollErr != nil {
		state["usage_last_error"] = truncateText(pollErr.Error(), 500)
	}
	noteCpaCollectHealth(instance, pollErr)
	_ = model.UpdateCpaInstanceUsageState(instance.ID, state)
	return total, pollErr
}

func storeCpaUsage(instanceID int, records []cpaUsageQueueRecord) error {
	rows, latest := aggregateCpaUsage(instanceID, records)
	for _, row := range rows {
		if err := model.AddCpaUsageHourly(row); err != nil {
			return err
		}
	}
	for _, row := range aggregateCpaFailures(instanceID, records) {
		if err := model.AddCpaUsageFailHourly(row); err != nil {
			return err
		}
	}
	// Side feature (RPM / TPM): a failure here must not fail the hourly
	// collection above nor skip the last-request update below.
	var minuteErr error
	for _, row := range aggregateCpaMinutes(instanceID, records) {
		if err := model.AddCpaUsageMinute(row); err != nil {
			minuteErr = err
		}
	}
	if minuteErr != nil {
		common.SysError(fmt.Sprintf("cpa usage minute store failed for instance %d (RPM / TPM only): %v", instanceID, minuteErr))
	}
	handleCpaAuthFailures(instanceID, records)
	if len(latest) == 0 {
		return nil
	}
	snaps, err := ListCpaSnapshots()
	if err != nil {
		return nil
	}
	for i := range snaps {
		snap := &snaps[i]
		if snap.InstanceID != instanceID {
			continue
		}
		ts, ok := latest[credentialString(snap.Credential, "auth_index")]
		if !ok || ts <= snap.LastRequestAt {
			continue
		}
		unlock := lockAccount(instanceID, snap.Name)
		if fresh, loadErr := loadSnapshot(instanceID, snap.Name); loadErr == nil && ts > fresh.LastRequestAt {
			fresh.LastRequestAt = ts
			_ = saveSnapshot(fresh)
		}
		unlock()
	}
	return nil
}

var cpaUsageCollectorOnce sync.Once

// startCpaUsageCollector polls every opted-in CPA on the master node.
func startCpaUsageCollector() {
	cpaUsageCollectorOnce.Do(func() {
		gopool.Go(backfillCpaQuotaCycles)
		gopool.Go(func() {
			ticker := time.NewTicker(cpaUsagePollInterval)
			defer ticker.Stop()
			lastPrune := time.Time{}
			for range ticker.C {
				instances, err := model.ListCpaInstances()
				if err != nil {
					continue
				}
				for i := range instances {
					if !instances[i].UsageCollectEnabled {
						continue
					}
					instance := instances[i]
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
					if _, collectErr := CollectCpaUsage(ctx, &instance); collectErr != nil {
						common.SysLog(fmt.Sprintf("cpa usage collect %s failed: %v", instance.Name, collectErr))
					}
					cancel()
				}
				if time.Since(lastPrune) > cpaPruneInterval {
					lastPrune = time.Now()
					now := time.Now()
					_ = model.PruneCpaStats(now.Add(-cpaUsageRetention).Unix(), now.Add(-cpaQuotaRetention).Unix())
					_ = model.PruneCpaUsageFail(now.Add(-cpaUsageRetention).Unix())
					_ = model.PruneCpaUsageMinutes(now.Add(-cpaMinuteRetention).Unix())
					closeExpiredCpaQuotaCycles(now.Unix())
				}
			}
		})
	})
}

// isCpaProxyFailure tells whether an error means a configured SOCKS proxy is
// unreachable: CPA failing to reach Anthropic through it (api-call answers
// 502) or rejecting it. A missing/invalid proxy_url (ErrCpaProxyBlocked) is
// not a failure: New API never sends the request, the card shows it grey and
// the account must not be auto-disabled. CPA itself being unreachable does
// not count either, that says nothing about the proxy.
func isCpaProxyFailure(err error) bool {
	if err == nil || errors.Is(err, ErrCpaProxyBlocked) {
		return false
	}
	var httpErr *cpaHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Status == http.StatusBadGateway ||
			(httpErr.Status == http.StatusBadRequest && strings.Contains(httpErr.Message, "proxy"))
	}
	return false
}

// trackCpaProxyHealth updates the failure streak on the snapshot and, at the
// threshold, disables the account in CPA and raises an alert that stays until
// acknowledged. The caller holds the account lock; disabling runs async
// because it re-syncs the instance (which takes account locks).
func trackCpaProxyHealth(snap *CpaAccountSnapshot, proxyErr error) {
	if proxyErr == nil {
		snap.ProxyFailStreak = 0
		snap.AutoDisabledAt = 0
		return
	}
	if !isCpaProxyFailure(proxyErr) {
		// e.g. CPA unreachable or upstream 4xx: says nothing about the proxy.
		return
	}
	snap.ProxyFailStreak++
	if snap.RestUntil > 0 && snap.ProxyFailStreak >= cpaProxyFailDisableThreshold {
		// Resting account (already disabled by rotation): the SOCKS guard
		// takes over, the rotation mark goes so it is never re-enabled.
		snap.RestStartedAt, snap.RestUntil, snap.RestWorkedHours = 0, 0, 0
		snap.AutoDisabledAt = common.GetTimestamp()
		cpaAutoDisableAccount(snap.InstanceID, snap.Name, snap.ProxyFailStreak, proxyErr.Error())
		return
	}
	if snap.ProxyFailStreak < cpaProxyFailDisableThreshold || credentialBool(snap.Credential, "disabled") {
		return
	}
	// No in-flight guard on purpose: if a previous disable call failed, every
	// later SOCKS failure retries it. Disabling is idempotent and alerts are
	// de-duplicated by RaiseCpaAlert.
	instanceID, name, streak, reason := snap.InstanceID, snap.Name, snap.ProxyFailStreak, proxyErr.Error()
	snap.AutoDisabledAt = common.GetTimestamp()
	cpaAutoDisableAccount(instanceID, name, streak, reason)
}

// cpaAutoDisableAccount disables the account in CPA and raises an alert. It
// runs async; tests replace it to avoid touching CPA and the database.
var cpaAutoDisableAccount = func(instanceID int, name string, streak int, reason string) {
	gopool.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		msg := fmt.Sprintf("SOCKS failure (%s), streak %d; account disabled in CPA", truncateText(reason, 300), streak)
		kind := CpaAlertKindSocksDisabled
		if err := SetCpaAccountDisabled(ctx, instanceID, name, true); err != nil {
			kind = CpaAlertKindSocksDisableErr
			msg = fmt.Sprintf("SOCKS failure (%s), streak %d; auto-disable failed: %s",
				truncateText(reason, 200), streak, truncateText(err.Error(), 200))
		}
		if err := model.RaiseCpaAlert(instanceID, name, kind, msg); err != nil {
			common.SysError("cpa alert save failed: " + err.Error())
		}
		NotifyCpaEvent(CpaNotifySocks, instanceID, name, msg)
		common.SysLog(fmt.Sprintf("cpa subscription %s: %s", name, msg))
	})
}

func usageWindowPercent(usage map[string]any, key string) *float64 {
	window, ok := usage[key].(map[string]any)
	if !ok {
		return nil
	}
	switch v := window["utilization"].(type) {
	case float64:
		return &v
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return &f
		}
	}
	return nil
}

// recordCpaQuotaPoint stores one utilization sample, at most every 10 minutes
// per account, for the dashboard trend chart.
func recordCpaQuotaPoint(snap *CpaAccountSnapshot) {
	if snap.Usage == nil {
		return
	}
	five, seven := usageWindowPercent(snap.Usage, "five_hour"), usageWindowPercent(snap.Usage, "seven_day")
	if five == nil && seven == nil {
		return
	}
	now := common.GetTimestamp()
	if last, err := model.LastCpaQuotaPointTs(snap.InstanceID, snap.Name); err == nil && now-last < cpaQuotaPointMinGap {
		return
	}
	_ = model.AddCpaQuotaPoint(&model.CpaQuotaPoint{InstanceID: snap.InstanceID, Name: snap.Name, Ts: now, FiveHour: five, SevenDay: seven})
}
