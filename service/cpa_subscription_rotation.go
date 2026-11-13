package service

import (
	"context"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

// Account rotation (test build, user decision 2026-11-11: one global set of
// settings, design draft "号池账号轮休"). An account that served requests in
// every one of the last N hours is disabled in CPA (same PATCH as the manual
// switch, no Anthropic request) for the rest time ± jitter, then enabled
// again. Only accounts carrying the rotation mark (RestUntil > 0) are ever
// re-enabled; a manual switch or the SOCKS guard clears the mark. Per CPA at
// most MaxRestPercent of the online Claude accounts rest at once and at least
// MinOnline stay online; the rest queue up.
const (
	cpaRotationTick    = 30 * time.Second
	cpaRotationTimeout = 30 * time.Second
	// The current hour counts as worked once it has a request; a past hour
	// without any request breaks the streak.
	cpaRotationLookbackHours = 72
)

var (
	cpaRotationOnce sync.Once
	cpaRotationMu   sync.Mutex
	// cpaRotationNow is replaced in tests.
	cpaRotationNow = time.Now
)

// CpaRotationState is the per-account rotation view sent to the page.
type CpaRotationState struct {
	InstanceID int    `json:"instance_id"`
	Name       string `json:"name"`
	// online | resting | queued | off (disabled by hand / SOCKS / 401-403) | excluded
	State       string `json:"state"`
	WorkedHours int    `json:"worked_hours"`
	RestUntil   int64  `json:"rest_until,omitempty"`
	QueuePos    int    `json:"queue_pos,omitempty"`
	RestHours   int    `json:"rest_worked_hours,omitempty"`
	RateLimited bool   `json:"rate_limited,omitempty"`
	// Reason of the rest / queue: rotation | shift | limit | quota | daily.
	Reason string `json:"reason,omitempty"`
	// Shift: day | night | all (only while shifts are on).
	Shift string `json:"shift,omitempty"`
	// ShiftOnAt / ShiftOffAt: next on-duty / off-duty moment of the shift.
	ShiftOnAt  int64 `json:"shift_on_at,omitempty"`
	ShiftOffAt int64 `json:"shift_off_at,omitempty"`
	// HoldUntil: woken early because the pool was busy; no rotation rest
	// before this moment.
	HoldUntil    int64 `json:"hold_until,omitempty"`
	TodayHours   int   `json:"today_hours"`
	AuthDisabled bool  `json:"auth_disabled,omitempty"`
	Participates bool  `json:"participates"`
	// Blocked: why a queued account is not resting yet: min_online |
	// percent | batch | capacity | busy.
	Blocked string `json:"blocked,omitempty"`
	// WouldRest: dry run only, the account would be resting right now.
	WouldRest bool `json:"would_rest,omitempty"`
}

// CpaRotationPool is the load of one CPA the safety gates look at.
type CpaRotationPool struct {
	InstanceID int `json:"instance_id"`
	// OnlineBefore / Online: serving accounts before and after this plan.
	OnlineBefore int `json:"online_before"`
	Online       int `json:"online"`
	Resting      int `json:"resting"`
	// PeakRpm / PeakTpm: busiest minute of the last hour (without 429s).
	PeakRpm int64 `json:"peak_rpm"`
	PeakTpm int64 `json:"peak_tpm"`
	// Recent429: 429s of the pool inside the capacity window.
	Recent429 int64 `json:"recent_429"`
}

// CpaRotationSummary feeds the page header line.
type CpaRotationSummary struct {
	Enabled    bool               `json:"enabled"`
	Online     int                `json:"online"`
	Resting    int                `json:"resting"`
	Queued     int                `json:"queued"`
	Off        int                `json:"off"`
	NextOnline int64              `json:"next_online,omitempty"`
	Night      bool               `json:"night"`
	LastWakeAt int64              `json:"last_wake_at,omitempty"`
	Accounts   []CpaRotationState `json:"accounts"`
	// DryRun: nobody is disabled; WouldRest counts who would be.
	DryRun    bool `json:"dry_run"`
	WouldRest int  `json:"would_rest"`
	// Hits: accounts meeting each rule right now (rotation | shift | limit
	// | quota | daily), whether or not the rule is switched on.
	Hits  map[string]int    `json:"hits"`
	Pools []CpaRotationPool `json:"pools"`
}

func startCpaRotation() {
	cpaRotationOnce.Do(func() {
		gopool.Go(func() {
			// Run once right after start so missed wake-ups are made up.
			runCpaRotation()
			ticker := time.NewTicker(cpaRotationTick)
			defer ticker.Stop()
			for range ticker.C {
				runCpaRotation()
			}
		})
	})
}

// cpaRotationAction is one change the rotation loop applies.
type cpaRotationAction struct {
	InstanceID  int
	Name        string
	Kind        string // rest | wake | clear (drop the mark, leave the switch)
	RestUntil   int64
	WorkedHours int
	// Reason: for rest the rest reason (rotation | shift | limit | quota |
	// daily); for wake why it ends (rest_over | rotation_off | busy | ...).
	Reason    string
	HoldUntil int64
}

type cpaRotationPlan struct {
	Summary CpaRotationSummary
	Actions []cpaRotationAction
	// Shifts assigned to accounts that had none (saved by the loop).
	NewShifts map[string]string
	// WakeAt: instances that got a busy wake-up in this run.
	WakeAt map[int]int64
	// WouldRest: rests a dry run held back.
	WouldRest []cpaRotationAction
	// RestAt: instances where an account went resting in this run.
	RestAt map[int]int64
}

// cpaRotationInput is everything the pure planner reads besides settings
// and snapshots.
type cpaRotationInput struct {
	// Active: "instanceID/authIndex" -> hour buckets with requests.
	Active map[string]map[int64]bool
	// Minutes: "instanceID/authIndex" -> recent per-minute rows.
	Minutes map[string][]model.CpaUsageMinute
	Loc     *time.Location
	// LastWake: instanceID -> last busy wake-up (cooldown).
	LastWake map[int]int64
	// LastRest: instanceID -> last time an account went resting (gap
	// between two steps). The planner also reads RestStartedAt of the
	// resting accounts, so the gap survives a restart.
	LastRest map[int]int64
}

// cpaWorkedHours counts consecutive hours with requests ending at the
// current hour. The current hour only counts once it has a request, so a
// streak is not broken just because a new hour has begun. Hours that ended
// before workSince are ignored.
func cpaWorkedHours(active map[int64]bool, now, workSince int64) int {
	hour := now - now%3600
	if !active[hour] {
		hour -= 3600
	}
	count := 0
	for i := 0; i < cpaRotationLookbackHours; i++ {
		if !active[hour] || hour+3600 <= workSince {
			break
		}
		count++
		hour -= 3600
	}
	return count
}

// cpaSevenDayLimited reports a 7-day window rejected on the last chat and
// not reset yet: such an account cannot serve anyway, so it neither rests
// nor takes a rest slot.
func cpaSevenDayLimited(cred map[string]any, now int64) bool {
	quota, _ := cred["quota"].(map[string]any)
	signals, _ := quota["signals"].(map[string]any)
	if signals == nil {
		return false
	}
	status, reset := "", int64(0)
	for key, value := range signals {
		switch strings.ToLower(key) {
		case "anthropic-ratelimit-unified-7d-status":
			status, _ = value.(string)
		case "anthropic-ratelimit-unified-7d-reset":
			reset = parseLooseInt(value)
		}
	}
	return strings.EqualFold(strings.TrimSpace(status), "rejected") && (reset == 0 || reset > now)
}

func parseLooseInt(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return n
	}
	return 0
}

// cpaRestSeconds draws rest minutes ± jitter.
var cpaRestSeconds = func(setting *model.CpaSubscriptionSetting) int64 {
	minutes := setting.RotationRestMinutes
	if j := setting.RotationJitterMinutes; j > 0 {
		minutes += rand.IntN(2*j+1) - j
	}
	return int64(max(minutes, 1)) * 60
}

// cpaRestPriority orders rest reasons (user decision 2026-11-12): safety
// stops are handled before planning, then shift > per-account protection >
// plain rotation.
var cpaRestPriority = map[string]int{"shift": 0, "limit": 1, "quota": 2, "daily": 3, "rotation": 4}

// cpaRefillOrder: which rest is cut short first to keep MinOnline (user
// decision 2026-11-12: an account resting because its quota is almost used
// still has some left, so it comes back first).
var cpaRefillOrder = map[string]int{"quota": 0, "rotation": 1, "daily": 2, "shift": 3, "limit": 4}

// cpaQuotaExhausted reports a quota window that is fully used and not reset
// yet: such an account cannot serve, so it is never brought back early.
func cpaQuotaExhausted(snap *CpaAccountSnapshot, now int64) bool {
	if cpaSevenDayLimited(snap.Credential, now) {
		return true
	}
	for _, key := range []string{"five_hour", "seven_day"} {
		p := usageWindowPercent(snap.Usage, key)
		if p == nil || *p < 100 {
			continue
		}
		window, _ := snap.Usage[key].(map[string]any)
		if reset := parseCpaResetTime(window["resets_at"]); reset == 0 || reset > now {
			return true
		}
	}
	return false
}

// cpaJitter draws a stable offset in [-j, j] minutes per account and day,
// so an account's shift times stay put during the day but differ between
// accounts and days.
func cpaJitter(key, day, salt string, j int) int {
	if j <= 0 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key + "|" + day + "|" + salt))
	return int(h.Sum32()%uint32(2*j+1)) - j
}

// cpaInWindow reports whether minute m lies in [start, end) on a 24 h clock.
func cpaInWindow(m, start, end int) bool {
	m, start, end = (m%1440+1440)%1440, (start%1440+1440)%1440, (end%1440+1440)%1440
	if start == end {
		return false
	}
	if start < end {
		return m >= start && m < end
	}
	return m >= start || m < end
}

// cpaNextClock returns the next moment after now whose local clock is at
// minute m (m may be outside 0..1439 because of jitter).
func cpaNextClock(now int64, loc *time.Location, m int) int64 {
	t := time.Unix(now, 0).In(loc)
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	m = (m%1440 + 1440) % 1440
	next := midnight.Add(time.Duration(m) * time.Minute)
	if !next.After(t) {
		next = next.AddDate(0, 0, 1)
	}
	return next.Unix()
}

// cpaShiftDuty tells whether the shift is on duty now and when it next
// goes on / off duty. "all" is always on duty.
func cpaShiftDuty(s *model.CpaSubscriptionSetting, shift, key string, now int64, loc *time.Location) (on bool, onAt, offAt int64) {
	if shift != "day" && shift != "night" {
		return true, 0, 0
	}
	startClock, endClock := s.RotationDayShiftStart, s.RotationDayShiftEnd
	if shift == "night" {
		startClock, endClock = s.RotationNightShiftStart, s.RotationNightShiftEnd
	}
	start, _ := model.ClockMinutes(startClock)
	end, _ := model.ClockMinutes(endClock)
	t := time.Unix(now, 0).In(loc)
	day := t.Format("2006-01-02")
	start += cpaJitter(key, day, "on", s.RotationShiftJitterMinutes)
	end += cpaJitter(key, day, "off", s.RotationShiftJitterMinutes)
	minute := t.Hour()*60 + t.Minute()
	// Off duty is [end, start); everything else is on duty.
	on = !cpaInWindow(minute, end, start)
	return on, cpaNextClock(now, loc, start), cpaNextClock(now, loc, end)
}

// cpaNightNow reports whether the "rest more at night" window is active.
func cpaNightNow(s *model.CpaSubscriptionSetting, now int64, loc *time.Location) bool {
	if !s.RotationNightEnabled {
		return false
	}
	start, _ := model.ClockMinutes(s.RotationNightStart)
	end, _ := model.ClockMinutes(s.RotationNightEnd)
	t := time.Unix(now, 0).In(loc)
	return cpaInWindow(t.Hour()*60+t.Minute(), start, end)
}

func cpaLocalMidnight(now int64, loc *time.Location) int64 {
	t := time.Unix(now, 0).In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).Unix()
}

// cpaQuotaRest returns when a nearly-used window resets (0 = not full).
func cpaQuotaRest(usage map[string]any, percent int, now int64) (bool, int64) {
	full, until := false, int64(0)
	for _, key := range []string{"five_hour", "seven_day"} {
		p := usageWindowPercent(usage, key)
		if p == nil || *p < float64(percent) {
			continue
		}
		full = true
		window, _ := usage[key].(map[string]any)
		if reset := parseCpaResetTime(window["resets_at"]); reset > now && reset > until {
			until = reset
		}
	}
	return full, until
}

func cpaMinuteSum(rows []model.CpaUsageMinute, from int64, pick func(*model.CpaUsageMinute) int64) int64 {
	var n int64
	for i := range rows {
		if rows[i].BucketTs >= from {
			n += pick(&rows[i])
		}
	}
	return n
}

// planCpaRotation is the pure decision step: given the settings, the cached
// accounts and the usage it reads, it returns the page view and the changes
// to apply. It never touches CPA or the database.
//
// Order per account: safety stops (SOCKS, 401/403, manual off) are never
// touched; then shift > per-account protection (429 limit, quota, daily
// hours) > plain rotation. MinOnline is always kept. The max-rest percentage
// (night percentage inside the night window) caps every reason, and the
// safety gates (batch, gap, capacity) decide who really goes in this run;
// everybody else queues with the reason in Blocked.
func planCpaRotation(setting *model.CpaSubscriptionSetting, snaps []CpaAccountSnapshot, in cpaRotationInput, now int64) cpaRotationPlan {
	loc := in.Loc
	if loc == nil {
		loc = time.Local
	}
	night := setting.RotationEnabled && cpaNightNow(setting, now, loc)
	plan := cpaRotationPlan{
		Summary: CpaRotationSummary{
			Enabled: setting.RotationEnabled, Night: night, Accounts: []CpaRotationState{},
			DryRun: setting.RotationEnabled && setting.RotationDryRun,
			Hits:   map[string]int{"rotation": 0, "shift": 0, "limit": 0, "quota": 0, "daily": 0},
			Pools:  []CpaRotationPool{},
		},
		NewShifts: map[string]string{},
		WakeAt:    map[int]int64{},
		RestAt:    map[int]int64{},
	}
	hits := plan.Summary.Hits
	excluded := setting.RotationExcludedSet()
	shifts := setting.RotationShiftMap()
	minute := now - now%60
	midnight := cpaLocalMidnight(now, loc)

	type candidate struct {
		idx      int
		reason   string
		until    int64
		lastReq  int64
		restFrom int64
		// own429: this account's 429s inside the capacity window.
		own429 int64
		// dead: quota fully used, the account serves nothing anyway.
		dead bool
	}
	type minuteLoad struct{ served, tokens int64 }
	type pool struct {
		// Every resting account counts against the max-rest percentage
		// (user decision 2026-11-12), so resting is the capped number.
		online, resting int
		before          int // really enabled and serving right now
		candidates      []candidate
		wakeable        []candidate
		refill          []candidate // resting accounts that may fill MinOnline
		r429, req5      int64
		// Safety gates: load of the last hour, 429s of the capacity
		// window and the last time an account went resting.
		load     map[int64]minuteLoad
		cap429   int64
		lastRest int64
	}
	pools := map[int]*pool{}
	states := make([]CpaRotationState, 0, len(snaps))

	// Shifts for accounts without one: alternate per CPA, fewer side first.
	if setting.RotationShiftEnabled {
		counts := map[int]map[string]int{}
		var unassigned []int
		for i := range snaps {
			snap := &snaps[i]
			if !snap.IsClaude || snap.Credential == nil {
				continue
			}
			if counts[snap.InstanceID] == nil {
				counts[snap.InstanceID] = map[string]int{}
			}
			if shift, ok := shifts[accountKey(snap.InstanceID, snap.Name)]; ok {
				counts[snap.InstanceID][shift]++
			} else {
				unassigned = append(unassigned, i)
			}
		}
		sort.SliceStable(unassigned, func(a, b int) bool {
			sa, sb := &snaps[unassigned[a]], &snaps[unassigned[b]]
			if sa.InstanceID != sb.InstanceID {
				return sa.InstanceID < sb.InstanceID
			}
			return sa.Name < sb.Name
		})
		for _, i := range unassigned {
			c := counts[snaps[i].InstanceID]
			shift := "day"
			if c["night"] < c["day"] {
				shift = "night"
			}
			c[shift]++
			key := accountKey(snaps[i].InstanceID, snaps[i].Name)
			shifts[key], plan.NewShifts[key] = shift, shift
		}
	}

	for i := range snaps {
		snap := &snaps[i]
		if !snap.IsClaude || snap.Credential == nil {
			continue
		}
		p := pools[snap.InstanceID]
		if p == nil {
			p = &pool{load: map[int64]minuteLoad{}}
			pools[snap.InstanceID] = p
		}
		key := accountKey(snap.InstanceID, snap.Name)
		authIndex := credentialString(snap.Credential, "auth_index")
		usageKeyStr := fmt.Sprintf("%d/%s", snap.InstanceID, authIndex)
		disabled := credentialBool(snap.Credential, "disabled")
		minutes := in.Minutes[usageKeyStr]
		st := CpaRotationState{InstanceID: snap.InstanceID, Name: snap.Name, Participates: !excluded[key]}
		// Work counts from zero when the switch is turned on: an account
		// cannot have worked longer than rotation has been on, so turning
		// it on never sends accounts resting for what they did before.
		st.WorkedHours = cpaWorkedHours(in.Active[usageKeyStr], now, snap.RotationWorkSince)
		if since := setting.RotationEnabledAt; since > 0 && now >= since {
			st.WorkedHours = min(st.WorkedHours, int((now-since)/3600))
		}
		for hour := range in.Active[usageKeyStr] {
			if hour >= midnight {
				st.TodayHours++
			}
		}
		if snap.RotationHoldUntil > now {
			st.HoldUntil = snap.RotationHoldUntil
		}
		onDuty := true
		if setting.RotationShiftEnabled {
			st.Shift = shifts[key]
			onDuty, st.ShiftOnAt, st.ShiftOffAt = cpaShiftDuty(setting, st.Shift, key, now, loc)
		}
		// Pool load for the busy wake-up (every account of this CPA).
		p.r429 += cpaMinuteSum(minutes, now-int64(setting.RotationWake429Minutes)*60, func(m *model.CpaUsageMinute) int64 { return m.Status429 })
		capFrom := now - int64(setting.RotationCapacity429Minutes)*60
		var own429 int64
		for j := range minutes {
			row := &minutes[j]
			if row.BucketTs >= minute-300 && row.BucketTs < minute {
				p.req5 += row.Requests
			}
			if row.BucketTs >= minute-3600 {
				l := p.load[row.BucketTs]
				l.served += cpaServedRequests(row)
				l.tokens += row.Tokens
				p.load[row.BucketTs] = l
			}
			if row.BucketTs >= capFrom {
				own429 += row.Status429
			}
		}
		p.cap429 += own429

		if snap.AuthDisabledAt > 0 {
			st.State, st.AuthDisabled = "off", true
			if !disabled {
				// Enabled again in the CPA panel: the stop is over.
				st.State, st.AuthDisabled = "online", false
				p.online++
				p.before++
				plan.Actions = append(plan.Actions, cpaRotationAction{InstanceID: snap.InstanceID, Name: snap.Name, Kind: "clear", Reason: "enabled_elsewhere"})
			}
			states = append(states, st)
			continue
		}

		if snap.RestUntil > 0 {
			reason := snap.RestReason
			if reason == "" {
				reason = "rotation"
			}
			st.Reason = reason
			wake := ""
			switch {
			case snap.AutoDisabledAt > 0:
				// SOCKS guard took over: never re-enable from here.
				st.State = "off"
				plan.Actions = append(plan.Actions, cpaRotationAction{InstanceID: snap.InstanceID, Name: snap.Name, Kind: "clear", Reason: "socks"})
				states = append(states, st)
				continue
			case !disabled:
				// Enabled elsewhere (CPA panel): the rest is over.
				st.State, st.Reason = "online", ""
				p.online++
				p.before++
				plan.Actions = append(plan.Actions, cpaRotationAction{InstanceID: snap.InstanceID, Name: snap.Name, Kind: "clear", Reason: "enabled_elsewhere"})
				states = append(states, st)
				continue
			case !setting.RotationEnabled:
				wake = "rotation_off"
			case reason == "shift" && (!setting.RotationShiftEnabled || onDuty):
				wake = "shift_on"
			case reason != "shift" && now >= snap.RestUntil:
				wake = "rest_over"
			}
			if wake != "" {
				st.State, st.Reason = "online", ""
				p.online++
				plan.Actions = append(plan.Actions, cpaRotationAction{InstanceID: snap.InstanceID, Name: snap.Name, Kind: "wake", Reason: wake})
				states = append(states, st)
				continue
			}
			st.State = "resting"
			st.RestUntil = snap.RestUntil
			if reason == "shift" && st.ShiftOnAt > 0 {
				st.RestUntil = st.ShiftOnAt
			}
			st.RestHours = snap.RestWorkedHours
			p.resting++
			p.lastRest = max(p.lastRest, snap.RestStartedAt)
			hits[reason]++
			states = append(states, st)
			if (reason == "rotation" && setting.RotationWakeRest) || (reason == "shift" && setting.RotationWakeShift) {
				p.wakeable = append(p.wakeable, candidate{idx: len(states) - 1, reason: reason, until: st.RestUntil, restFrom: snap.RestStartedAt})
			}
			if reason != "quota" || !cpaQuotaExhausted(snap, now) { // a used-up quota cannot serve anyway
				p.refill = append(p.refill, candidate{idx: len(states) - 1, reason: reason, until: st.RestUntil})
			}
			continue
		}
		if disabled {
			st.State = "off"
			states = append(states, st)
			continue
		}
		if cpaSevenDayLimited(snap.Credential, now) {
			// Cannot serve anyway: no rest, no slot, not counted online.
			st.State = "online"
			st.RateLimited = true
			states = append(states, st)
			continue
		}
		p.online++
		p.before++
		st.State = "online"
		if !st.Participates {
			st.State = "excluded"
		}
		states = append(states, st)
		if !st.Participates {
			continue
		}
		// Which rules this account meets right now, whether or not the
		// rule (or rotation itself) is switched on: shown next to each
		// rule in the settings dialog.
		held := st.HoldUntil > now
		offDuty := !onDuty
		if !setting.RotationShiftEnabled {
			// Shifts are off: only for the hit count of the rule.
			on, _, _ := cpaShiftDuty(setting, shifts[key], key, now, loc)
			offDuty = !on
		}
		limited := cpaMinuteSum(minutes, now-int64(setting.RotationLimitMinutes)*60, func(m *model.CpaUsageMinute) int64 { return m.Status429 }) >= int64(setting.RotationLimitCount)
		quotaFull, quotaUntil := cpaQuotaRest(snap.Usage, setting.RotationQuotaPercent, now)
		dailyFull := st.TodayHours >= setting.RotationDailyHours
		tired := st.WorkedHours >= setting.RotationWorkHours
		for reason, hit := range map[string]bool{"shift": offDuty, "limit": limited, "quota": quotaFull, "daily": dailyFull, "rotation": tired} {
			if hit {
				hits[reason]++
			}
		}
		if !setting.RotationEnabled {
			continue
		}
		c := candidate{idx: len(states) - 1, lastReq: snap.LastRequestAt, own429: own429}
		switch {
		case setting.RotationShiftEnabled && offDuty && !held:
			c.reason, c.until = "shift", st.ShiftOnAt
		case setting.RotationLimitEnabled && limited:
			c.reason, c.until = "limit", now+int64(setting.RotationLimitRestMinutes)*60
		case setting.RotationQuotaEnabled && quotaFull:
			if quotaUntil == 0 {
				quotaUntil = now + cpaRestSeconds(setting)
			}
			c.reason, c.until = "quota", quotaUntil
			c.dead = cpaQuotaExhausted(snap, now)
		}
		if c.reason == "" && setting.RotationDailyEnabled && dailyFull {
			c.reason, c.until = "daily", cpaNextClock(now, loc, 0)
		}
		if c.reason == "" && !held && tired {
			c.reason, c.until = "rotation", now+cpaRestSeconds(setting)
		}
		if c.reason != "" {
			p.candidates = append(p.candidates, c)
		}
	}

	percent := setting.RotationMaxRestPercent
	if night {
		percent = max(percent, setting.RotationNightMaxRestPercent)
	}
	instanceIDs := make([]int, 0, len(pools))
	for id := range pools {
		instanceIDs = append(instanceIDs, id)
	}
	sort.Ints(instanceIDs)
	for _, instanceID := range instanceIDs {
		p := pools[instanceID]
		// Busy wake-up: many 429s, or many requests per online account.
		busy := false
		if setting.RotationEnabled {
			if setting.RotationWake429Enabled && p.r429 >= int64(setting.RotationWake429Count) {
				busy = true
			}
			if setting.RotationWakeRpmEnabled && p.online > 0 && float64(p.req5)/5/float64(p.online) >= float64(setting.RotationWakeRpm) {
				busy = true
			}
		}
		if busy && len(p.wakeable) > 0 && now-in.LastWake[instanceID] >= int64(setting.RotationWakeCooldownMinutes)*60 {
			// Soonest back first: it was going to come back anyway.
			sort.SliceStable(p.wakeable, func(a, b int) bool { return p.wakeable[a].until < p.wakeable[b].until })
			for _, w := range p.wakeable[:min(setting.RotationWakeBatch, len(p.wakeable))] {
				st := &states[w.idx]
				hold := now + int64(setting.RotationWakeHoldMinutes)*60
				st.State, st.RestUntil, st.Reason, st.HoldUntil = "online", 0, "", hold
				p.online++
				p.resting--
				plan.Actions = append(plan.Actions, cpaRotationAction{InstanceID: instanceID, Name: st.Name, Kind: "wake", Reason: "busy", HoldUntil: hold})
			}
			plan.WakeAt[instanceID] = now
		}

		// MinOnline is always kept: when safety stops (SOCKS / 401 / 403) or
		// manual switches drop the pool below it, bring resting accounts
		// back in cpaRefillOrder, then the soonest back.
		if setting.RotationEnabled && p.online < setting.RotationMinOnline && len(p.refill) > 0 {
			sort.SliceStable(p.refill, func(a, b int) bool {
				ra, rb := cpaRefillOrder[p.refill[a].reason], cpaRefillOrder[p.refill[b].reason]
				if ra != rb {
					return ra < rb
				}
				return p.refill[a].until < p.refill[b].until
			})
			for _, w := range p.refill {
				if p.online >= setting.RotationMinOnline {
					break
				}
				st := &states[w.idx]
				if st.State != "resting" { // already woken above
					continue
				}
				st.State, st.RestUntil, st.Reason = "online", 0, ""
				p.online++
				p.resting--
				plan.Actions = append(plan.Actions, cpaRotationAction{InstanceID: instanceID, Name: st.Name, Kind: "wake", Reason: "min_online"})
			}
		}

		sort.SliceStable(p.candidates, func(a, b int) bool {
			ca, cb := p.candidates[a], p.candidates[b]
			if cpaRestPriority[ca.reason] != cpaRestPriority[cb.reason] {
				return cpaRestPriority[ca.reason] < cpaRestPriority[cb.reason]
			}
			sa, sb := states[ca.idx], states[cb.idx]
			if sa.WorkedHours != sb.WorkedHours {
				return sa.WorkedHours > sb.WorkedHours
			}
			return ca.lastReq < cb.lastReq
		})
		maxRest := max(1, (p.online+p.resting)*percent/100)
		// Safety gates (user decision 2026-11-12). Every reason obeys the
		// max-rest percentage; at most RotationBatchSize accounts go in one
		// step and two steps are RotationBatchGapMinutes apart; and nobody
		// goes when the accounts left could not carry the busiest minute
		// of the last hour or the pool was rate limited a moment ago.
		lastRest := max(p.lastRest, in.LastRest[instanceID])
		gapOpen := now-lastRest >= int64(setting.RotationBatchGapMinutes)*60
		info := CpaRotationPool{InstanceID: instanceID, OnlineBefore: p.before, Recent429: p.cap429}
		for _, l := range p.load {
			info.PeakRpm, info.PeakTpm = max(info.PeakRpm, l.served), max(info.PeakTpm, l.tokens)
		}
		capacityOK := func(c candidate) bool {
			if !setting.RotationCapacityEnabled || c.dead {
				return true // a used-up account carries nothing anyway
			}
			left := int64(p.online - 1)
			if left <= 0 {
				return false
			}
			if limit := int64(setting.RotationCapacityRpm); limit > 0 && info.PeakRpm > limit*left {
				return false
			}
			if limit := int64(setting.RotationCapacityTpm); limit > 0 && info.PeakTpm > limit*left {
				return false
			}
			// An account resting because it is rate limited itself does
			// not count its own 429s: that is what its rest is for.
			others := p.cap429
			if c.reason == "limit" || c.reason == "quota" {
				others -= c.own429
			}
			return setting.RotationCapacity429Minutes == 0 || others <= 0
		}
		rested, queue := 0, 0
		for _, c := range p.candidates {
			st := &states[c.idx]
			blocked := ""
			switch {
			case p.online-1 < setting.RotationMinOnline:
				blocked = "min_online"
			case p.resting >= maxRest:
				blocked = "percent"
			case busy && c.reason == "rotation":
				// While the pool is busy no account goes on plain rotation rest.
				blocked = "busy"
			case !gapOpen || rested >= setting.RotationBatchSize:
				blocked = "batch"
			case !capacityOK(c):
				blocked = "capacity"
			}
			if blocked != "" {
				queue++
				st.State, st.QueuePos, st.Reason, st.Blocked = "queued", queue, c.reason, blocked
				continue
			}
			rested++
			p.resting++
			p.online--
			action := cpaRotationAction{
				InstanceID: instanceID, Name: st.Name, Kind: "rest", RestUntil: c.until, WorkedHours: st.WorkedHours, Reason: c.reason,
			}
			if setting.RotationDryRun {
				// Dry run: the account stays online, the page only shows
				// that it would be resting now.
				st.WouldRest, st.Reason = true, c.reason
				plan.WouldRest = append(plan.WouldRest, action)
				continue
			}
			st.State, st.RestUntil, st.RestHours, st.Reason = "resting", c.until, st.WorkedHours, c.reason
			plan.Actions = append(plan.Actions, action)
			plan.RestAt[instanceID] = now
		}
		info.Online, info.Resting = p.online, p.resting
		plan.Summary.Pools = append(plan.Summary.Pools, info)
	}

	sum := &plan.Summary
	for _, st := range states {
		switch st.State {
		case "resting":
			sum.Resting++
			if sum.NextOnline == 0 || st.RestUntil < sum.NextOnline {
				sum.NextOnline = st.RestUntil
			}
		case "queued":
			sum.Queued++
			sum.Online++
		case "off":
			sum.Off++
		default:
			sum.Online++
		}
	}
	for id := range pools {
		if t := max(in.LastWake[id], plan.WakeAt[id]); t > sum.LastWakeAt {
			sum.LastWakeAt = t
		}
	}
	sum.WouldRest = len(plan.WouldRest)
	sum.Accounts = states
	return plan
}

var cpaRotationLast atomic.Pointer[CpaRotationSummary]

// GetCpaRotationSummary returns the view computed by the last loop run.
func GetCpaRotationSummary() *CpaRotationSummary {
	if s := cpaRotationLast.Load(); s != nil {
		return s
	}
	return &CpaRotationSummary{Accounts: []CpaRotationState{}}
}

// TriggerCpaRotation runs one loop soon, e.g. after a manual switch or a
// settings change, so the page does not wait for the next tick.
func TriggerCpaRotation() {
	gopool.Go(runCpaRotation)
}

func loadCpaActiveHours(now int64) map[string]map[int64]bool {
	out := map[string]map[int64]bool{}
	rows, err := model.ListCpaActiveHours(now - now%3600 - cpaRotationLookbackHours*3600)
	if err != nil {
		return out
	}
	for _, row := range rows {
		key := fmt.Sprintf("%d/%s", row.InstanceID, row.AuthIndex)
		if out[key] == nil {
			out[key] = map[int64]bool{}
		}
		out[key][row.BucketTs] = true
	}
	return out
}

var (
	cpaRotationWakeMu sync.Mutex
	cpaRotationWake   = map[int]int64{}
	// cpaRotationRest: instanceID -> last time an account went resting.
	cpaRotationRest = map[int]int64{}
	// cpaRotationDryLogged: dry-run rests already written to the log, so
	// the same account is not logged every 30 seconds.
	cpaRotationDryLogged = map[string]bool{}
)

func runCpaRotation() {
	if !cpaRotationMu.TryLock() {
		return
	}
	defer cpaRotationMu.Unlock()
	runCpaRotationLocked()
}

// cpaRotationInputAt reads everything the planner needs at moment now.
func cpaRotationInputAt(setting *model.CpaSubscriptionSetting, now int64) cpaRotationInput {
	cpaRotationWakeMu.Lock()
	lastWake := make(map[int]int64, len(cpaRotationWake))
	for k, v := range cpaRotationWake {
		lastWake[k] = v
	}
	lastRest := make(map[int]int64, len(cpaRotationRest))
	for k, v := range cpaRotationRest {
		lastRest[k] = v
	}
	cpaRotationWakeMu.Unlock()
	return cpaRotationInput{
		Active:   loadCpaActiveHours(now),
		Minutes:  loadCpaRecentMinutes(setting, now),
		Loc:      time.Local,
		LastWake: lastWake,
		LastRest: lastRest,
	}
}

// runCpaRotationLocked plans and applies one run; the caller holds
// cpaRotationMu. It returns how many accounts were brought back online and
// how many of those calls failed.
func runCpaRotationLocked() (woken, failedWakes int) {
	setting, err := model.GetCpaSubscriptionSetting()
	if err != nil {
		return 0, 0
	}
	snaps, err := ListCpaSnapshots()
	if err != nil {
		return 0, 0
	}
	now := cpaRotationNow().Unix()
	plan := planCpaRotation(setting, snaps, cpaRotationInputAt(setting, now), now)
	if len(plan.NewShifts) > 0 {
		saveCpaNewShifts(plan.NewShifts)
	}
	if len(plan.WakeAt) > 0 {
		cpaRotationWakeMu.Lock()
		for k, v := range plan.WakeAt {
			cpaRotationWake[k] = v
		}
		cpaRotationWakeMu.Unlock()
	}
	noteCpaRotationDryRun(plan.WouldRest)
	failed := map[string]bool{}
	for _, action := range plan.Actions {
		err := applyCpaRotationAction(action, now)
		if err != nil {
			failed[accountKey(action.InstanceID, action.Name)] = true
			common.SysLog(fmt.Sprintf("cpa rotation %s %s failed: %v", action.Kind, action.Name, err))
		}
		switch {
		case action.Kind == "wake" && err != nil:
			failedWakes++
		case action.Kind == "wake":
			woken++
		case action.Kind == "rest" && err == nil:
			cpaRotationWakeMu.Lock()
			cpaRotationRest[action.InstanceID] = now
			cpaRotationWakeMu.Unlock()
		}
	}
	if len(failed) > 0 {
		// Re-plan from the stored state so the page shows what really happened.
		if fresh, err := ListCpaSnapshots(); err == nil {
			for i := range plan.Summary.Accounts {
				st := &plan.Summary.Accounts[i]
				if failed[accountKey(st.InstanceID, st.Name)] {
					for j := range fresh {
						if fresh[j].InstanceID == st.InstanceID && fresh[j].Name == st.Name {
							if fresh[j].RestUntil > 0 {
								st.State, st.RestUntil = "resting", fresh[j].RestUntil
							} else {
								st.State, st.RestUntil = "online", 0
							}
						}
					}
				}
			}
		}
	}
	cpaRotationLast.Store(&plan.Summary)
	return woken, failedWakes
}

// noteCpaRotationDryRun writes each held-back rest to the log once.
func noteCpaRotationDryRun(would []cpaRotationAction) {
	cpaRotationWakeMu.Lock()
	defer cpaRotationWakeMu.Unlock()
	seen := make(map[string]bool, len(would))
	for _, a := range would {
		key := accountKey(a.InstanceID, a.Name) + "|" + a.Reason
		seen[key] = true
		if !cpaRotationDryLogged[key] {
			common.SysLog(fmt.Sprintf("cpa rotation dry run: %s (CPA %d) would go resting (%s), nothing was changed", a.Name, a.InstanceID, a.Reason))
		}
	}
	cpaRotationDryLogged = seen
}

// PreviewCpaRotation plans with the given (not saved) settings and applies
// nothing: who would go resting in the first step, who would queue and why,
// and the load of each CPA before and after.
func PreviewCpaRotation(setting *model.CpaSubscriptionSetting) (*CpaRotationSummary, error) {
	snaps, err := ListCpaSnapshots()
	if err != nil {
		return nil, err
	}
	now := cpaRotationNow().Unix()
	preview := *setting
	preview.Normalize()
	preview.RotationDryRun = false
	plan := planCpaRotation(&preview, snaps, cpaRotationInputAt(&preview, now), now)
	return &plan.Summary, nil
}

// StopCpaRotationNow is the emergency button: rotation is switched off and
// every account it had sent resting is enabled again right away. Accounts
// stopped by the SOCKS guard, a 401/403 or by hand are never touched.
func StopCpaRotationNow() (woken, failed int, err error) {
	setting, err := model.GetCpaSubscriptionSetting()
	if err != nil {
		return 0, 0, err
	}
	setting.RotationEnabled = false
	if err := model.SaveCpaSubscriptionSetting(setting); err != nil {
		return 0, 0, err
	}
	cpaRotationMu.Lock()
	defer cpaRotationMu.Unlock()
	woken, failed = runCpaRotationLocked()
	return woken, failed, nil
}

// loadCpaRecentMinutes reads the minute rows the rules look at.
func loadCpaRecentMinutes(setting *model.CpaSubscriptionSetting, now int64) map[string][]model.CpaUsageMinute {
	out := map[string][]model.CpaUsageMinute{}
	// 61 minutes: the capacity check reads the busiest minute of the last hour.
	span := int64(max(setting.RotationWake429Minutes, setting.RotationLimitMinutes, setting.RotationCapacity429Minutes, 61)) * 60
	rows, err := model.ListCpaUsageMinutes(now-now%60-span, now+60)
	if err != nil {
		return out
	}
	for _, row := range rows {
		key := fmt.Sprintf("%d/%s", row.InstanceID, row.AuthIndex)
		out[key] = append(out[key], row)
	}
	return out
}

// saveCpaNewShifts stores the alternate shift given to new accounts so it
// stays put and can be changed by hand on the page.
func saveCpaNewShifts(add map[string]string) {
	setting, err := model.GetCpaSubscriptionSetting()
	if err != nil {
		return
	}
	current := setting.RotationShiftMap()
	for k, v := range add {
		if _, ok := current[k]; !ok {
			current[k] = v
		}
	}
	setting.RotationShifts = formatCpaShifts(current)
	if err := model.SaveCpaSubscriptionSetting(setting); err != nil {
		common.SysError("cpa rotation: save shifts failed: " + err.Error())
	}
}

func formatCpaShifts(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+"="+m[k])
	}
	return strings.Join(lines, "\n")
}

// SetCpaAccountShift changes one account's shift (day | night | all).
func SetCpaAccountShift(instanceID int, name, shift string) error {
	if shift != "day" && shift != "night" && shift != "all" {
		return fmt.Errorf("unknown shift %q", shift)
	}
	setting, err := model.GetCpaSubscriptionSetting()
	if err != nil {
		return err
	}
	m := setting.RotationShiftMap()
	m[accountKey(instanceID, name)] = shift
	setting.RotationShifts = formatCpaShifts(m)
	if err := model.SaveCpaSubscriptionSetting(setting); err != nil {
		return err
	}
	TriggerCpaRotation()
	return nil
}

// updateCpaRotationMark edits the rotation fields of one snapshot under the
// account lock. fn returns false to skip saving.
func updateCpaRotationMark(instanceID int, name string, fn func(snap *CpaAccountSnapshot) bool) (bool, error) {
	unlock := lockAccount(instanceID, name)
	defer unlock()
	snap, err := loadSnapshot(instanceID, name)
	if err != nil {
		return false, err
	}
	if !fn(snap) {
		return false, nil
	}
	return true, saveSnapshot(snap)
}

func applyCpaRotationAction(action cpaRotationAction, now int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), cpaRotationTimeout)
	defer cancel()
	switch action.Kind {
	case "rest":
		// Mark first: if the disable call then fails, the next run sees an
		// enabled account with a mark and simply drops the mark.
		if _, err := updateCpaRotationMark(action.InstanceID, action.Name, func(s *CpaAccountSnapshot) bool {
			s.RestStartedAt, s.RestUntil, s.RestWorkedHours, s.RestReason = now, action.RestUntil, action.WorkedHours, action.Reason
			s.RotationHoldUntil = 0
			return true
		}); err != nil {
			return err
		}
		if err := SetCpaAccountDisabled(ctx, action.InstanceID, action.Name, true); err != nil {
			_, _ = updateCpaRotationMark(action.InstanceID, action.Name, func(s *CpaAccountSnapshot) bool {
				s.RestStartedAt, s.RestUntil, s.RestWorkedHours, s.RestReason = 0, 0, 0, ""
				return true
			})
			return err
		}
		recordCpaRotationAudit("cpa_subscription.rotation_rest", action, map[string]any{
			"worked_hours": action.WorkedHours, "rest_until": action.RestUntil, "reason": action.Reason,
		})
	case "wake":
		// Re-check under the lock: only an account still carrying the mark
		// and not taken over by a safety stop is enabled again.
		still := false
		_, _ = updateCpaRotationMark(action.InstanceID, action.Name, func(s *CpaAccountSnapshot) bool {
			still = s.RestUntil > 0 && s.AutoDisabledAt == 0 && s.AuthDisabledAt == 0
			return false
		})
		if !still {
			return nil
		}
		if err := SetCpaAccountDisabled(ctx, action.InstanceID, action.Name, false); err != nil {
			return err
		}
		_, err := updateCpaRotationMark(action.InstanceID, action.Name, func(s *CpaAccountSnapshot) bool {
			s.RestStartedAt, s.RestUntil, s.RestWorkedHours, s.RestReason = 0, 0, 0, ""
			s.RotationWorkSince = now
			s.RotationHoldUntil = action.HoldUntil
			return true
		})
		recordCpaRotationAudit("cpa_subscription.rotation_wake", action, map[string]any{"reason": action.Reason})
		return err
	case "clear":
		_, err := updateCpaRotationMark(action.InstanceID, action.Name, func(s *CpaAccountSnapshot) bool {
			if s.RestUntil == 0 && s.AuthDisabledAt == 0 {
				return false
			}
			s.RestStartedAt, s.RestUntil, s.RestWorkedHours, s.RestReason = 0, 0, 0, ""
			if action.Reason == "enabled_elsewhere" {
				s.AuthDisabledAt = 0
				s.RotationWorkSince = now
			}
			return true
		})
		return err
	}
	return nil
}

// ClearCpaRotationOnManualSwitch is called after the root user flips the
// switch. Enabling a resting account = "back online early": the mark goes,
// the work streak restarts and the account is held online for the busy
// hold time so a shift rest does not take it straight back. Disabling by
// hand also drops the mark, so rotation never re-enables an account the
// user turned off. Either way a 401/403 stop is considered handled.
func ClearCpaRotationOnManualSwitch(instanceID int, name string, disabled bool) {
	now := cpaRotationNow().Unix()
	hold := int64(0)
	if setting, err := model.GetCpaSubscriptionSetting(); err == nil {
		hold = now + int64(setting.RotationWakeHoldMinutes)*60
	}
	_, _ = updateCpaRotationMark(instanceID, name, func(s *CpaAccountSnapshot) bool {
		changed := s.RestUntil > 0 || s.AuthDisabledAt > 0
		s.RestStartedAt, s.RestUntil, s.RestWorkedHours, s.RestReason = 0, 0, 0, ""
		s.AuthDisabledAt = 0
		if !disabled {
			s.RotationWorkSince = now
			s.RotationHoldUntil = hold
			changed = true
		}
		return changed
	})
	TriggerCpaRotation()
}

var cpaRotationReasonText = map[string]string{
	"rotation":     "连续工作到时长，轮休",
	"shift":        "排班下班",
	"limit":        "频繁被限流(429)，先休息",
	"quota":        "额度快用完，休息到重置",
	"daily":        "今天工作时长到上限",
	"busy":         "号池忙，提前叫醒",
	"min_online":   "在线数低于下限，补位上线",
	"rest_over":    "休息结束",
	"shift_on":     "排班上班",
	"rotation_off": "轮休已关闭",
}

func recordCpaRotationAudit(action string, a cpaRotationAction, extra map[string]any) {
	params := map[string]any{"instance_id": a.InstanceID, "name": a.Name}
	for k, v := range extra {
		params[k] = v
	}
	content := fmt.Sprintf("Rotation: subscription account %s (CPA %d) went resting (%s)", a.Name, a.InstanceID, a.Reason)
	notify := fmt.Sprintf("下线：%s", cpaRotationReasonText[a.Reason])
	if a.RestUntil > 0 {
		notify += "，预计 " + time.Unix(a.RestUntil, 0).Format("01-02 15:04") + " 上线"
	}
	if action == "cpa_subscription.rotation_wake" {
		content = fmt.Sprintf("Rotation: subscription account %s (CPA %d) back online (%s)", a.Name, a.InstanceID, a.Reason)
		notify = fmt.Sprintf("上线：%s", cpaRotationReasonText[a.Reason])
	}
	model.RecordAuditLog(nil, model.AuditLog{
		Username:  "system",
		ActorRole: common.RoleRootUser,
		Category:  model.AuditCategoryOperation,
		Action:    action,
		Content:   content,
		Status:    200,
		Success:   true,
		Other:     model.AuditOther{Op: &model.AuditOperation{Action: action, Params: params}},
	})
	common.SysLog(content)
	NotifyCpaEvent(CpaNotifyRotation, a.InstanceID, a.Name, notify)
}
