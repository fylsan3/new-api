package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// Per-minute usage (user decision 2026-11-12): RPM / TPM on the cards (last
// full minute + 5-minute average), charts of 1 h / 12 h / 24 h / 7 d, and
// the 429 counts the busy wake-up and the per-account limit rest read.
//
// Tokens = input + output, without cache read / cache write: the usage log
// of New API stores Claude requests the same way (prompt_tokens is the
// uncached input), so the TPM here and the TPM of the usage log agree.

const cpaMinuteRetention = 7*24*time.Hour + time.Hour

type cpaMinuteKey struct {
	authIndex string
	bucket    int64
}

func aggregateCpaMinutes(instanceID int, records []cpaUsageQueueRecord) map[cpaMinuteKey]*model.CpaUsageMinute {
	rows := map[cpaMinuteKey]*model.CpaUsageMinute{}
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
		key := cpaMinuteKey{authIndex: authIndex, bucket: ts - ts%60}
		row := rows[key]
		if row == nil {
			row = &model.CpaUsageMinute{InstanceID: instanceID, AuthIndex: authIndex, BucketTs: key.bucket}
			rows[key] = row
		}
		c := rec.counts()
		row.Requests++
		row.Tokens += c.Input + c.Output
		if rec.Failed {
			row.Failed++
			switch rec.Fail.StatusCode {
			case 429:
				row.Status429++
			case 401:
				row.Status401++
			case 403:
				row.Status403++
			}
		}
	}
	return rows
}

// CpaRate is the RPM / TPM view of one account.
type CpaRate struct {
	Rpm1 int64   `json:"rpm_1m"`
	Tpm1 int64   `json:"tpm_1m"`
	Rpm5 float64 `json:"rpm_5m"`
	Tpm5 float64 `json:"tpm_5m"`
	Peak int64   `json:"rpm_peak_1h"`
	// TpmPeak: tokens of the busiest minute of the last hour.
	TpmPeak int64 `json:"tpm_peak_1h"`
	Err1h   int64 `json:"r429_1h"`
}

// cpaServedRequests is what the peak counts: requests the account really
// took. A 429 was turned away, so a rate-limited account hammered with
// retries must not look like its busiest minute (user decision 2026-11-12).
func cpaServedRequests(row *model.CpaUsageMinute) int64 {
	return max(row.Requests-row.Status429, 0)
}

// ListCpaRates returns rates keyed by "instance_id/auth_index" and the
// total of the whole pool. "Last minute" is the last complete minute so
// the number does not jump to 0 at the start of each minute. Peaks leave
// out 429s; the pool peak is the busiest minute of all accounts together,
// not the sum of each account's own peak (those are different minutes).
func ListCpaRates(now int64) (map[string]CpaRate, CpaRate, error) {
	minute := now - now%60
	rows, err := model.ListCpaUsageMinutes(minute-3600, minute)
	if err != nil {
		return nil, CpaRate{}, err
	}
	out, total := summarizeCpaRates(rows, minute)
	return out, total, nil
}

// summarizeCpaRates is the pure part of ListCpaRates; minute is the start
// of the current (incomplete) minute.
func summarizeCpaRates(rows []model.CpaUsageMinute, minute int64) (map[string]CpaRate, CpaRate) {
	out := map[string]CpaRate{}
	var total CpaRate
	poolMinutes := map[int64]int64{}
	poolTokens := map[int64]int64{}
	for i := range rows {
		row := &rows[i]
		key := usageKey(row.InstanceID, row.AuthIndex)
		rate := out[key]
		if row.BucketTs == minute-60 {
			rate.Rpm1, rate.Tpm1 = row.Requests, row.Tokens
			total.Rpm1 += row.Requests
			total.Tpm1 += row.Tokens
		}
		if row.BucketTs >= minute-300 {
			rate.Rpm5 += float64(row.Requests) / 5
			rate.Tpm5 += float64(row.Tokens) / 5
			total.Rpm5 += float64(row.Requests) / 5
			total.Tpm5 += float64(row.Tokens) / 5
		}
		served := cpaServedRequests(row)
		rate.Peak = max(rate.Peak, served)
		rate.TpmPeak = max(rate.TpmPeak, row.Tokens)
		poolTokens[row.BucketTs] += row.Tokens
		rate.Err1h += row.Status429
		total.Err1h += row.Status429
		poolMinutes[row.BucketTs] += served
		out[key] = rate
	}
	for _, served := range poolMinutes {
		total.Peak = max(total.Peak, served)
	}
	for _, tokens := range poolTokens {
		total.TpmPeak = max(total.TpmPeak, tokens)
	}
	return out, total
}

// CpaRatePoint is one chart bar. Requests / Tokens are the per-minute
// average of the bar; PeakRequests / PeakTokens are its busiest single
// minute (requests without 429s), so wide bars do not hide spikes.
type CpaRatePoint struct {
	Ts           int64 `json:"ts"`
	Requests     int64 `json:"requests"`
	Tokens       int64 `json:"tokens"`
	Failed       int64 `json:"failed"`
	R429         int64 `json:"r429"`
	PeakRequests int64 `json:"peak_requests"`
	PeakTokens   int64 `json:"peak_tokens"`
	// PeakFailed: failed requests other than 429 inside the busiest minute
	// (the part of PeakRequests that did not succeed).
	PeakFailed int64 `json:"peak_failed"`
}

// CpaRateSeries is the chart data of one account or the whole pool.
type CpaRateSeries struct {
	Range  string         `json:"range"`
	Step   int64          `json:"step"`
	Points []CpaRatePoint `json:"points"`
}

var cpaRateRanges = map[string][2]int64{
	"1h":  {3600, 60},
	"12h": {12 * 3600, 600},
	"24h": {24 * 3600, 1800},
	"7d":  {7 * 24 * 3600, 2 * 3600},
}

// BuildCpaRateSeries buckets minute rows into chart bars. An empty
// authIndex with instanceID 0 means every account. Values are per-minute
// averages of each bar so the three ranges read the same (RPM / TPM).
func BuildCpaRateSeries(instanceID int, name, rangeKey string, now int64) (*CpaRateSeries, error) {
	spec, ok := cpaRateRanges[rangeKey]
	if !ok {
		rangeKey, spec = "1h", cpaRateRanges["1h"]
	}
	span, step := spec[0], spec[1]
	end := now - now%step + step
	from := end - span
	var rows []model.CpaUsageMinute
	var err error
	if instanceID > 0 && name != "" {
		snap, loadErr := loadSnapshot(instanceID, name)
		if loadErr != nil {
			return nil, loadErr
		}
		rows, err = model.ListCpaUsageMinutesOf(instanceID, credentialString(snap.Credential, "auth_index"), from, end)
	} else {
		// One row per minute, summed in SQL: 7 days of every account would
		// be far too many rows to load.
		rows, err = model.ListCpaUsageMinuteTotals(from, end)
	}
	if err != nil {
		return nil, err
	}
	return &CpaRateSeries{Range: rangeKey, Step: step, Points: bucketCpaRatePoints(rows, from, span, step)}, nil
}

// bucketCpaRatePoints is the pure part of BuildCpaRateSeries.
func bucketCpaRatePoints(rows []model.CpaUsageMinute, from, span, step int64) []CpaRatePoint {
	n := span / step
	points := make([]CpaRatePoint, n)
	for i := range points {
		points[i].Ts = from + int64(i)*step
	}
	// Per-minute totals of every selected account, for the peaks.
	type minuteTotal struct{ served, tokens, failed int64 }
	minutes := map[int64]minuteTotal{}
	for i := range rows {
		idx := (rows[i].BucketTs - from) / step
		if idx < 0 || idx >= n {
			continue
		}
		p := &points[idx]
		p.Requests += rows[i].Requests
		p.Tokens += rows[i].Tokens
		p.Failed += rows[i].Failed
		p.R429 += rows[i].Status429
		m := minutes[rows[i].BucketTs]
		m.served += cpaServedRequests(&rows[i])
		m.tokens += rows[i].Tokens
		m.failed += max(rows[i].Failed-rows[i].Status429, 0)
		minutes[rows[i].BucketTs] = m
	}
	for ts, m := range minutes {
		p := &points[(ts-from)/step]
		if m.served > p.PeakRequests || (m.served == p.PeakRequests && m.failed > p.PeakFailed) {
			p.PeakRequests, p.PeakFailed = m.served, min(m.failed, m.served)
		}
		p.PeakTokens = max(p.PeakTokens, m.tokens)
	}
	if per := step / 60; per > 1 {
		for i := range points {
			points[i].Requests = (points[i].Requests + per/2) / per
			points[i].Tokens = (points[i].Tokens + per/2) / per
		}
	}
	return points
}

// ---- 401 / 403: stop the account and alert (switch AuthStopEnabled) ----

const CpaAlertKindAuthDisabled = "auth_auto_disabled"

// handleCpaAuthFailures disables accounts whose relay requests came back
// 401 or 403 (login expired / banned). Like the SOCKS guard the account is
// never re-enabled automatically; the alert waits for the root user.
func handleCpaAuthFailures(instanceID int, records []cpaUsageQueueRecord) {
	hits := map[string]int{}
	for i := range records {
		rec := &records[i]
		if rec.Failed && (rec.Fail.StatusCode == 401 || rec.Fail.StatusCode == 403) {
			if idx := strings.TrimSpace(rec.AuthIndex); idx != "" {
				hits[idx] = rec.Fail.StatusCode
			}
		}
	}
	if len(hits) == 0 {
		return
	}
	setting, err := model.GetCpaSubscriptionSetting()
	if err != nil || !setting.AuthStopEnabled {
		return
	}
	snaps, err := ListCpaSnapshots()
	if err != nil {
		return
	}
	for i := range snaps {
		snap := &snaps[i]
		status, ok := hits[credentialString(snap.Credential, "auth_index")]
		if snap.InstanceID != instanceID || !ok || snap.AuthDisabledAt > 0 {
			continue
		}
		name := snap.Name
		_, _ = updateCpaRotationMark(instanceID, name, func(s *CpaAccountSnapshot) bool {
			s.RestStartedAt, s.RestUntil, s.RestWorkedHours, s.RestReason = 0, 0, 0, ""
			s.AuthDisabledAt = common.GetTimestamp()
			return true
		})
		cpaAuthDisableAccount(instanceID, name, status)
	}
}

var cpaAuthDisableAccount = func(instanceID int, name string, status int) {
	go func() {
		ctx, cancel := cpaBackgroundContext()
		defer cancel()
		msg := fmt.Sprintf("Upstream returned %d (login expired or account banned); account disabled in CPA", status)
		if err := SetCpaAccountDisabled(ctx, instanceID, name, true); err != nil {
			msg = fmt.Sprintf("Upstream returned %d; auto-disable failed: %s", status, truncateText(err.Error(), 200))
		}
		_ = model.RaiseCpaAlert(instanceID, name, CpaAlertKindAuthDisabled, msg)
		common.SysLog(fmt.Sprintf("cpa subscription %s: %s", name, msg))
		NotifyCpaEvent(CpaNotifyAuth, instanceID, name, msg)
	}()
}

// ---- CPA unreachable / collection errors ----

var cpaCollectFailing = map[int]bool{}

// noteCpaCollectHealth alerts once when collection starts failing and once
// when it recovers, not on every 5-second poll.
func noteCpaCollectHealth(instance *model.CpaInstance, err error) {
	cpaNotifyMu.Lock()
	was := cpaCollectFailing[instance.ID]
	cpaCollectFailing[instance.ID] = err != nil
	cpaNotifyMu.Unlock()
	switch {
	case err != nil && !was:
		NotifyCpaEvent(CpaNotifyCpa, instance.ID, "", fmt.Sprintf("CPA %s: usage collection failed: %s", instance.Name, truncateText(err.Error(), 300)))
	case err == nil && was:
		NotifyCpaEvent(CpaNotifyCpa, instance.ID, "", fmt.Sprintf("CPA %s: usage collection recovered", instance.Name))
	}
}
