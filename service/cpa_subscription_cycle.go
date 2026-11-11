package service

import (
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// Cycle comparison (design draft 2026-11-11): every weekly quota window of
// every account is cut into reset periods. For each period we keep the
// newest used percent seen and, once it ends, the tokens spent inside it, so
// the dashboard can estimate "a full window = tokens / used%" and compare
// periods. Tokens = input + output + cache creation (cache reads excluded,
// user decision A); only traffic through CPA is counted.

const (
	cpaCycleWindowSeconds = 7 * 86400
	// resets_at of the same period may drift a little between reads.
	cpaCycleResetTolerance = 3600
	// Below this used percent the estimate is too noisy (user decision).
	cpaCycleMinPercent = 20.0
)

// Windows taking part, with the models whose tokens count toward them.
// seven_day_oauth_apps / seven_day_cowork cannot be mapped to models and are
// left out.
var cpaCycleWindows = []string{"seven_day", "seven_day_opus", "seven_day_sonnet", "seven_day_fable"}

func cpaCycleModelMatches(window, modelName string) bool {
	m := strings.ToLower(modelName)
	switch window {
	case "seven_day":
		return true
	case "seven_day_opus":
		return strings.Contains(m, "opus")
	case "seven_day_sonnet":
		return strings.Contains(m, "sonnet")
	case "seven_day_fable":
		return strings.Contains(m, "fable")
	}
	return false
}

// cpaCycleTokens counts the window's tokens in one hourly row.
func cpaCycleTokens(row *model.CpaUsageHourly) int64 {
	return row.InputTokens + row.OutputTokens + row.CacheCreationTokens
}

type cpaCycleObservation struct {
	Window   string
	Percent  float64
	ResetsAt int64
	Limited  bool
	At       int64
	Source   string // "refresh" | "chat"
}

var cpaCycleMu sync.Mutex

func parseCpaResetTime(v any) int64 {
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0
		}
		if parsed, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return parsed.Unix()
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	case float64:
		return int64(t)
	}
	return 0
}

func anyToFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f, err == nil
	}
	return 0, false
}

// cpaCycleObservationsFromUsage reads the weekly windows of a refreshed
// /api/oauth/usage body. Percent is 0-100.
func cpaCycleObservationsFromUsage(usage map[string]any, at int64) []cpaCycleObservation {
	if usage == nil {
		return nil
	}
	out := make([]cpaCycleObservation, 0, 4)
	for _, key := range []string{"seven_day", "seven_day_opus", "seven_day_sonnet"} {
		window, ok := usage[key].(map[string]any)
		if !ok {
			continue
		}
		pct, ok := anyToFloat(window["utilization"])
		reset := parseCpaResetTime(window["resets_at"])
		if !ok || reset <= 0 {
			continue
		}
		out = append(out, cpaCycleObservation{Window: key, Percent: pct, ResetsAt: reset, At: at, Source: "refresh"})
	}
	if fable := cpaFableObservation(usage, at); fable != nil {
		out = append(out, *fable)
	}
	return out
}

// cpaFableObservation mirrors the card logic (lib/quota.ts findFableLimit):
// a weekly_scoped limit on model "Fable" first, else the iguana_necktie
// window when it is a percent window (not a dollar balance).
func cpaFableObservation(usage map[string]any, at int64) *cpaCycleObservation {
	if limits, ok := usage["limits"].([]any); ok {
		var picked map[string]any
		for _, item := range limits {
			limit, ok := item.(map[string]any)
			if !ok || strings.ToLower(strings.TrimSpace(credentialString(limit, "kind"))) != "weekly_scoped" {
				continue
			}
			scope, _ := limit["scope"].(map[string]any)
			scopeModel, _ := scope["model"].(map[string]any)
			name := strings.ToLower(strings.TrimSpace(credentialString(scopeModel, "display_name")))
			if name != "fable" && name != "fable 5" {
				continue
			}
			if _, ok := anyToFloat(limit["percent"]); !ok {
				continue
			}
			if picked == nil || limit["is_active"] == true {
				picked = limit
			}
		}
		if picked != nil {
			pct, _ := anyToFloat(picked["percent"])
			if reset := parseCpaResetTime(picked["resets_at"]); reset > 0 {
				return &cpaCycleObservation{Window: "seven_day_fable", Percent: pct, ResetsAt: reset, At: at, Source: "refresh"}
			}
		}
	}
	window, ok := usage["iguana_necktie"].(map[string]any)
	if !ok {
		return nil
	}
	if _, isDollar := window["limit_dollars"]; isDollar {
		return nil
	}
	pct, ok := anyToFloat(window["utilization"])
	reset := parseCpaResetTime(window["resets_at"])
	if !ok || reset <= 0 {
		return nil
	}
	return &cpaCycleObservation{Window: "seven_day_fable", Percent: pct, ResetsAt: reset, At: at, Source: "refresh"}
}

// cpaCycleObservationFromCredential reads the 7-day window CPA saw on the
// last chat (credential.quota.signals; utilization is a 0-1 fraction).
func cpaCycleObservationFromCredential(cred map[string]any) *cpaCycleObservation {
	quota, _ := cred["quota"].(map[string]any)
	signals, _ := quota["signals"].(map[string]any)
	if signals == nil {
		return nil
	}
	at := parseCpaResetTime(quota["observed_at"])
	if at <= 0 {
		return nil
	}
	get := func(name string) any {
		wanted := strings.ToLower("anthropic-ratelimit-unified-7d-" + name)
		for k, v := range signals {
			if strings.ToLower(k) == wanted {
				return v
			}
		}
		return nil
	}
	fraction, ok := anyToFloat(get("utilization"))
	reset := parseCpaResetTime(get("reset"))
	if !ok || reset <= 0 {
		return nil
	}
	status, _ := get("status").(string)
	return &cpaCycleObservation{
		Window: "seven_day", Percent: fraction * 100, ResetsAt: reset, At: at, Source: "chat",
		Limited: strings.EqualFold(strings.TrimSpace(status), "rejected"),
	}
}

func cpaCycleNow() int64 { return common.GetTimestamp() }

// cpaCycleSeen de-duplicates observations: saveSnapshot runs on every
// credential change, but a window only needs work when its reading changes.
var cpaCycleSeen sync.Map // "instance/name/window" -> "at/percent/reset/limited/plan"

// observeCpaCycles feeds the weekly windows of a snapshot into the cycle
// table. Called from saveSnapshot; errors are ignored (statistics only).
func observeCpaCycles(snap *CpaAccountSnapshot) {
	if snap == nil || !snap.IsClaude || snap.Credential == nil {
		return
	}
	authIndex := credentialString(snap.Credential, "auth_index")
	if authIndex == "" {
		return
	}
	obs := cpaCycleObservationsFromUsage(snap.Usage, snap.QuotaObservedAt)
	if chat := cpaCycleObservationFromCredential(snap.Credential); chat != nil {
		obs = append(obs, *chat)
	}
	for _, o := range obs {
		if o.At <= 0 {
			continue
		}
		key := accountKey(snap.InstanceID, snap.Name) + "/" + o.Window
		sig := strconv.FormatInt(o.At, 10) + "/" + strconv.FormatFloat(o.Percent, 'f', 2, 64) + "/" +
			strconv.FormatInt(o.ResetsAt, 10) + "/" + strconv.FormatBool(o.Limited) + "/" + snap.PlanType
		if prev, ok := cpaCycleSeen.Load(key); ok && prev.(string) == sig {
			continue
		}
		if applyCpaCycleObservation(snap.InstanceID, snap.Name, authIndex, snap.PlanType, o) == nil {
			cpaCycleSeen.Store(key, sig)
		}
	}
}

// applyCpaCycleObservation updates or rolls the cycle of one account window.
func applyCpaCycleObservation(instanceID int, name, authIndex, plan string, o cpaCycleObservation) error {
	cpaCycleMu.Lock()
	defer cpaCycleMu.Unlock()
	latest, err := model.GetLatestCpaQuotaCycle(instanceID, name, o.Window)
	if err != nil {
		return err
	}
	start := o.ResetsAt - cpaCycleWindowSeconds
	if latest != nil {
		if latest.EndReason == "" {
			switch {
			case abs64(latest.EndTs-o.ResetsAt) <= cpaCycleResetTolerance:
				if latest.PlanType != "" && plan != "" && latest.PlanType != plan {
					if err := closeCpaQuotaCycle(latest, "plan_change", o.At); err != nil {
						return err
					}
					return openCpaQuotaCycle(instanceID, name, authIndex, plan, o, max(o.At, latest.StartTs))
				}
				updateCpaCycleReading(latest, plan, o)
				return model.SaveCpaQuotaCycle(latest)
			case o.ResetsAt > latest.EndTs:
				// The window moved on. Before its old end means a reset grant
				// (or another early reset) was used.
				reason, endTs := "natural", latest.EndTs
				if start < latest.EndTs-cpaCycleResetTolerance {
					reason, endTs = "manual_reset", max(start, latest.StartTs)
				}
				if err := closeCpaQuotaCycle(latest, reason, endTs); err != nil {
					return err
				}
				return openCpaQuotaCycle(instanceID, name, authIndex, plan, o, max(start, endTs))
			default:
				return nil // older reading of an earlier period
			}
		}
		if o.ResetsAt <= latest.EndTs+cpaCycleResetTolerance {
			return nil
		}
		return openCpaQuotaCycle(instanceID, name, authIndex, plan, o, max(start, latest.EndTs))
	}
	return openCpaQuotaCycle(instanceID, name, authIndex, plan, o, start)
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func updateCpaCycleReading(cycle *model.CpaQuotaCycle, plan string, o cpaCycleObservation) {
	if cycle.PlanType == "" {
		cycle.PlanType = plan
	}
	if o.Limited || o.Percent >= 99.5 {
		cycle.HitLimit = true
	}
	if o.At >= cycle.LastPercentAt {
		pct := o.Percent
		cycle.LastPercent = &pct
		cycle.LastPercentAt = o.At
		cycle.LastPercentSource = o.Source
	}
}

func openCpaQuotaCycle(instanceID int, name, authIndex, plan string, o cpaCycleObservation, start int64) error {
	cycle := &model.CpaQuotaCycle{
		InstanceID: instanceID, Name: name, Window: o.Window, AuthIndex: authIndex,
		StartTs: start, EndTs: o.ResetsAt, PlanType: plan,
	}
	updateCpaCycleReading(cycle, plan, o)
	return model.SaveCpaQuotaCycle(cycle)
}

// cpaCycleTokenSum adds the window's tokens of one account in [from, to).
func cpaCycleTokenSum(instanceID int, authIndex, window string, from, to int64) (tokens, cacheRead int64, err error) {
	if to <= from {
		return 0, 0, nil
	}
	rows, err := model.ListCpaUsageHourlyOfAccount(instanceID, authIndex, from-from%3600, to)
	if err != nil {
		return 0, 0, err
	}
	for i := range rows {
		if !cpaCycleModelMatches(window, rows[i].ModelName) {
			continue
		}
		tokens += cpaCycleTokens(&rows[i])
		cacheRead += rows[i].CacheReadTokens
	}
	return tokens, cacheRead, nil
}

// closeCpaQuotaCycle freezes the token totals (hourly rows are pruned
// after 90 days) and marks the cycle ended.
func closeCpaQuotaCycle(cycle *model.CpaQuotaCycle, reason string, endTs int64) error {
	cycle.EndTs = endTs
	tokens, cacheRead, err := cpaCycleTokenSum(cycle.InstanceID, cycle.AuthIndex, cycle.Window, cycle.StartTs, endTs)
	if err != nil {
		return err
	}
	cycle.Tokens, cycle.CacheReadTokens = tokens, cacheRead
	cycle.EndReason = reason
	cycle.ClosedAt = cpaCycleNow()
	return model.SaveCpaQuotaCycle(cycle)
}

// closeExpiredCpaQuotaCycles ends cycles whose reset time passed without a
// newer reading (e.g. the account was idle).
func closeExpiredCpaQuotaCycles(now int64) {
	cpaCycleMu.Lock()
	defer cpaCycleMu.Unlock()
	cycles, err := model.ListOpenCpaQuotaCyclesEndedBefore(now - cpaCycleResetTolerance)
	if err != nil {
		return
	}
	for i := range cycles {
		_ = closeCpaQuotaCycle(&cycles[i], "natural", cycles[i].EndTs)
	}
}

// backfillCpaQuotaCycles runs once at start: every account gets its current
// period from the cached snapshot and, when the 7-day total has no older
// period yet, the previous one rebuilt from the 14-day quota samples. Backfilled rows
// are flagged "for reference only" (user decision B).
func backfillCpaQuotaCycles() {
	snaps, err := ListCpaSnapshots()
	if err != nil {
		return
	}
	points, _ := model.ListCpaQuotaPoints(0)
	for i := range snaps {
		snap := &snaps[i]
		if !snap.IsClaude || snap.Credential == nil {
			continue
		}
		authIndex := credentialString(snap.Credential, "auth_index")
		if authIndex == "" {
			continue
		}
		observeCpaCycles(snap)
		current, err := model.GetLatestCpaQuotaCycle(snap.InstanceID, snap.Name, "seven_day")
		if err != nil || current == nil {
			continue
		}
		if older, err := model.HasCpaQuotaCycleBefore(snap.InstanceID, snap.Name, "seven_day", current.StartTs); err != nil || older {
			continue
		}
		backfillPreviousCpaCycle(snap, authIndex, current, points)
	}
}

func backfillPreviousCpaCycle(snap *CpaAccountSnapshot, authIndex string, current *model.CpaQuotaCycle, points []model.CpaQuotaPoint) {
	end := current.StartTs
	start := end - cpaCycleWindowSeconds
	var last *model.CpaQuotaPoint
	for i := range points {
		p := &points[i]
		if p.InstanceID != snap.InstanceID || p.Name != snap.Name || p.SevenDay == nil || p.Ts < start || p.Ts >= end {
			continue
		}
		if last == nil || p.Ts > last.Ts {
			last = p
		}
	}
	if last == nil {
		return
	}
	pct := *last.SevenDay
	cycle := &model.CpaQuotaCycle{
		InstanceID: snap.InstanceID, Name: snap.Name, Window: "seven_day", AuthIndex: authIndex,
		StartTs: start, PlanType: current.PlanType, LastPercent: &pct, LastPercentAt: last.Ts,
		LastPercentSource: "refresh", HitLimit: pct >= 99.5, Backfilled: true,
	}
	cpaCycleMu.Lock()
	defer cpaCycleMu.Unlock()
	_ = closeCpaQuotaCycle(cycle, "natural", end)
}

// CpaCycleRow is one period on the cycle comparison tab.
type CpaCycleRow struct {
	ID              int      `json:"id"`
	StartTs         int64    `json:"start_ts"`
	EndTs           int64    `json:"end_ts"`
	Open            bool     `json:"open"`
	EndReason       string   `json:"end_reason"`
	PlanType        string   `json:"plan_type"`
	Tokens          int64    `json:"tokens"`
	CacheReadTokens int64    `json:"cache_read_tokens"`
	UsedPercent     *float64 `json:"used_percent"`
	PercentAt       int64    `json:"percent_at"`
	PercentSource   string   `json:"percent_source"`
	HitLimit        bool     `json:"hit_limit"`
	// EstimatedFull is tokens / used%; nil when used < 20% or unknown.
	EstimatedFull *int64 `json:"estimated_full"`
	// ChangePercent compares EstimatedFull with the previous comparable
	// period (same plan, not across a plan change); nil when not comparable.
	ChangePercent *float64 `json:"change_percent"`
	Backfilled    bool     `json:"backfilled"`
}

type CpaCycleWindow struct {
	Window string        `json:"window"`
	Cycles []CpaCycleRow `json:"cycles"`
}

type CpaCycleAccount struct {
	InstanceID int              `json:"instance_id"`
	Name       string           `json:"name"`
	Email      string           `json:"email,omitempty"`
	PlanType   string           `json:"plan_type,omitempty"`
	Windows    []CpaCycleWindow `json:"windows"`
}

type CpaCycleReport struct {
	MinPercent float64           `json:"min_percent"`
	Accounts   []CpaCycleAccount `json:"accounts"`
}

// BuildCpaCycleReport lists every account's weekly cycles, newest first.
func BuildCpaCycleReport() (*CpaCycleReport, error) {
	cycles, err := model.ListCpaQuotaCycles()
	if err != nil {
		return nil, err
	}
	snaps, err := ListCpaSnapshots()
	if err != nil {
		return nil, err
	}
	report := &CpaCycleReport{MinPercent: cpaCycleMinPercent, Accounts: []CpaCycleAccount{}}
	index := map[string]int{}
	for i := range snaps {
		snap := &snaps[i]
		if !snap.IsClaude {
			continue
		}
		index[accountKey(snap.InstanceID, snap.Name)] = len(report.Accounts)
		report.Accounts = append(report.Accounts, CpaCycleAccount{
			InstanceID: snap.InstanceID, Name: snap.Name, PlanType: snap.PlanType,
			Email: credentialString(snap.Credential, "email"), Windows: []CpaCycleWindow{},
		})
	}
	now := cpaCycleNow()
	grouped := map[string][]model.CpaQuotaCycle{}
	order := []string{}
	for _, c := range cycles {
		key := accountKey(c.InstanceID, c.Name) + "\x00" + c.Window
		if _, ok := grouped[key]; !ok {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], c)
	}
	for _, key := range order {
		list := grouped[key]
		first := list[0]
		pos, ok := index[accountKey(first.InstanceID, first.Name)]
		if !ok {
			pos = len(report.Accounts)
			index[accountKey(first.InstanceID, first.Name)] = pos
			report.Accounts = append(report.Accounts, CpaCycleAccount{InstanceID: first.InstanceID, Name: first.Name, Windows: []CpaCycleWindow{}})
		}
		rows := make([]CpaCycleRow, 0, len(list))
		for _, c := range list { // start_ts desc
			row := CpaCycleRow{
				ID: c.ID, StartTs: c.StartTs, EndTs: c.EndTs, Open: c.EndReason == "", EndReason: c.EndReason,
				PlanType: c.PlanType, Tokens: c.Tokens, CacheReadTokens: c.CacheReadTokens,
				UsedPercent: c.LastPercent, PercentAt: c.LastPercentAt, PercentSource: c.LastPercentSource,
				HitLimit: c.HitLimit, Backfilled: c.Backfilled,
			}
			if row.Open {
				row.Tokens, row.CacheReadTokens, _ = cpaCycleTokenSum(c.InstanceID, c.AuthIndex, c.Window, c.StartTs, min(now+1, c.EndTs))
			}
			pct := 0.0
			if c.LastPercent != nil {
				pct = *c.LastPercent
			}
			if c.HitLimit {
				pct = 100
				row.UsedPercent = &pct
			}
			if pct >= cpaCycleMinPercent && row.Tokens > 0 {
				full := int64(float64(row.Tokens) * 100 / pct)
				row.EstimatedFull = &full
			}
			rows = append(rows, row)
		}
		// Compare each period with the next older comparable one.
		for i := range rows {
			if rows[i].EstimatedFull == nil {
				continue
			}
			for j := i + 1; j < len(rows); j++ {
				// A plan change ends rows[j]; never compare across it.
				if rows[j].EndReason == "plan_change" || (rows[j].PlanType != "" && rows[i].PlanType != "" && rows[j].PlanType != rows[i].PlanType) {
					break
				}
				if rows[j].EstimatedFull == nil || *rows[j].EstimatedFull == 0 {
					continue
				}
				change := (float64(*rows[i].EstimatedFull)/float64(*rows[j].EstimatedFull) - 1) * 100
				rows[i].ChangePercent = &change
				break
			}
		}
		acc := &report.Accounts[pos]
		acc.Windows = append(acc.Windows, CpaCycleWindow{Window: first.Window, Cycles: rows})
	}
	for i := range report.Accounts {
		w := report.Accounts[i].Windows
		rank := func(name string) int {
			for k, v := range cpaCycleWindows {
				if v == name {
					return k
				}
			}
			return len(cpaCycleWindows)
		}
		sort.SliceStable(w, func(a, b int) bool { return rank(w[a].Window) < rank(w[b].Window) })
	}
	return report, nil
}
