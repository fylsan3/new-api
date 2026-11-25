package service

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/common"
	"net/http"
	"strings"
	"time"
)

func isContainerAccount(snap *CpaAccountSnapshot) bool {
	return snap != nil && credentialString(snap.Credential, "source") == "container"
}

func (c *cpaClient) containerAction(ctx context.Context, name, action string, out any) error {
	return c.do(ctx, http.MethodPost, "/account-environments/action", nil, map[string]any{"action": action, "slug": name}, out)
}

// Only import the worker's explicit sanitized snapshot. Never download credentials
// or query Anthropic from NewAPI/the central host for a container account.
func applyContainerObservation(snap *CpaAccountSnapshot) {
	if !isContainerAccount(snap) {
		return
	}
	snap.ResetGrants = nil
	snap.ResetGrantsError = ""
	observation, _ := snap.Credential["container_observation"].(map[string]any)
	usage, _ := observation["usage"].(map[string]any)
	if values, ok := usage["values"].(map[string]any); ok {
		if grants, ok := values["cedar_ember"].(map[string]any); ok {
			snap.ResetGrants = grants
		}
	}
	if message, ok := usage["error"].(string); ok {
		snap.ResetGrantsError = message
	}

	if values, ok := usage["values"].(map[string]any); ok {
		stamp, _ := usage["observed_at"].(string)
		if observed, err := time.Parse(time.RFC3339, stamp); err == nil && observed.Unix() >= snap.QuotaObservedAt {
			snap.Usage = values
			snap.QuotaObservedAt = observed.Unix()
		}
	}
	if message, ok := usage["error"].(string); ok && message != "" {
		snap.QuotaError = message
	} else if usage["observed_at"] != nil {
		snap.QuotaError = ""
		snap.QuotaErrorStatus = 0
	}
	profile, _ := observation["profile"].(map[string]any)
	values, _ := profile["values"].(map[string]any)
	// Use only explicit subscription labels, never infer a tier from token usage.
	text := ""
	for _, key := range []string{"subscription_type", "plan_type", "rate_limit_tier"} {
		if value, ok := values[key].(string); ok {
			text += " " + strings.ToLower(value)
		}
	}
	switch {
	case strings.Contains(text, "20x"):
		snap.PlanType = "plan_max20"
	case strings.Contains(text, "5x"):
		snap.PlanType = "plan_max5"
	case strings.Contains(text, "pro"):
		snap.PlanType = "plan_pro"
	case strings.Contains(text, "max"):
		snap.PlanType = "plan_max"
	}
}

func refreshContainerAccount(ctx context.Context, client *cpaClient, snap *CpaAccountSnapshot) (*CpaAccountSnapshot, error) {
	snap.QuotaCheckedAt = common.GetTimestamp()
	if err := client.containerAction(ctx, snap.Name, "account-update", nil); err != nil {
		snap.QuotaError = err.Error()
		if saveErr := saveSnapshot(snap); saveErr != nil {
			return nil, saveErr
		}
		return snap, nil
	}
	files, err := client.listCredentials(ctx)
	if err != nil {
		return nil, err
	}
	for _, raw := range files {
		if credentialString(raw, "name") == snap.Name {
			if credentialString(raw, "source") != "container" || credentialString(raw, "id") != credentialString(snap.Credential, "id") {
				return nil, errors.New("container account binding changed")
			}
			snap.Credential = filterCredential(raw)
			snap.CredentialSyncedAt = common.GetTimestamp()
			applyContainerObservation(snap)
			if err := saveSnapshot(snap); err != nil {
				return nil, err
			}
			return snap, nil
		}
	}
	return nil, ErrCpaAccountNotFound
}
