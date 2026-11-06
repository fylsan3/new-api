package service

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

// Daily random refresh (user decision 2026-11-06, design draft section 2+3):
// quota now arrives passively with every chat (CPA credential "quota"), so
// the background only keeps plan type and reset grants fresh. Every account
// gets 1 or 2 (50/50) random moments per local day, at least 4 hours apart,
// anywhere in 0-24h. Only the profile and cedar_ember requests are sent, both
// through CPA with the account's own SOCKS proxy. Disabled accounts, accounts
// without a valid SOCKS proxy and direct (no proxy_url) test accounts are
// skipped. Slots missed while New API was down are not made up.
const (
	cpaDailyRefreshTick      = 30 * time.Second
	cpaDailyRefreshMinGap    = 4 * time.Hour
	cpaDailyRefreshMissGrace = 10 * time.Minute
	cpaDailyRefreshTimeout   = 2 * time.Minute
	cpaDailyRefreshSpread    = 20 * time.Second
)

var (
	cpaDailyRefreshOnce sync.Once
	cpaDailyRefreshBusy sync.Map // accountKey -> struct{}
)

func startCpaDailyRefresh() {
	cpaDailyRefreshOnce.Do(func() {
		gopool.Go(func() {
			ticker := time.NewTicker(cpaDailyRefreshTick)
			defer ticker.Stop()
			for range ticker.C {
				runCpaDailyRefresh(time.Now())
			}
		})
	})
}

func runCpaDailyRefresh(now time.Time) {
	if cpaRefreshAllBusy.Load() {
		return
	}
	snaps, err := ListCpaSnapshots()
	if err != nil {
		return
	}
	due := make([]CpaAccountSnapshot, 0)
	for i := range snaps {
		if !snaps[i].IsClaude || snaps[i].Credential == nil {
			continue
		}
		if planCpaDailyRefresh(snaps[i].InstanceID, snaps[i].Name, now) {
			due = append(due, snaps[i])
		}
	}
	delays := staggerDelays(len(due), cpaDailyRefreshSpread)
	for i := range due {
		instanceID, name, delay := due[i].InstanceID, due[i].Name, delays[i]
		key := accountKey(instanceID, name)
		if _, running := cpaDailyRefreshBusy.LoadOrStore(key, struct{}{}); running {
			continue
		}
		gopool.Go(func() {
			defer cpaDailyRefreshBusy.Delete(key)
			time.Sleep(delay)
			ctx, cancel := context.WithTimeout(context.Background(), cpaDailyRefreshTimeout)
			defer cancel()
			if err := RefreshCpaPlanAndGrants(ctx, instanceID, name); err != nil {
				common.SysLog(fmt.Sprintf("cpa subscription daily refresh %s failed: %v", name, err))
			}
		})
	}
}

// planCpaDailyRefresh makes sure today's random plan exists, drops slots that
// were missed (no make-up) and reports whether a slot is due now.
func planCpaDailyRefresh(instanceID int, name string, now time.Time) bool {
	unlock := lockAccount(instanceID, name)
	defer unlock()
	snap, err := loadSnapshot(instanceID, name)
	if err != nil || snap.Credential == nil {
		return false
	}
	changed := false
	day := now.Format("20060102")
	if snap.AutoRefreshDay != day {
		snap.AutoRefreshDay = day
		snap.AutoRefreshPlan = drawCpaDailyPlan(now)
		snap.AutoRefreshDone = 0
		changed = true
	}
	nowSec := now.Unix()
	for snap.AutoRefreshDone < len(snap.AutoRefreshPlan) &&
		snap.AutoRefreshPlan[snap.AutoRefreshDone] < nowSec-int64(cpaDailyRefreshMissGrace/time.Second) {
		snap.AutoRefreshDone++
		changed = true
	}
	if changed {
		if err := saveSnapshot(snap); err != nil {
			return false
		}
	}
	if credentialBool(snap.Credential, "disabled") {
		return false
	}
	return snap.AutoRefreshDone < len(snap.AutoRefreshPlan) &&
		snap.AutoRefreshPlan[snap.AutoRefreshDone] <= nowSec
}

// drawCpaDailyPlan picks 1 or 2 (50/50) random unix-second moments between
// now and the end of the local day, at least cpaDailyRefreshMinGap apart.
// When less than the gap is left today only one slot is drawn.
func drawCpaDailyPlan(now time.Time) []int64 {
	y, m, d := now.Date()
	end := time.Date(y, m, d+1, 0, 0, 0, 0, now.Location())
	window := end.Sub(now)
	if window <= time.Minute {
		return nil
	}
	pick := func() int64 {
		return now.Add(time.Duration(rand.Int64N(int64(window)))).Unix()
	}
	if rand.IntN(2) == 0 || window <= cpaDailyRefreshMinGap {
		return []int64{pick()}
	}
	gap := int64(cpaDailyRefreshMinGap / time.Second)
	for range 200 {
		a, b := pick(), pick()
		if a > b {
			a, b = b, a
		}
		if b-a >= gap {
			return []int64{a, b}
		}
	}
	return []int64{pick()}
}

// RefreshCpaPlanAndGrants refreshes only the plan type (profile) and reset
// grants (cedar_ember) of one account. It never queries quota and never
// sends anything without a working SOCKS proxy. The slot is counted as done
// whatever the outcome, so a failure is not retried until the next slot.
func RefreshCpaPlanAndGrants(ctx context.Context, instanceID int, name string) error {
	instance, err := model.GetCpaInstance(instanceID)
	if err != nil {
		return err
	}
	client, err := newCpaClient(instance)
	if err != nil {
		return err
	}
	unlock := lockAccount(instanceID, name)
	defer unlock()
	snap, err := loadSnapshot(instanceID, name)
	if err != nil {
		return err
	}
	if snap.Credential == nil {
		return ErrCpaAccountNotFound
	}
	if snap.AutoRefreshDone < len(snap.AutoRefreshPlan) {
		snap.AutoRefreshDone++
	}
	snap.AutoRefreshLastAt = common.GetTimestamp()
	authIndex := credentialString(snap.Credential, "auth_index")
	if !snap.IsClaude || credentialBool(snap.Credential, "disabled") || authIndex == "" {
		return saveSnapshot(snap)
	}
	guard, proxyErr := client.requireSocksProxy(ctx, name)
	if proxyErr != nil {
		// No proxy_url, or an invalid one: skip, never go out directly.
		snap.AutoRefreshError = proxyErr.Error()
		return saveSnapshot(snap)
	}
	snap.ProxyURL = MaskProxyURL(guard.proxyURL)

	profile, _, profileErr := fetchClaudeJSON(ctx, client, guard, authIndex, claudeProfileURL, false)
	trackCpaProxyHealth(snap, profileErr)
	if profileErr != nil {
		snap.AutoRefreshError = profileErr.Error()
		return saveSnapshot(snap)
	}
	if plan := resolveClaudePlanType(profile); plan != "" {
		snap.PlanType = plan
	}
	grants, _, grantsErr := fetchClaudeJSON(ctx, client, guard, authIndex, claudeAPIOrigin+"/api/oauth/usage?cedar_ember=1&skip_spend=1", false)
	if grantsErr != nil {
		snap.ResetGrantsError = grantsErr.Error()
		snap.AutoRefreshError = grantsErr.Error()
	} else {
		if block, ok := grants["cedar_ember"].(map[string]any); ok {
			snap.ResetGrants = block
		} else {
			snap.ResetGrants = nil
		}
		snap.ResetGrantsError = ""
		snap.AutoRefreshError = ""
	}
	return saveSnapshot(snap)
}
