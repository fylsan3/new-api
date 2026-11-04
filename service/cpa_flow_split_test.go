package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
)

func TestSplitFlowRowSumsToOriginal(t *testing.T) {
	row := &model.FlowQuotaData{ChannelID: 7, ModelName: "claude-x", Count: 10, Quota: 1001, TokenUsed: 333}
	shares := sortedShares(map[string]int64{"a@x": 600, "b@x": 300, "c@x": 100})
	assert.Equal(t, "a@x", shares[0].label)

	parts := splitFlowRow(row, shares)
	var count, quota, tokens int
	byLabel := map[string]*model.FlowQuotaData{}
	for _, part := range parts {
		count += part.Count
		quota += part.Quota
		tokens += part.TokenUsed
		byLabel[part.CpaAccount] = part
		assert.Equal(t, 7, part.ChannelID)
	}
	assert.Equal(t, 10, count)
	assert.Equal(t, 1001, quota)
	assert.Equal(t, 333, tokens)
	assert.Equal(t, 100, byLabel["c@x"].Quota)
	assert.Equal(t, 300, byLabel["b@x"].Quota)
	assert.Equal(t, 601, byLabel["a@x"].Quota) // largest share absorbs rounding
	assert.Equal(t, "", row.CpaAccount, "original row must not be mutated")
}

func TestSplitFlowRowDropsEmptyParts(t *testing.T) {
	row := &model.FlowQuotaData{Count: 1, Quota: 1, TokenUsed: 1}
	parts := splitFlowRow(row, sortedShares(map[string]int64{"big": 999, "tiny": 1, "zero": 0}))
	assert.Len(t, parts, 1)
	assert.Equal(t, "big", parts[0].CpaAccount)
}
