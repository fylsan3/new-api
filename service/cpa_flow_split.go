package service

import (
	"sort"

	"github.com/QuantumNous/new-api/model"
)

// CpaChannelIDs returns every New API channel that forwards to a registered
// CPA (explicit ChannelIDs or host match). The result is never nil, so an
// empty slice means "no CPA channels" rather than "no filter".
func CpaChannelIDs() ([]int, error) {
	instances, err := model.ListCpaInstances()
	if err != nil {
		return nil, err
	}
	seen := map[int]struct{}{}
	ids := make([]int, 0)
	for _, list := range cpaChannelMap(instances) {
		for _, id := range list {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids, nil
}

// cpaAccountShare is one account's slice of a CPA's traffic.
type cpaAccountShare struct {
	label  string
	tokens int64
}

// SplitFlowRowsByCpaAccount adds the "CPA account" layer to root flow rows.
//
// New API only knows which channel served a request, not which CPA account
// CPA picked, so rows on a CPA channel are split across that CPA's accounts by
// each account's token share of the same model in [startTime, endTime]
// (falling back to the account's share of all models). The split is an
// estimate; rows on other channels get CpaAccount "" and are left untouched.
func SplitFlowRowsByCpaAccount(rows []*model.FlowQuotaData, startTime, endTime int64) ([]*model.FlowQuotaData, error) {
	if len(rows) == 0 {
		return rows, nil
	}
	instances, err := model.ListCpaInstances()
	if err != nil || len(instances) == 0 {
		return rows, err
	}
	channelInstance := map[int]int{}
	for instanceID, ids := range cpaChannelMap(instances) {
		for _, id := range ids {
			// A channel mapped to several CPAs keeps the first one; the
			// mapping is configured per CPA and overlaps are a setup error.
			if _, ok := channelInstance[id]; !ok {
				channelInstance[id] = instanceID
			}
		}
	}
	if len(channelInstance) == 0 {
		return rows, nil
	}

	// cpa_usage_hourly buckets are hour-aligned; widen to whole hours.
	from := startTime - startTime%3600
	to := endTime - endTime%3600 + 3600
	usage, err := model.ListCpaUsageHourly(from, to)
	if err != nil {
		return rows, err
	}
	labels := cpaAccountLabels()
	byModel := map[int]map[string]map[string]int64{} // instance -> model -> label -> tokens
	byInstance := map[int]map[string]int64{}         // instance -> label -> tokens
	for i := range usage {
		row := &usage[i]
		tokens := row.TotalTokens()
		if tokens <= 0 {
			continue
		}
		label := labels[usageKey(row.InstanceID, row.AuthIndex)]
		if label == "" {
			label = "auth_index " + row.AuthIndex
		}
		if byModel[row.InstanceID] == nil {
			byModel[row.InstanceID] = map[string]map[string]int64{}
		}
		if byModel[row.InstanceID][row.ModelName] == nil {
			byModel[row.InstanceID][row.ModelName] = map[string]int64{}
		}
		byModel[row.InstanceID][row.ModelName][label] += tokens
		if byInstance[row.InstanceID] == nil {
			byInstance[row.InstanceID] = map[string]int64{}
		}
		byInstance[row.InstanceID][label] += tokens
	}

	out := make([]*model.FlowQuotaData, 0, len(rows))
	for _, row := range rows {
		instanceID, onCpa := channelInstance[row.ChannelID]
		if !onCpa {
			out = append(out, row)
			continue
		}
		shares := sortedShares(byModel[instanceID][row.ModelName])
		if len(shares) == 0 {
			shares = sortedShares(byInstance[instanceID])
		}
		if len(shares) == 0 {
			// CPA channel without collected usage (collection off or no data).
			clone := *row
			clone.CpaAccount = CpaAccountUnknown
			out = append(out, &clone)
			continue
		}
		out = append(out, splitFlowRow(row, shares)...)
	}
	return out, nil
}

// CpaAccountUnknown marks CPA traffic whose account cannot be estimated.
const CpaAccountUnknown = "__unknown__"

// cpaAccountLabels maps "instance/auth_index" to the account's display name.
func cpaAccountLabels() map[string]string {
	out := map[string]string{}
	snaps, err := ListCpaSnapshots()
	if err != nil {
		return out
	}
	for _, snap := range snaps {
		authIndex := credentialString(snap.Credential, "auth_index")
		if authIndex == "" {
			continue
		}
		label := credentialString(snap.Credential, "email")
		if label == "" {
			label = snap.Name
		}
		out[usageKey(snap.InstanceID, authIndex)] = label
	}
	return out
}

func sortedShares(tokens map[string]int64) []cpaAccountShare {
	shares := make([]cpaAccountShare, 0, len(tokens))
	for label, value := range tokens {
		if value > 0 {
			shares = append(shares, cpaAccountShare{label: label, tokens: value})
		}
	}
	sort.Slice(shares, func(i, j int) bool {
		if shares[i].tokens != shares[j].tokens {
			return shares[i].tokens > shares[j].tokens
		}
		return shares[i].label < shares[j].label
	})
	return shares
}

// splitFlowRow divides one row's integer metrics by the shares. The largest
// share absorbs the rounding remainder so the parts always sum to the row.
func splitFlowRow(row *model.FlowQuotaData, shares []cpaAccountShare) []*model.FlowQuotaData {
	var total int64
	for _, share := range shares {
		total += share.tokens
	}
	parts := make([]*model.FlowQuotaData, 0, len(shares))
	var count, quota, tokenUsed int
	for i := len(shares) - 1; i >= 0; i-- {
		clone := *row
		clone.CpaAccount = shares[i].label
		if i == 0 {
			clone.Count = row.Count - count
			clone.Quota = row.Quota - quota
			clone.TokenUsed = row.TokenUsed - tokenUsed
		} else {
			ratio := float64(shares[i].tokens) / float64(total)
			clone.Count = int(float64(row.Count) * ratio)
			clone.Quota = int(float64(row.Quota) * ratio)
			clone.TokenUsed = int(float64(row.TokenUsed) * ratio)
			count += clone.Count
			quota += clone.Quota
			tokenUsed += clone.TokenUsed
		}
		if clone.Count == 0 && clone.Quota == 0 && clone.TokenUsed == 0 {
			continue
		}
		parts = append(parts, &clone)
	}
	return parts
}
