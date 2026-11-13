package service

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
)

// Peaks leave out 429s, and the pool peak is the busiest minute of all
// accounts together, not the sum of each account's own peak.
func TestSummarizeCpaRatesPeakExcludes429(t *testing.T) {
	const minute = int64(6000)
	rows := []model.CpaUsageMinute{
		// a1: a 429 storm (50 requests, 48 turned away), then a normal minute.
		{InstanceID: 1, AuthIndex: "a1", BucketTs: minute - 600, Requests: 50, Status429: 48, Failed: 48, Tokens: 100},
		{InstanceID: 1, AuthIndex: "a1", BucketTs: minute - 60, Requests: 10, Tokens: 1000},
		// a2: busiest in another minute.
		{InstanceID: 1, AuthIndex: "a2", BucketTs: minute - 1200, Requests: 20, Tokens: 500},
		{InstanceID: 1, AuthIndex: "a2", BucketTs: minute - 60, Requests: 5, Status429: 1, Failed: 1, Tokens: 200},
	}
	rates, total := summarizeCpaRates(rows, minute)
	a1, a2 := rates[usageKey(1, "a1")], rates[usageKey(1, "a2")]
	if a1.Peak != 10 {
		t.Fatalf("a1 peak = %d, want 10 (the 429 storm must not count)", a1.Peak)
	}
	if a1.Rpm1 != 10 || a1.Tpm1 != 1000 || a1.Err1h != 48 {
		t.Fatalf("a1 = %+v", a1)
	}
	if a2.Peak != 20 || a2.Rpm1 != 5 || a2.Err1h != 1 {
		t.Fatalf("a2 = %+v", a2)
	}
	// Pool: last minute 15 requests; busiest minute 20 (a2 alone), not 10+20.
	if total.Rpm1 != 15 || total.Tpm1 != 1200 || total.Err1h != 49 {
		t.Fatalf("total = %+v", total)
	}
	if total.Peak != 20 {
		t.Fatalf("pool peak = %d, want 20", total.Peak)
	}
	if total.Rpm5 != 3 { // 15 requests in the last 5 minutes
		t.Fatalf("pool rpm5 = %v, want 3", total.Rpm5)
	}
	// TPM peaks: a1 1000, a2 500; pool = busiest minute together (1000+200).
	if a1.TpmPeak != 1000 || a2.TpmPeak != 500 || total.TpmPeak != 1200 {
		t.Fatalf("tpm peaks = %d / %d / pool %d", a1.TpmPeak, a2.TpmPeak, total.TpmPeak)
	}
}

// A wide bar shows the per-minute average and keeps its busiest minute.
func TestBucketCpaRatePointsPeak(t *testing.T) {
	const from, span, step = int64(0), int64(1200), int64(600)
	rows := []model.CpaUsageMinute{
		{InstanceID: 1, AuthIndex: "a1", BucketTs: 0, Requests: 30, Tokens: 3000},
		{InstanceID: 1, AuthIndex: "a2", BucketTs: 0, Requests: 10, Status429: 4, Failed: 4, Tokens: 1000},
		{InstanceID: 1, AuthIndex: "a1", BucketTs: 60, Requests: 10, Tokens: 6000},
		{InstanceID: 1, AuthIndex: "a1", BucketTs: 660, Requests: 20, Status429: 20, Failed: 20},
		{InstanceID: 1, AuthIndex: "a1", BucketTs: 1200, Requests: 99}, // out of range
	}
	points := bucketCpaRatePoints(rows, from, span, step)
	if len(points) != 2 {
		t.Fatalf("points = %d, want 2", len(points))
	}
	first := points[0]
	if first.Requests != 5 || first.Tokens != 1000 { // 50 requests, 10000 tokens over 10 minutes
		t.Fatalf("first average = %+v", first)
	}
	if first.PeakRequests != 36 || first.PeakTokens != 6000 { // minute 0: 30 + (10 - 4)
		t.Fatalf("first peak = %+v", first)
	}
	if first.R429 != 4 || first.Failed != 4 {
		t.Fatalf("first failures = %+v", first)
	}
	second := points[1]
	if second.Requests != 2 || second.PeakRequests != 0 || second.R429 != 20 {
		t.Fatalf("second = %+v (a minute of only 429s has no served peak)", second)
	}
}

// The busiest minute also reports how many of its requests failed for
// another reason than 429, so the chart can colour that part.
func TestBucketCpaRatePointsPeakFailed(t *testing.T) {
	rows := []model.CpaUsageMinute{
		{InstanceID: 1, AuthIndex: "a1", BucketTs: 0, Requests: 12, Failed: 5, Status429: 2},
		{InstanceID: 1, AuthIndex: "a1", BucketTs: 60, Requests: 8, Failed: 8},
	}
	p := bucketCpaRatePoints(rows, 0, 600, 600)[0]
	if p.PeakRequests != 10 || p.PeakFailed != 3 { // minute 0: 12 - 2 served, 5 - 2 failed
		t.Fatalf("peak = %d failed = %d, want 10 / 3", p.PeakRequests, p.PeakFailed)
	}
}

// TPM counts input + output only, like the usage log of New API.
func TestAggregateCpaMinutesTokensLeaveOutCache(t *testing.T) {
	rec := cpaUsageQueueRecord{AuthIndex: "a1"}
	rec.Tokens.InputTokens, rec.Tokens.OutputTokens = 100, 20
	rec.Tokens.CacheReadTokens, rec.Tokens.CacheCreationTokens = 9000, 500
	rows := aggregateCpaMinutes(1, []cpaUsageQueueRecord{rec})
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	for _, row := range rows {
		if row.Requests != 1 || row.Tokens != 120 {
			t.Fatalf("row = %+v, want 1 request / 120 tokens", row)
		}
	}
}
