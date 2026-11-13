package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
)

func rotationSetting() *model.CpaSubscriptionSetting {
	s := &model.CpaSubscriptionSetting{}
	s.Normalize()
	s.RotationEnabled = true
	s.RotationJitterMinutes = 0
	return s
}

// rotationSettingOpen switches the safety gates off (any number per step,
// no gap, no capacity check) for tests of the older rules.
func rotationSettingOpen() *model.CpaSubscriptionSetting {
	s := rotationSetting()
	s.RotationBatchSize, s.RotationBatchGapMinutes = 100, 0
	s.RotationCapacityEnabled = false
	return s
}

func rotationSnap(i int, disabled bool) CpaAccountSnapshot {
	return CpaAccountSnapshot{
		InstanceID: 1, Name: fmt.Sprintf("a%d.json", i), IsClaude: true,
		Credential: map[string]any{"auth_index": fmt.Sprintf("idx%d", i), "disabled": disabled},
	}
}

// busy marks the last n hours (including the current one) as active.
func busy(active map[string]map[int64]bool, i int, now int64, n int) {
	key := fmt.Sprintf("1/idx%d", i)
	active[key] = map[int64]bool{}
	hour := now - now%3600
	for k := 0; k < n; k++ {
		active[key][hour-int64(k)*3600] = true
	}
}

const rotNow = int64(1_800_000_000) // fixed, mid-hour

func TestCpaWorkedHours(t *testing.T) {
	active := map[string]map[int64]bool{}
	busy(active, 1, rotNow, 5)
	if got := cpaWorkedHours(active["1/idx1"], rotNow, 0); got != 5 {
		t.Fatalf("worked = %d, want 5", got)
	}
	// Current hour without requests yet does not break the streak.
	delete(active["1/idx1"], rotNow-rotNow%3600)
	if got := cpaWorkedHours(active["1/idx1"], rotNow, 0); got != 4 {
		t.Fatalf("worked = %d, want 4", got)
	}
	// Hours before workSince do not count.
	if got := cpaWorkedHours(active["1/idx1"], rotNow, rotNow-rotNow%3600-3600); got != 1 {
		t.Fatalf("worked since = %d, want 1", got)
	}
}

func TestPlanCpaRotationLimits(t *testing.T) {
	s := rotationSettingOpen() // 6h work, 30%, min online 2
	snaps := []CpaAccountSnapshot{}
	active := map[string]map[int64]bool{}
	for i := 1; i <= 10; i++ {
		snaps = append(snaps, rotationSnap(i, false))
		busy(active, i, rotNow, 7) // everyone is tired
	}
	plan := planCpaRotation(s, snaps, cpaRotationInput{Active: active, Loc: time.UTC}, rotNow)
	rests := 0
	for _, a := range plan.Actions {
		if a.Kind == "rest" {
			rests++
		}
	}
	if rests != 3 { // 30% of 10
		t.Fatalf("rests = %d, want 3", rests)
	}
	if plan.Summary.Resting != 3 || plan.Summary.Queued != 7 {
		t.Fatalf("summary = %+v", plan.Summary)
	}
}

func TestPlanCpaRotationMinOnline(t *testing.T) {
	s := rotationSetting()
	s.RotationMaxRestPercent = 90
	snaps := []CpaAccountSnapshot{rotationSnap(1, false), rotationSnap(2, false), rotationSnap(3, false)}
	active := map[string]map[int64]bool{}
	for i := 1; i <= 3; i++ {
		busy(active, i, rotNow, 8)
	}
	plan := planCpaRotation(s, snaps, cpaRotationInput{Active: active, Loc: time.UTC}, rotNow)
	if plan.Summary.Resting != 1 { // 3 online, keep 2
		t.Fatalf("resting = %d, want 1", plan.Summary.Resting)
	}
}

func TestPlanCpaRotationWakeAndGuards(t *testing.T) {
	s := rotationSetting()
	s.RotationMinOnline = 1 // refill is covered by TestPlanCpaRotationRefillMinOnline
	due := rotationSnap(1, true)
	due.RestUntil = rotNow - 1
	resting := rotationSnap(2, true)
	resting.RestUntil = rotNow + 600
	socks := rotationSnap(3, true)
	socks.RestUntil = rotNow - 1
	socks.AutoDisabledAt = rotNow - 100
	manual := rotationSnap(4, true) // disabled by hand, no mark
	plan := planCpaRotation(s, []CpaAccountSnapshot{due, resting, socks, manual}, cpaRotationInput{Active: map[string]map[int64]bool{}, Loc: time.UTC}, rotNow)
	kinds := map[string]string{}
	for _, a := range plan.Actions {
		kinds[a.Name] = a.Kind
	}
	if kinds["a1.json"] != "wake" {
		t.Fatalf("due account: %q", kinds["a1.json"])
	}
	if _, ok := kinds["a2.json"]; ok {
		t.Fatal("resting account must not change")
	}
	if kinds["a3.json"] != "clear" {
		t.Fatalf("socks account must only lose the mark: %q", kinds["a3.json"])
	}
	if _, ok := kinds["a4.json"]; ok {
		t.Fatal("manually disabled account must not be touched")
	}
}

func TestPlanCpaRotationExcludedAndLimited(t *testing.T) {
	s := rotationSetting()
	s.RotationMaxRestPercent = 90
	s.RotationMinOnline = 1
	s.RotationExcluded = "1/a1.json"
	limited := rotationSnap(2, false)
	limited.Credential["quota"] = map[string]any{"signals": map[string]any{
		"Anthropic-Ratelimit-Unified-7d-Status": "rejected",
		"Anthropic-Ratelimit-Unified-7d-Reset":  fmt.Sprint(rotNow + 3600),
	}}
	snaps := []CpaAccountSnapshot{rotationSnap(1, false), limited, rotationSnap(3, false), rotationSnap(4, false)}
	active := map[string]map[int64]bool{}
	for i := 1; i <= 4; i++ {
		busy(active, i, rotNow, 9)
	}
	plan := planCpaRotation(s, snaps, cpaRotationInput{Active: active, Loc: time.UTC}, rotNow)
	for _, a := range plan.Actions {
		if a.Name == "a1.json" || a.Name == "a2.json" {
			t.Fatalf("%s must not rest", a.Name)
		}
	}
}

func TestPlanCpaRotationDisabledWakesAll(t *testing.T) {
	s := rotationSetting()
	s.RotationEnabled = false
	r := rotationSnap(1, true)
	r.RestUntil = rotNow + 3600
	plan := planCpaRotation(s, []CpaAccountSnapshot{r}, cpaRotationInput{Active: map[string]map[int64]bool{}, Loc: time.UTC}, rotNow)
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != "wake" {
		t.Fatalf("turning rotation off must wake resting accounts: %+v", plan.Actions)
	}
}

func actionsByName(plan cpaRotationPlan) map[string]cpaRotationAction {
	out := map[string]cpaRotationAction{}
	for _, a := range plan.Actions {
		out[a.Name] = a
	}
	return out
}

// rotAt returns a fixed UTC moment at hh:mm on 2027-01-15.
func rotAt(hh, mm int) int64 {
	return time.Date(2027, 1, 15, hh, mm, 30, 0, time.UTC).Unix()
}

func TestCpaShiftDuty(t *testing.T) {
	s := rotationSetting()
	s.RotationShiftJitterMinutes = 0
	cases := []struct {
		shift string
		hh    int
		on    bool
	}{
		{"day", 9, true}, {"day", 1, true}, {"day", 3, false}, {"day", 7, false},
		{"night", 21, true}, {"night", 13, true}, {"night", 15, false}, {"night", 19, false},
		{"all", 4, true},
	}
	for _, c := range cases {
		on, _, _ := cpaShiftDuty(s, c.shift, "1/x", rotAt(c.hh, 0), time.UTC)
		if on != c.on {
			t.Fatalf("%s at %02d:00 on=%v, want %v", c.shift, c.hh, on, c.on)
		}
	}
	// Jitter is stable within a day and bounded.
	s.RotationShiftJitterMinutes = 30
	for _, key := range []string{"1/a", "1/b", "2/c"} {
		j := cpaJitter(key, "2027-01-15", "on", 30)
		if j < -30 || j > 30 || j != cpaJitter(key, "2027-01-15", "on", 30) {
			t.Fatalf("jitter %d", j)
		}
	}
}

func TestPlanCpaRotationShifts(t *testing.T) {
	s := rotationSettingOpen()
	s.RotationShiftEnabled = true
	s.RotationShiftJitterMinutes = 0
	s.RotationMinOnline = 1
	s.RotationMaxRestPercent = 90 // the percentage caps shift rests too
	snaps := []CpaAccountSnapshot{rotationSnap(1, false), rotationSnap(2, false), rotationSnap(3, false), rotationSnap(4, false)}
	plan := planCpaRotation(s, snaps, cpaRotationInput{Loc: time.UTC}, rotAt(4, 0))
	if len(plan.NewShifts) != 4 {
		t.Fatalf("new shifts = %v", plan.NewShifts)
	}
	day := 0
	for _, v := range plan.NewShifts {
		if v == "day" {
			day++
		}
	}
	if day != 2 {
		t.Fatalf("alternate assignment: %v", plan.NewShifts)
	}
	// 04:00: day shift is off duty -> both day accounts rest for "shift".
	rests := 0
	for _, a := range plan.Actions {
		if a.Kind == "rest" {
			if a.Reason != "shift" || plan.NewShifts["1/"+a.Name] != "day" {
				t.Fatalf("unexpected rest %+v", a)
			}
			rests++
		}
	}
	if rests != 2 {
		t.Fatalf("shift rests = %d, want 2", rests)
	}
	// A resting day account wakes when the day shift starts.
	s.RotationShifts = "1/a1.json=day"
	r := rotationSnap(1, true)
	r.RestUntil, r.RestReason = rotAt(8, 0), "shift"
	plan = planCpaRotation(s, []CpaAccountSnapshot{r}, cpaRotationInput{Loc: time.UTC}, rotAt(8, 5))
	if a := actionsByName(plan)["a1.json"]; a.Kind != "wake" || a.Reason != "shift_on" {
		t.Fatalf("shift wake: %+v", a)
	}
}

func TestPlanCpaRotationShiftKeepsMinOnline(t *testing.T) {
	s := rotationSetting()
	s.RotationShiftEnabled = true
	s.RotationShiftJitterMinutes = 0
	s.RotationShifts = "1/a1.json=day\n1/a2.json=day\n1/a3.json=day"
	snaps := []CpaAccountSnapshot{rotationSnap(1, false), rotationSnap(2, false), rotationSnap(3, false)}
	plan := planCpaRotation(s, snaps, cpaRotationInput{Loc: time.UTC}, rotAt(4, 0))
	if plan.Summary.Resting != 1 || plan.Summary.Queued != 2 { // keep 2 online
		t.Fatalf("summary %+v", plan.Summary)
	}
}

func limitRows(i int, now int64, n429 int64) []model.CpaUsageMinute {
	return []model.CpaUsageMinute{{InstanceID: 1, AuthIndex: fmt.Sprintf("idx%d", i), BucketTs: now - now%60 - 60, Requests: n429, Status429: n429}}
}

func TestPlanCpaRotationLimitAndQuota(t *testing.T) {
	s := rotationSettingOpen()
	s.RotationLimitEnabled, s.RotationQuotaEnabled = true, true
	s.RotationMinOnline = 1
	s.RotationMaxRestPercent = 90
	quota := rotationSnap(2, false)
	reset := rotNow + 7200
	quota.Usage = map[string]any{"five_hour": map[string]any{"utilization": 95.0, "resets_at": time.Unix(reset, 0).UTC().Format(time.RFC3339)}}
	snaps := []CpaAccountSnapshot{rotationSnap(1, false), quota, rotationSnap(3, false), rotationSnap(4, false)}
	in := cpaRotationInput{Loc: time.UTC, Minutes: map[string][]model.CpaUsageMinute{"1/idx1": limitRows(1, rotNow, 12)}}
	acts := actionsByName(planCpaRotation(s, snaps, in, rotNow))
	if a := acts["a1.json"]; a.Kind != "rest" || a.Reason != "limit" || a.RestUntil != rotNow+30*60 {
		t.Fatalf("limit rest: %+v", a)
	}
	if a := acts["a2.json"]; a.Kind != "rest" || a.Reason != "quota" || a.RestUntil != reset {
		t.Fatalf("quota rest: %+v", a)
	}
	if _, ok := acts["a3.json"]; ok {
		t.Fatal("healthy account must stay")
	}
}

func TestPlanCpaRotationNightPercent(t *testing.T) {
	s := rotationSettingOpen()
	s.RotationNightEnabled = true
	snaps := []CpaAccountSnapshot{}
	active := map[string]map[int64]bool{}
	now := rotAt(3, 0)
	for i := 1; i <= 10; i++ {
		snaps = append(snaps, rotationSnap(i, false))
		busy(active, i, now, 7)
	}
	plan := planCpaRotation(s, snaps, cpaRotationInput{Active: active, Loc: time.UTC}, now)
	if !plan.Summary.Night || plan.Summary.Resting != 6 { // 60% at night
		t.Fatalf("night summary %+v", plan.Summary)
	}
	noon := rotAt(12, 0)
	for i := 1; i <= 10; i++ {
		busy(active, i, noon, 7)
	}
	plan = planCpaRotation(s, snaps, cpaRotationInput{Active: active, Loc: time.UTC}, noon)
	if plan.Summary.Night || plan.Summary.Resting != 3 {
		t.Fatalf("day summary %+v", plan.Summary)
	}
}

func TestPlanCpaRotationBusyWake(t *testing.T) {
	s := rotationSetting()
	s.RotationWake429Enabled = true
	s.RotationWakeBatch = 1
	r1 := rotationSnap(1, true)
	r1.RestUntil, r1.RestReason = rotNow+3000, "rotation"
	r2 := rotationSnap(2, true)
	r2.RestUntil, r2.RestReason = rotNow+600, "rotation"
	snaps := []CpaAccountSnapshot{r1, r2, rotationSnap(3, false), rotationSnap(4, false)}
	in := cpaRotationInput{Loc: time.UTC, Minutes: map[string][]model.CpaUsageMinute{"1/idx3": limitRows(3, rotNow, 25)}}
	plan := planCpaRotation(s, snaps, in, rotNow)
	acts := actionsByName(plan)
	if a := acts["a2.json"]; a.Kind != "wake" || a.Reason != "busy" || a.HoldUntil != rotNow+30*60 {
		t.Fatalf("soonest resting account must wake: %+v", a)
	}
	if _, ok := acts["a1.json"]; ok {
		t.Fatal("batch is 1")
	}
	// Cooldown: no second wake-up right after.
	in.LastWake = map[int]int64{1: rotNow - 60}
	if a := actionsByName(planCpaRotation(s, snaps, in, rotNow))["a2.json"]; a.Kind == "wake" {
		t.Fatal("cooldown ignored")
	}
}

func TestPlanCpaRotationAuthDisabledUntouched(t *testing.T) {
	s := rotationSetting()
	a := rotationSnap(1, true)
	a.AuthDisabledAt = rotNow - 10
	plan := planCpaRotation(s, []CpaAccountSnapshot{a}, cpaRotationInput{Loc: time.UTC}, rotNow)
	if len(plan.Actions) != 0 || !plan.Summary.Accounts[0].AuthDisabled {
		t.Fatalf("401/403 account: %+v", plan)
	}
}

func TestPlanCpaRotationRefillMinOnline(t *testing.T) {
	s := rotationSetting() // min online 2
	r1 := rotationSnap(1, true)
	r1.RestUntil, r1.RestReason = rotNow+3000, "rotation"
	r2 := rotationSnap(2, true)
	r2.RestUntil, r2.RestReason = rotNow+600, "limit"
	r3 := rotationSnap(3, true) // 92% used: still has quota left
	r3.RestUntil, r3.RestReason = rotNow+9000, "quota"
	r3.Usage = map[string]any{"five_hour": map[string]any{"utilization": 92.0}}
	full := rotationSnap(6, true) // 100% used: cannot serve
	full.RestUntil, full.RestReason = rotNow+600, "quota"
	full.Usage = map[string]any{"five_hour": map[string]any{"utilization": 100.0}}
	gone := rotationSnap(4, true)
	gone.AuthDisabledAt = rotNow - 10 // 401: never refilled
	online := rotationSnap(5, false)

	plan := planCpaRotation(s, []CpaAccountSnapshot{r1, r2, r3, full, gone, online}, cpaRotationInput{Loc: time.UTC}, rotNow)
	acts := actionsByName(plan)
	if a := acts["a3.json"]; a.Kind != "wake" || a.Reason != "min_online" {
		t.Fatalf("almost-full quota rest must come back first: %+v", a)
	}
	for _, name := range []string{"a1.json", "a2.json"} {
		if _, ok := acts[name]; ok {
			t.Fatalf("only one account was missing, %s must stay", name)
		}
	}
	if _, ok := acts["a6.json"]; ok {
		t.Fatal("a used-up quota is never refilled")
	}
	if _, ok := acts["a4.json"]; ok {
		t.Fatal("401/403 stop must stay")
	}
	if plan.Summary.Online != 2 {
		t.Fatalf("summary %+v", plan.Summary)
	}

	// Without a usable quota rest, plain rotation is next.
	plan = planCpaRotation(s, []CpaAccountSnapshot{r1, r2, full, gone, online}, cpaRotationInput{Loc: time.UTC}, rotNow)
	acts = actionsByName(plan)
	if a := acts["a1.json"]; a.Kind != "wake" || a.Reason != "min_online" {
		t.Fatalf("plain rotation rest must be cut next: %+v", a)
	}
	if _, ok := acts["a6.json"]; ok {
		t.Fatal("a used-up quota is never refilled")
	}
}

func TestSignDingtalkWebhook(t *testing.T) {
	got, err := signDingtalkWebhook("https://oapi.dingtalk.com/robot/send?access_token=abc", "SECxyz", 1700000000000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "access_token=abc") || !strings.Contains(got, "timestamp=1700000000000") || !strings.Contains(got, "sign=") {
		t.Fatalf("signed url %s", got)
	}
}

func TestAggregateCpaMinutes(t *testing.T) {
	ts := time.Unix(rotNow, 0)
	recs := []cpaUsageQueueRecord{
		{Timestamp: ts, AuthIndex: "i1"},
		{Timestamp: ts, AuthIndex: "i1", Failed: true},
		{Timestamp: ts.Add(time.Minute), AuthIndex: "i1"},
	}
	recs[1].Fail.StatusCode = 429
	rows := aggregateCpaMinutes(1, recs)
	if len(rows) != 2 {
		t.Fatalf("rows %d", len(rows))
	}
	row := rows[cpaMinuteKey{authIndex: "i1", bucket: rotNow - rotNow%60}]
	if row.Requests != 2 || row.Status429 != 1 || row.Failed != 1 {
		t.Fatalf("row %+v", row)
	}
}

// ---- safety gates (user decision 2026-11-12) ----

func countRests(plan cpaRotationPlan) int {
	n := 0
	for _, a := range plan.Actions {
		if a.Kind == "rest" {
			n++
		}
	}
	return n
}

func tiredPool(n int, now int64) ([]CpaAccountSnapshot, map[string]map[int64]bool) {
	snaps := []CpaAccountSnapshot{}
	active := map[string]map[int64]bool{}
	for i := 1; i <= n; i++ {
		snaps = append(snaps, rotationSnap(i, false))
		busy(active, i, now, 8)
	}
	return snaps, active
}

// loadRow is one minute of account i, ago minutes before now.
func loadRow(i int, now, ago, requests, tokens, r429 int64) model.CpaUsageMinute {
	return model.CpaUsageMinute{
		InstanceID: 1, AuthIndex: fmt.Sprintf("idx%d", i), BucketTs: now - now%60 - ago*60,
		Requests: requests, Tokens: tokens, Status429: r429, Failed: r429,
	}
}

func TestPlanCpaRotationOneAccountPerStep(t *testing.T) {
	s := rotationSetting() // defaults: 1 account per step, 5 minutes apart
	snaps, active := tiredPool(10, rotNow)
	plan := planCpaRotation(s, snaps, cpaRotationInput{Active: active, Loc: time.UTC}, rotNow)
	if countRests(plan) != 1 || plan.Summary.Resting != 1 || plan.Summary.Queued != 9 {
		t.Fatalf("first step must take one account: rests=%d summary=%+v", countRests(plan), plan.Summary)
	}
	if plan.RestAt[1] != rotNow {
		t.Fatalf("rest time not reported: %v", plan.RestAt)
	}
	for _, st := range plan.Summary.Accounts {
		if st.State == "queued" && st.Blocked != "batch" {
			t.Fatalf("%s blocked by %q, want batch", st.Name, st.Blocked)
		}
	}
	// Inside the gap nobody else goes, whether the last rest is known from
	// memory or from the resting account itself (after a restart).
	plan = planCpaRotation(s, snaps, cpaRotationInput{Active: active, Loc: time.UTC, LastRest: map[int]int64{1: rotNow - 60}}, rotNow)
	if countRests(plan) != 0 {
		t.Fatalf("gap ignored: %d rests", countRests(plan))
	}
	resting := rotationSnap(11, true)
	resting.RestUntil, resting.RestReason, resting.RestStartedAt = rotNow+3600, "rotation", rotNow-60
	plan = planCpaRotation(s, append([]CpaAccountSnapshot{resting}, snaps...), cpaRotationInput{Active: active, Loc: time.UTC}, rotNow)
	if countRests(plan) != 0 {
		t.Fatalf("gap after restart ignored: %d rests", countRests(plan))
	}
	// After the gap the next one goes.
	plan = planCpaRotation(s, snaps, cpaRotationInput{Active: active, Loc: time.UTC, LastRest: map[int]int64{1: rotNow - 600}}, rotNow)
	if countRests(plan) != 1 {
		t.Fatalf("after the gap: %d rests", countRests(plan))
	}
}

func TestPlanCpaRotationCountsWorkFromSwitchOn(t *testing.T) {
	s := rotationSettingOpen()
	s.RotationEnabledAt = rotNow - 30 // just switched on
	snaps, active := tiredPool(10, rotNow)
	plan := planCpaRotation(s, snaps, cpaRotationInput{Active: active, Loc: time.UTC}, rotNow)
	if len(plan.Actions) != 0 || plan.Summary.Queued != 0 {
		t.Fatalf("work before the switch must not count: %+v", plan.Actions)
	}
	for _, st := range plan.Summary.Accounts {
		if st.WorkedHours != 0 {
			t.Fatalf("%s worked %d h right after switch on", st.Name, st.WorkedHours)
		}
	}
	// Six hours later the same accounts are due.
	later := rotNow + 6*3600
	_, active = tiredPool(10, later)
	if plan = planCpaRotation(s, snaps, cpaRotationInput{Active: active, Loc: time.UTC}, later); countRests(plan) != 3 {
		t.Fatalf("after 6 h: %d rests, want 3", countRests(plan))
	}
}

func TestPlanCpaRotationCapacity(t *testing.T) {
	s := rotationSetting()
	s.RotationMinOnline = 1
	s.RotationCapacityRpm = 8
	snaps, active := tiredPool(4, rotNow)
	plan := func(rows ...model.CpaUsageMinute) cpaRotationPlan {
		minutes := map[string][]model.CpaUsageMinute{}
		for _, r := range rows {
			key := fmt.Sprintf("1/%s", r.AuthIndex)
			minutes[key] = append(minutes[key], r)
		}
		return planCpaRotation(s, snaps, cpaRotationInput{Active: active, Minutes: minutes, Loc: time.UTC}, rotNow)
	}
	// Busiest minute 30 requests: 3 accounts left x 8 = 24 is not enough.
	p := plan(loadRow(1, rotNow, 20, 15, 0, 0), loadRow(2, rotNow, 20, 15, 0, 0))
	if countRests(p) != 0 || p.Summary.Queued != 4 {
		t.Fatalf("capacity ignored: %+v", p.Summary)
	}
	for _, st := range p.Summary.Accounts {
		if st.Blocked != "capacity" {
			t.Fatalf("%s blocked by %q, want capacity", st.Name, st.Blocked)
		}
	}
	if pool := p.Summary.Pools[0]; pool.PeakRpm != 30 || pool.OnlineBefore != 4 || pool.Online != 4 {
		t.Fatalf("pool %+v", pool)
	}
	// 20 requests fit into 24.
	if p = plan(loadRow(1, rotNow, 20, 10, 0, 0), loadRow(2, rotNow, 20, 10, 0, 0)); countRests(p) != 1 {
		t.Fatalf("enough capacity: %d rests", countRests(p))
	}
	// 429s do not count as load (they were turned away) ...
	if p = plan(loadRow(1, rotNow, 20, 40, 0, 30)); countRests(p) != 1 {
		t.Fatalf("old 429s must not block: %d rests", countRests(p))
	}
	// ... but a 429 in the last 5 minutes means the pool is short right now.
	if p = plan(loadRow(1, rotNow, 2, 5, 0, 1)); countRests(p) != 0 {
		t.Fatalf("recent 429 must block: %d rests", countRests(p))
	}
	// Tokens: 3 accounts x 1000 < 5000.
	s.RotationCapacityTpm = 1000
	if p = plan(loadRow(1, rotNow, 20, 1, 5000, 0)); countRests(p) != 0 {
		t.Fatalf("token capacity ignored: %d rests", countRests(p))
	}
	// Switched off: the old behaviour.
	s.RotationCapacityEnabled = false
	if p = plan(loadRow(1, rotNow, 20, 1, 5000, 0), loadRow(1, rotNow, 2, 5, 0, 1)); countRests(p) != 1 {
		t.Fatalf("capacity check off: %d rests", countRests(p))
	}
}

func TestPlanCpaRotationPercentCapsEveryReason(t *testing.T) {
	s := rotationSettingOpen()
	s.RotationShiftEnabled = true
	s.RotationShiftJitterMinutes = 0
	lines := []string{}
	snaps := []CpaAccountSnapshot{}
	for i := 1; i <= 10; i++ {
		snaps = append(snaps, rotationSnap(i, false))
		lines = append(lines, fmt.Sprintf("1/a%d.json=day", i))
	}
	s.RotationShifts = strings.Join(lines, "\n")
	// 04:00: the whole day shift is off duty, but only 30% may rest.
	plan := planCpaRotation(s, snaps, cpaRotationInput{Loc: time.UTC}, rotAt(4, 0))
	if countRests(plan) != 3 || plan.Summary.Queued != 7 {
		t.Fatalf("shift rests = %d, summary %+v", countRests(plan), plan.Summary)
	}
	if plan.Summary.Hits["shift"] != 10 {
		t.Fatalf("hits %v", plan.Summary.Hits)
	}
}

func TestPlanCpaRotationDryRun(t *testing.T) {
	s := rotationSetting()
	s.RotationDryRun = true
	snaps, active := tiredPool(10, rotNow)
	plan := planCpaRotation(s, snaps, cpaRotationInput{Active: active, Loc: time.UTC}, rotNow)
	if len(plan.Actions) != 0 {
		t.Fatalf("dry run must not change anything: %+v", plan.Actions)
	}
	if len(plan.WouldRest) != 1 || !plan.Summary.DryRun || plan.Summary.WouldRest != 1 || plan.Summary.Resting != 0 {
		t.Fatalf("dry run summary %+v would=%v", plan.Summary, plan.WouldRest)
	}
	would := 0
	for _, st := range plan.Summary.Accounts {
		if st.WouldRest {
			would++
			if st.State != "online" || st.Reason != "rotation" {
				t.Fatalf("dry run state %+v", st)
			}
		}
	}
	if would != 1 || plan.Summary.Hits["rotation"] != 10 {
		t.Fatalf("would=%d hits=%v", would, plan.Summary.Hits)
	}
	// A resting account still comes back when its rest is over.
	due := rotationSnap(11, true)
	due.RestUntil = rotNow - 1
	plan = planCpaRotation(s, []CpaAccountSnapshot{due}, cpaRotationInput{Loc: time.UTC}, rotNow)
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != "wake" {
		t.Fatalf("dry run must still wake: %+v", plan.Actions)
	}
}

func TestPlanCpaRotationHitsWhileOff(t *testing.T) {
	s := rotationSetting()
	s.RotationEnabled = false
	snaps, active := tiredPool(4, rotNow)
	plan := planCpaRotation(s, snaps, cpaRotationInput{Active: active, Loc: time.UTC}, rotNow)
	if len(plan.Actions) != 0 || plan.Summary.Hits["rotation"] != 4 {
		t.Fatalf("hits are counted while rotation is off: %v %+v", plan.Summary.Hits, plan.Actions)
	}
}
