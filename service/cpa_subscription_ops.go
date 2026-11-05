package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

var (
	cpaAccountLocks   sync.Map // key -> *sync.Mutex
	cpaRefreshAllBusy atomic.Bool
	cpaSchedulerOnce  sync.Once
	cpaPendingClaims  sync.Map // key -> pendingClaim

	cpaGrantIDRe = regexp.MustCompile(`^[a-z0-9_-]{1,40}$`)
	cpaOrgUUIDRe = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

const (
	cpaRefreshAllMinSpread = 5 * time.Second
	cpaRefreshAllMaxSpread = 10 * time.Second
	cpaAutoMaxJitter       = 10 * time.Minute
	cpaClaimRetryWindow    = 10 * time.Minute
	cpaClaimTimeout        = 25 * time.Second
)

var ErrCpaAccountNotFound = errors.New("account not found in CPA")

func accountKey(instanceID int, name string) string {
	return fmt.Sprintf("%d/%s", instanceID, name)
}

func lockAccount(instanceID int, name string) func() {
	v, _ := cpaAccountLocks.LoadOrStore(accountKey(instanceID, name), &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// SyncCpaInstance pulls the credential list of one CPA and updates the cache.
// It returns the credentials keyed by name.
func SyncCpaInstance(ctx context.Context, instance *model.CpaInstance) (map[string]map[string]any, error) {
	client, err := newCpaClient(instance)
	if err != nil {
		return nil, err
	}
	files, err := client.listCredentials(ctx)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]map[string]any, len(files))
	names := make([]string, 0, len(files))
	for _, raw := range files {
		cred := filterCredential(raw)
		name := credentialString(cred, "name")
		if name == "" {
			continue
		}
		byName[name] = cred
		names = append(names, name)
		unlock := lockAccount(instance.ID, name)
		snap, loadErr := loadSnapshot(instance.ID, name)
		if loadErr == nil {
			snap.Credential = cred
			snap.CredentialSyncedAt = common.GetTimestamp()
			snap.IsClaude = isClaudeCredential(cred)
			_ = saveSnapshot(snap)
		}
		unlock()
	}
	if err := model.PruneCpaAccountCaches(instance.ID, names); err != nil {
		return byName, err
	}
	return byName, nil
}

// SyncAllCpaInstances refreshes the credential list of every CPA (one request each).
func SyncAllCpaInstances(ctx context.Context) map[int]error {
	instances, err := model.ListCpaInstances()
	errs := map[int]error{}
	if err != nil {
		errs[0] = err
		return errs
	}
	for i := range instances {
		if _, syncErr := SyncCpaInstance(ctx, &instances[i]); syncErr != nil {
			errs[instances[i].ID] = syncErr
		}
	}
	return errs
}

// RefreshCpaAccount refreshes proxy, usage, plan and reset grants of one account.
func RefreshCpaAccount(ctx context.Context, instanceID int, name string) (*CpaAccountSnapshot, error) {
	instance, err := model.GetCpaInstance(instanceID)
	if err != nil {
		return nil, err
	}
	client, err := newCpaClient(instance)
	if err != nil {
		return nil, err
	}
	unlock := lockAccount(instanceID, name)
	defer unlock()

	snap, err := loadSnapshot(instanceID, name)
	if err != nil {
		return nil, err
	}
	if snap.Credential == nil {
		return nil, ErrCpaAccountNotFound
	}
	authIndex := credentialString(snap.Credential, "auth_index")

	guard, proxyErr := client.requireSocksProxy(ctx, name)
	if guard != nil {
		snap.ProxyURL = MaskProxyURL(guard.proxyURL)
	}
	// No proxy_url at all is not a proxy error: the user leaves test accounts
	// like this on purpose (see directGuard below).
	noProxy := proxyErr != nil && guard != nil && guard.proxyURL == ""
	if proxyErr != nil && !noProxy {
		snap.ProxyError = proxyErr.Error()
	} else {
		snap.ProxyError = ""
	}

	if !snap.IsClaude || credentialBool(snap.Credential, "disabled") || authIndex == "" {
		if err := saveSnapshot(snap); err != nil {
			return nil, err
		}
		return snap, nil
	}

	now := common.GetTimestamp()
	snap.QuotaCheckedAt = now
	direct := false
	if noProxy {
		// No proxy_url at all: the user leaves test accounts like this on
		// purpose, so read-only quota queries still go through CPA (which
		// connects directly). A proxy_url that is set but invalid stays
		// blocked below, and token refresh / reset grants keep requiring SOCKS.
		guard = directGuard(name)
		direct = true
	} else if proxyErr != nil {
		// Invalid SOCKS proxy: do not let CPA reach Anthropic at all.
		snap.QuotaError = proxyErr.Error()
		snap.QuotaErrorStatus = 0
		trackCpaProxyHealth(snap, proxyErr)
		if err := saveSnapshot(snap); err != nil {
			return nil, err
		}
		return snap, nil
	}
	usage, status, usageErr := fetchClaudeJSON(ctx, client, guard, authIndex, claudeUsageURL, true)
	if !direct {
		// Without a proxy there is no proxy health to track.
		trackCpaProxyHealth(snap, usageErr)
	}
	if usageErr != nil {
		snap.QuotaError = usageErr.Error()
		snap.QuotaErrorStatus = status
	} else {
		snap.Usage = usage
		snap.QuotaObservedAt = now
		snap.QuotaError = ""
		snap.QuotaErrorStatus = 0
		recordCpaQuotaPoint(snap)
		if profile, _, profileErr := fetchClaudeJSON(ctx, client, guard, authIndex, claudeProfileURL, false); profileErr == nil {
			snap.PlanType = resolveClaudePlanType(profile)
		}
	}

	grants, _, grantsErr := fetchClaudeJSON(ctx, client, guard, authIndex, claudeAPIOrigin+"/api/oauth/usage?cedar_ember=1&skip_spend=1", false)
	if grantsErr != nil {
		snap.ResetGrantsError = grantsErr.Error()
	} else if block, ok := grants["cedar_ember"].(map[string]any); ok {
		snap.ResetGrants = block
		snap.ResetGrantsError = ""
	} else {
		snap.ResetGrants = nil
		snap.ResetGrantsError = ""
	}

	if err := saveSnapshot(snap); err != nil {
		return nil, err
	}
	return snap, nil
}

func fetchClaudeJSON(ctx context.Context, client *cpaClient, guard *socksGuard, authIndex, target string, accept bool) (map[string]any, int, error) {
	result, err := client.apiCall(ctx, guard, authIndex, http.MethodGet, target, claudeRequestHeaders(accept), "")
	if err != nil {
		return nil, 0, err
	}
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return nil, result.StatusCode, fmt.Errorf("upstream HTTP %d: %s", result.StatusCode, extractErrorMessage([]byte(result.Body)))
	}
	var parsed map[string]any
	if err := common.UnmarshalJsonStr(result.Body, &parsed); err != nil {
		return nil, result.StatusCode, errors.New("upstream returned invalid JSON")
	}
	return parsed, result.StatusCode, nil
}

func flagValue(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case float64:
		return t != 0, true
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true", "1", "yes", "y", "on":
			return true, true
		case "false", "0", "no", "n", "off":
			return false, true
		}
	}
	return false, false
}

func resolveClaudePlanType(profile map[string]any) string {
	org, _ := profile["organization"].(map[string]any)
	account, _ := profile["account"].(map[string]any)
	if org != nil {
		orgType, _ := org["organization_type"].(string)
		subStatus, _ := org["subscription_status"].(string)
		if strings.EqualFold(orgType, "claude_team") && strings.EqualFold(subStatus, "active") {
			return "plan_team"
		}
		tier, _ := org["rate_limit_tier"].(string)
		if tier == "" {
			tier, _ = org["rateLimitTier"].(string)
		}
		tier = strings.ToLower(tier)
		if strings.HasSuffix(tier, "claude_max_20x") {
			return "plan_max20"
		}
		if strings.HasSuffix(tier, "claude_max_5x") {
			return "plan_max5"
		}
	}
	if account == nil {
		return ""
	}
	hasMax, maxKnown := flagValue(account["has_claude_max"])
	if hasMax {
		return "plan_max"
	}
	hasPro, proKnown := flagValue(account["has_claude_pro"])
	if hasPro {
		return "plan_pro"
	}
	if maxKnown && proKnown {
		return "plan_free"
	}
	return ""
}

// ---- refresh all / background ----

type CpaRefreshPlan struct {
	Scheduled   int   `json:"scheduled"`
	SpreadMs    int64 `json:"spread_ms"`
	SyncErrors  int   `json:"sync_errors"`
	CooldownSec int   `json:"cooldown_seconds"`
}

type CpaCooldownError struct{ RemainingSeconds int64 }

func (e *CpaCooldownError) Error() string {
	return fmt.Sprintf("refresh all is cooling down, retry in %d seconds", e.RemainingSeconds)
}

var ErrCpaRefreshAllRunning = errors.New("refresh all is already running")

func refreshTargets() []CpaAccountSnapshot {
	snaps, err := ListCpaSnapshots()
	if err != nil {
		return nil
	}
	return snaps
}

// StartCpaRefreshAll syncs every CPA list, then fires one refresh per account
// spread randomly over 5-10 seconds. It returns right after scheduling.
func StartCpaRefreshAll() (*CpaRefreshPlan, error) {
	setting, err := model.GetCpaSubscriptionSetting()
	if err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	cooldown := int64(setting.RefreshAllCooldownSeconds)
	if setting.LastRefreshAllAt > 0 && now-setting.LastRefreshAllAt < cooldown {
		return nil, &CpaCooldownError{RemainingSeconds: cooldown - (now - setting.LastRefreshAllAt)}
	}
	if !cpaRefreshAllBusy.CompareAndSwap(false, true) {
		return nil, ErrCpaRefreshAllRunning
	}
	if err := model.MarkCpaRefreshAllAt(now); err != nil {
		cpaRefreshAllBusy.Store(false)
		return nil, err
	}

	ctx := context.Background()
	syncErrs := SyncAllCpaInstances(ctx)
	targets := refreshTargets()
	spread := cpaRefreshAllMinSpread + time.Duration(rand.Int64N(int64(cpaRefreshAllMaxSpread-cpaRefreshAllMinSpread)+1))
	delays := staggerDelays(len(targets), spread)

	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Add(1)
		instanceID, name, delay := target.InstanceID, target.Name, delays[i]
		gopool.Go(func() {
			defer wg.Done()
			time.Sleep(delay)
			if _, refreshErr := RefreshCpaAccount(ctx, instanceID, name); refreshErr != nil {
				common.SysLog(fmt.Sprintf("cpa subscription refresh %s failed: %v", name, refreshErr))
			}
		})
	}
	gopool.Go(func() {
		wg.Wait()
		cpaRefreshAllBusy.Store(false)
	})
	return &CpaRefreshPlan{
		Scheduled:   len(targets),
		SpreadMs:    spread.Milliseconds(),
		SyncErrors:  len(syncErrs),
		CooldownSec: setting.RefreshAllCooldownSeconds,
	}, nil
}

// staggerDelays spreads n requests over the window: even slots plus jitter,
// shuffled so the order is not predictable.
func staggerDelays(n int, window time.Duration) []time.Duration {
	delays := make([]time.Duration, n)
	if n == 0 {
		return delays
	}
	slot := window / time.Duration(n)
	for i := range delays {
		jitter := time.Duration(0)
		if slot > 0 {
			jitter = time.Duration(rand.Int64N(int64(slot)))
		}
		delays[i] = time.Duration(i)*slot + jitter
	}
	rand.Shuffle(n, func(i, j int) { delays[i], delays[j] = delays[j], delays[i] })
	return delays
}

func IsCpaRefreshAllRunning() bool { return cpaRefreshAllBusy.Load() }

// StartCpaSubscriptionScheduler runs background auto-refresh on the master
// node. Every account gets an extra random delay of 0-10 minutes.
func StartCpaSubscriptionScheduler() {
	cpaSchedulerOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		startCpaUsageCollector()
		gopool.Go(func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			var nextRun int64
			for range ticker.C {
				setting, err := model.GetCpaSubscriptionSetting()
				if err != nil || !setting.AutoRefreshEnabled {
					nextRun = 0
					continue
				}
				now := common.GetTimestamp()
				if nextRun == 0 {
					nextRun = now + int64(setting.AutoRefreshIntervalMinutes)*60
					continue
				}
				if now < nextRun {
					continue
				}
				nextRun = now + int64(setting.AutoRefreshIntervalMinutes)*60
				runCpaAutoRefresh()
			}
		})
	})
}

func runCpaAutoRefresh() {
	ctx := context.Background()
	SyncAllCpaInstances(ctx)
	for _, target := range refreshTargets() {
		instanceID, name := target.InstanceID, target.Name
		delay := time.Duration(rand.Int64N(int64(cpaAutoMaxJitter)))
		gopool.Go(func() {
			time.Sleep(delay)
			if _, err := RefreshCpaAccount(ctx, instanceID, name); err != nil {
				common.SysLog(fmt.Sprintf("cpa subscription auto refresh %s failed: %v", name, err))
			}
		})
	}
}

// ---- write actions ----

func clientForAccount(instanceID int, name string) (*cpaClient, *CpaAccountSnapshot, error) {
	instance, err := model.GetCpaInstance(instanceID)
	if err != nil {
		return nil, nil, err
	}
	client, err := newCpaClient(instance)
	if err != nil {
		return nil, nil, err
	}
	snap, err := loadSnapshot(instanceID, name)
	if err != nil {
		return nil, nil, err
	}
	if snap.Credential == nil {
		return nil, nil, ErrCpaAccountNotFound
	}
	return client, snap, nil
}

func resyncInstance(instanceID int) {
	if instance, err := model.GetCpaInstance(instanceID); err == nil {
		_, _ = SyncCpaInstance(context.Background(), instance)
	}
}

func SetCpaAccountDisabled(ctx context.Context, instanceID int, name string, disabled bool) error {
	client, snap, err := clientForAccount(instanceID, name)
	if err != nil {
		return err
	}
	body := map[string]any{"name": name, "disabled": disabled}
	if idx := credentialString(snap.Credential, "auth_index"); idx != "" {
		body["auth_index"] = idx
	}
	if err := client.do(ctx, http.MethodPatch, "/credentials/status", nil, body, nil); err != nil {
		return err
	}
	resyncInstance(instanceID)
	return nil
}

// RefreshCpaAccountToken forces a token refresh; the CPA response (which may
// contain token material) is discarded.
func RefreshCpaAccountToken(ctx context.Context, instanceID int, name string) error {
	client, snap, err := clientForAccount(instanceID, name)
	if err != nil {
		return err
	}
	// Token refresh goes to Anthropic through CPA with the account proxy; an
	// empty or malformed proxy would make CPA connect directly.
	if _, err := client.requireSocksProxy(ctx, name); err != nil {
		return err
	}
	body := map[string]any{"name": name}
	if idx := credentialString(snap.Credential, "auth_index"); idx != "" {
		body["auth_index"] = idx
	}
	if err := client.do(ctx, http.MethodPost, "/credentials/refresh", nil, body, nil); err != nil {
		return err
	}
	resyncInstance(instanceID)
	return nil
}

// RevealCpaAccountProxy reads the account's proxy_url from CPA unmasked.
func RevealCpaAccountProxy(ctx context.Context, instanceID int, name string) (string, error) {
	client, _, err := clientForAccount(instanceID, name)
	if err != nil {
		return "", err
	}
	return client.credentialProxyURL(ctx, name)
}

func ListCpaAccountModels(ctx context.Context, instanceID int, name string) ([]map[string]any, error) {
	client, _, err := clientForAccount(instanceID, name)
	if err != nil {
		return nil, err
	}
	var out struct {
		Models []map[string]any `json:"models"`
	}
	if err := client.do(ctx, http.MethodGet, "/credentials/models", url.Values{"name": {name}}, nil, &out); err != nil {
		return nil, err
	}
	models := make([]map[string]any, 0, len(out.Models))
	for _, m := range out.Models {
		models = append(models, map[string]any{
			"id": m["id"], "display_name": m["display_name"], "type": m["type"], "owned_by": m["owned_by"],
		})
	}
	return models, nil
}

// ---- reset grant (cedar_ember) ----

type cpaGrant struct {
	ID               string
	ResetsLeft       int
	Paused           bool
	UsableNow        bool
	UseRequiresLimit bool
	StartsAt         *time.Time
	EndsAt           *time.Time
}

type pendingClaim struct {
	RequestID string
	GrantID   string
	At        time.Time
}

func optTime(v any) *time.Time {
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return &t
}

func optBool(v any, fallback bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return fallback
}

// selectCpaGrant mirrors the panel: upstream eligibility, unpaused, usable,
// has resets left, at limit when required, inside its window, not in cooldown.
func selectCpaGrant(block map[string]any, now time.Time) (string, string) {
	if eligible, _ := block["eligible"].(bool); !eligible {
		return "", "ineligible"
	}
	if cd := optTime(block["cooldown_until"]); cd != nil && cd.After(now) {
		return "", "cooldown"
	}
	atLimit := optBool(block["at_limit"], false)
	rawGrants, _ := block["grants"].([]any)
	var usable []cpaGrant
	reason := "no_grant"
	for _, raw := range rawGrants {
		g, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := g["id"].(string)
		if !cpaGrantIDRe.MatchString(id) {
			continue
		}
		left, _ := g["resets_left"].(float64)
		grant := cpaGrant{
			ID: id, ResetsLeft: int(left),
			Paused: optBool(g["paused"], false), UsableNow: optBool(g["usable_now"], false),
			UseRequiresLimit: optBool(g["use_requires_limit"], true),
			StartsAt:         optTime(g["starts_at"]), EndsAt: optTime(g["ends_at"]),
		}
		blocker := ""
		switch {
		case grant.ResetsLeft <= 0:
			blocker = "exhausted"
		case grant.Paused:
			blocker = "paused"
		case !grant.UsableNow:
			blocker = "not_usable"
		case grant.UseRequiresLimit && !atLimit:
			blocker = "not_limited"
		case grant.StartsAt != nil && grant.StartsAt.After(now):
			blocker = "not_started"
		case grant.EndsAt != nil && !grant.EndsAt.After(now):
			blocker = "expired"
		default:
			usable = append(usable, grant)
		}
		// An exhausted grant explains nothing when another grant has resets left.
		if blocker != "" && (reason == "no_grant" || reason == "exhausted") {
			reason = blocker
		}
	}
	if len(usable) == 0 {
		return "", reason
	}
	if next, _ := block["next_grant_id"].(string); next != "" {
		for _, g := range usable {
			if g.ID == next {
				return g.ID, ""
			}
		}
	}
	sort.Slice(usable, func(i, j int) bool { return usable[i].ID < usable[j].ID })
	return usable[0].ID, ""
}

type CpaResetResult struct {
	Result  string `json:"result"`
	GrantID string `json:"grant_id,omitempty"`
}

// ClaimCpaResetGrant spends one reset grant for the account. An unknown outcome
// is retried with the same request_id within 10 minutes so it is never spent twice.
func ClaimCpaResetGrant(ctx context.Context, instanceID int, name string) (*CpaResetResult, error) {
	client, snap, err := clientForAccount(instanceID, name)
	if err != nil {
		return nil, err
	}
	authIndex := credentialString(snap.Credential, "auth_index")
	if authIndex == "" {
		return nil, errors.New("account has no auth_index")
	}
	guard, err := client.requireSocksProxy(ctx, name)
	if err != nil {
		return nil, err
	}
	profile, _, err := fetchClaudeJSON(ctx, client, guard, authIndex, claudeProfileURL, false)
	if err != nil {
		return nil, err
	}
	org, _ := profile["organization"].(map[string]any)
	orgUUID, _ := org["uuid"].(string)
	if !cpaOrgUUIDRe.MatchString(orgUUID) {
		return nil, errors.New("cannot read organization of the account")
	}
	orgUUID = strings.ToLower(orgUUID)

	status, _, err := fetchClaudeJSON(ctx, client, guard, authIndex, claudeAPIOrigin+"/api/oauth/usage?cedar_ember=1&skip_spend=1", false)
	if err != nil {
		return nil, err
	}
	block, _ := status["cedar_ember"].(map[string]any)
	if block == nil {
		return nil, errors.New("account has no reset grant information")
	}

	key := accountKey(instanceID, name)
	now := time.Now()
	var grantID, requestID string
	if v, ok := cpaPendingClaims.Load(key); ok {
		pending := v.(pendingClaim)
		if now.Sub(pending.At) < cpaClaimRetryWindow {
			grantID, requestID = pending.GrantID, pending.RequestID
		} else {
			cpaPendingClaims.Delete(key)
		}
	}
	if grantID == "" {
		selected, reason := selectCpaGrant(block, now)
		if selected == "" {
			return &CpaResetResult{Result: "blocked_" + reason}, nil
		}
		grantID, requestID = selected, common.GetUUID()
	}

	payload, _ := common.Marshal(map[string]any{
		"program": "cedar_ember", "grant_id": grantID, "request_id": requestID,
	})
	claimCtx, cancel := context.WithTimeout(ctx, cpaClaimTimeout)
	defer cancel()
	target := fmt.Sprintf("%s/api/organizations/%s/reset_rate_limits", claudeAPIOrigin, orgUUID)
	result, callErr := client.apiCall(claimCtx, guard, authIndex, http.MethodPost, target, claudeRequestHeaders(false), string(payload))

	settled := ""
	if callErr == nil {
		switch {
		case result.StatusCode == http.StatusTooManyRequests:
			settled = "rate_limited"
		case result.StatusCode == http.StatusUnauthorized || result.StatusCode == http.StatusForbidden:
			settled = "auth_error"
		case result.StatusCode >= 200 && result.StatusCode < 300:
			var body map[string]any
			if common.UnmarshalJsonStr(result.Body, &body) == nil {
				if r, ok := body["result"].(string); ok {
					switch r {
					case "reset", "already_used", "not_limited", "cooldown", "ineligible", "unavailable":
						settled = r
					}
				}
			}
		}
	}
	if settled == "" {
		cpaPendingClaims.Store(key, pendingClaim{RequestID: requestID, GrantID: grantID, At: now})
		return &CpaResetResult{Result: "unknown", GrantID: grantID}, nil
	}
	cpaPendingClaims.Delete(key)
	if settled == "reset" || settled == "already_used" {
		_ = client.do(ctx, http.MethodPost, "/routing/cooldown/reset", nil, map[string]any{"auth_index": authIndex}, nil)
	}
	gopool.Go(func() {
		_, _ = RefreshCpaAccount(context.Background(), instanceID, name)
	})
	return &CpaResetResult{Result: settled, GrantID: grantID}, nil
}
