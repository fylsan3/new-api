package service

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// Official monthly prices (USD) per plan, used as the account cost (user
// decision). Plans not listed (team, generic "max", free) have no cost.
var cpaPlanMonthlyUSD = map[string]float64{
	"plan_pro":   20,
	"plan_max5":  100,
	"plan_max20": 200,
}

const cpaDaysPerMonth = 30.0

type CpaAccountStats struct {
	InstanceID       int      `json:"instance_id"`
	Name             string   `json:"name"`
	AuthIndex        string   `json:"auth_index"`
	Email            string   `json:"email,omitempty"`
	PlanType         string   `json:"plan_type,omitempty"`
	Disabled         bool     `json:"disabled"`
	Requests         int64    `json:"requests"`
	Failed           int64    `json:"failed"`
	Tokens           int64    `json:"tokens"`
	InputTokens      int64    `json:"input_tokens"`
	OutputTokens     int64    `json:"output_tokens"`
	CacheReadTokens  int64    `json:"cache_read_tokens"`
	CacheWriteTokens int64    `json:"cache_write_tokens"`
	AvgLatencyMs     int64    `json:"avg_latency_ms"`
	CostUSD          float64  `json:"cost_usd"`
	UnpricedRequests int64    `json:"unpriced_requests"`
	RevenueUSD       float64  `json:"revenue_usd"`
	PlanCostUSD      float64  `json:"plan_cost_usd"`
	PlanCostKnown    bool     `json:"plan_cost_known"`
	ProxyCostUSD     float64  `json:"proxy_cost_usd"`
	FiveHour         *float64 `json:"five_hour,omitempty"`
	SevenDay         *float64 `json:"seven_day,omitempty"`
}

type CpaDailyPoint struct {
	Day        int64   `json:"day"`
	Requests   int64   `json:"requests"`
	Failed     int64   `json:"failed"`
	Tokens     int64   `json:"tokens"`
	CostUSD    float64 `json:"cost_usd"`
	RevenueUSD float64 `json:"revenue_usd"`
}

type CpaInstanceRevenue struct {
	InstanceID int     `json:"instance_id"`
	ChannelIDs []int   `json:"channel_ids"`
	RevenueUSD float64 `json:"revenue_usd"`
	Requests   int64   `json:"requests"`
}

type CpaStatsReport struct {
	From        int64                 `json:"from"`
	To          int64                 `json:"to"`
	Days        int                   `json:"days"`
	Accounts    []CpaAccountStats     `json:"accounts"`
	Daily       []CpaDailyPoint       `json:"daily"`
	Instances   []CpaInstanceRevenue  `json:"instances"`
	QuotaPoints []model.CpaQuotaPoint `json:"quota_points"`
	Estimated   bool                  `json:"estimated"`
}

// localDayStart returns the server-local midnight of ts, so "today" on the
// dashboard matches the operator's calendar day instead of UTC.
func localDayStart(ts int64) int64 {
	t := time.Unix(ts, 0).In(time.Local)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local).Unix()
}

func usageKey(instanceID int, authIndex string) string {
	return strconv.Itoa(instanceID) + "/" + authIndex
}

func parseChannelIDs(raw string) []int {
	ids := make([]int, 0)
	for _, part := range strings.Split(raw, ",") {
		if id, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func hostOf(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Host)
}

// cpaChannelMap resolves which New API channels feed each CPA: explicit
// ChannelIDs first, otherwise channels whose base_url host equals the CPA host.
func cpaChannelMap(instances []model.CpaInstance) map[int][]int {
	out := make(map[int][]int, len(instances))
	var refs []model.CpaChannelRef
	loaded := false
	for _, instance := range instances {
		if ids := parseChannelIDs(instance.ChannelIDs); len(ids) > 0 {
			out[instance.ID] = ids
			continue
		}
		if !loaded {
			refs, _ = model.ListCpaChannelRefs()
			loaded = true
		}
		host := hostOf(instance.BaseURL)
		for _, ref := range refs {
			if ref.BaseURL != nil && host != "" && hostOf(*ref.BaseURL) == host {
				out[instance.ID] = append(out[instance.ID], ref.ID)
			}
		}
	}
	return out
}

// BuildCpaStats aggregates the last `days` days (1-90).
func BuildCpaStats(days int) (*CpaStatsReport, error) {
	days = min(max(days, 1), 90)
	now := time.Now()
	to := now.Unix() - now.Unix()%3600 + 3600
	from := to - int64(days)*86400

	instances, err := model.ListCpaInstances()
	if err != nil {
		return nil, err
	}
	snaps, err := ListCpaSnapshots()
	if err != nil {
		return nil, err
	}
	rows, err := model.ListCpaUsageHourly(from, to)
	if err != nil {
		return nil, err
	}
	setting, err := model.GetCpaSubscriptionSetting()
	if err != nil {
		return nil, err
	}

	report := &CpaStatsReport{From: from, To: to, Days: days, Estimated: true}
	periodMonths := float64(days) / cpaDaysPerMonth

	byKey := map[string]*CpaAccountStats{}
	for _, snap := range snaps {
		if !snap.IsClaude {
			continue
		}
		authIndex := credentialString(snap.Credential, "auth_index")
		stats := &CpaAccountStats{
			InstanceID: snap.InstanceID, Name: snap.Name, AuthIndex: authIndex,
			Email: credentialString(snap.Credential, "email"), PlanType: snap.PlanType,
			Disabled:     credentialBool(snap.Credential, "disabled"),
			ProxyCostUSD: setting.ProxyMonthlyCostUSD * periodMonths,
		}
		if price, ok := cpaPlanMonthlyUSD[snap.PlanType]; ok {
			stats.PlanCostUSD = price * periodMonths
			stats.PlanCostKnown = true
		}
		stats.FiveHour = usageWindowPercent(snap.Usage, "five_hour")
		stats.SevenDay = usageWindowPercent(snap.Usage, "seven_day")
		byKey[usageKey(snap.InstanceID, authIndex)] = stats
	}

	instanceHourTokens := map[int]map[int64]int64{} // instance -> hour -> tokens
	accountHourTokens := map[string]map[int64]int64{}
	daily := map[int64]*CpaDailyPoint{}
	latency := map[string]int64{}
	for i := range rows {
		row := &rows[i]
		key := usageKey(row.InstanceID, row.AuthIndex)
		stats := byKey[key]
		if stats == nil {
			stats = &CpaAccountStats{InstanceID: row.InstanceID, AuthIndex: row.AuthIndex, Name: "auth_index " + row.AuthIndex}
			byKey[key] = stats
		}
		tokens := row.TotalTokens()
		stats.Requests += row.Requests
		stats.Failed += row.Failed
		stats.Tokens += tokens
		stats.InputTokens += row.InputTokens
		stats.OutputTokens += row.OutputTokens
		stats.CacheReadTokens += row.CacheReadTokens
		stats.CacheWriteTokens += row.CacheCreationTokens
		stats.CostUSD += row.CostUSD
		stats.UnpricedRequests += row.UnpricedRequests
		latency[key] += row.LatencySumMs

		if instanceHourTokens[row.InstanceID] == nil {
			instanceHourTokens[row.InstanceID] = map[int64]int64{}
		}
		instanceHourTokens[row.InstanceID][row.BucketTs] += tokens
		if accountHourTokens[key] == nil {
			accountHourTokens[key] = map[int64]int64{}
		}
		accountHourTokens[key][row.BucketTs] += tokens

		day := localDayStart(row.BucketTs)
		point := daily[day]
		if point == nil {
			point = &CpaDailyPoint{Day: day}
			daily[day] = point
		}
		point.Requests += row.Requests
		point.Failed += row.Failed
		point.Tokens += tokens
		point.CostUSD += row.CostUSD
	}

	// Revenue: what New API charged on the channels of each CPA, allocated
	// to accounts by their token share within the same hour.
	channelMap := cpaChannelMap(instances)
	for _, instance := range instances {
		ids := channelMap[instance.ID]
		entry := CpaInstanceRevenue{InstanceID: instance.ID, ChannelIDs: ids}
		revenueRows, revErr := model.ListCpaChannelRevenue(ids, from, to)
		if revErr != nil {
			return nil, revErr
		}
		hourRevenue := map[int64]float64{}
		for _, rev := range revenueRows {
			usd := float64(rev.Quota) / common.QuotaPerUnit
			entry.RevenueUSD += usd
			entry.Requests += rev.Count
			hourRevenue[rev.CreatedAt-rev.CreatedAt%3600] += usd
		}
		report.Instances = append(report.Instances, entry)
		for hour, usd := range hourRevenue {
			day := localDayStart(hour)
			if daily[day] == nil {
				daily[day] = &CpaDailyPoint{Day: day}
			}
			daily[day].RevenueUSD += usd
			total := instanceHourTokens[instance.ID][hour]
			if total <= 0 {
				continue
			}
			for key, hours := range accountHourTokens {
				stats := byKey[key]
				if stats == nil || stats.InstanceID != instance.ID || hours[hour] == 0 {
					continue
				}
				stats.RevenueUSD += usd * float64(hours[hour]) / float64(total)
			}
		}
	}

	for key, stats := range byKey {
		if stats.Requests > 0 {
			stats.AvgLatencyMs = latency[key] / stats.Requests
		}
		report.Accounts = append(report.Accounts, *stats)
	}
	sort.Slice(report.Accounts, func(i, j int) bool {
		a, b := report.Accounts[i], report.Accounts[j]
		if a.Tokens != b.Tokens {
			return a.Tokens > b.Tokens
		}
		return a.Name < b.Name
	})
	for _, point := range daily {
		report.Daily = append(report.Daily, *point)
	}
	sort.Slice(report.Daily, func(i, j int) bool { return report.Daily[i].Day < report.Daily[j].Day })

	pointsFrom := max(from, now.Add(-cpaQuotaRetention).Unix())
	if points, pointsErr := model.ListCpaQuotaPoints(pointsFrom); pointsErr == nil {
		report.QuotaPoints = points
	}
	if report.Accounts == nil {
		report.Accounts = []CpaAccountStats{}
	}
	if report.Daily == nil {
		report.Daily = []CpaDailyPoint{}
	}
	if report.QuotaPoints == nil {
		report.QuotaPoints = []model.CpaQuotaPoint{}
	}
	return report, nil
}

// CpaCardUsage is the per-account usage shown on cards (last 30 days).
type CpaCardUsage struct {
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	CostUSD  float64 `json:"cost_usd"`
}

// ListCpaCardUsage returns last-30-day usage keyed by "instance_id/auth_index".
func ListCpaCardUsage() (map[string]CpaCardUsage, error) {
	now := time.Now().Unix()
	rows, err := model.ListCpaUsageHourly(now-30*86400, now+3600)
	if err != nil {
		return nil, err
	}
	out := map[string]CpaCardUsage{}
	for i := range rows {
		key := usageKey(rows[i].InstanceID, rows[i].AuthIndex)
		entry := out[key]
		entry.Requests += rows[i].Requests
		entry.Tokens += rows[i].TotalTokens()
		entry.CostUSD += rows[i].CostUSD
		out[key] = entry
	}
	return out, nil
}
