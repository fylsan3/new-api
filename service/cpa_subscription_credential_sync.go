package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

// Background credential sync (user decision 2026-11-06): every 5 seconds
// New API reads each CPA's credential list (GET /credentials) so the card
// success / failed counts and status stay fresh. That list is served from
// CPA memory and never reaches Anthropic, so it adds no upstream traffic.
// Quota / plan are NOT refreshed here; they stay manual only.
//
// Safety rules:
//   - update / add only, never prune: a partial list (CPA restarting) must not
//     delete cards. Pruning stays with the manual "Refresh all".
//   - write a snapshot only when its credential actually changed.
//   - an account whose lock is held (a refresh in flight) is skipped this
//     round instead of waiting for it.
//   - each CPA read has a 4s timeout; time.Ticker drops ticks while a round
//     is still running, so slow rounds are skipped, never queued.
const (
	cpaCredentialSyncInterval = 5 * time.Second
	cpaCredentialSyncTimeout  = 4 * time.Second
)

var cpaCredentialSyncOnce sync.Once

func startCpaCredentialSync() {
	cpaCredentialSyncOnce.Do(func() {
		gopool.Go(func() {
			ticker := time.NewTicker(cpaCredentialSyncInterval)
			defer ticker.Stop()
			for range ticker.C {
				runCpaCredentialSync()
			}
		})
	})
}

func runCpaCredentialSync() {
	if cpaRefreshAllBusy.Load() {
		// Refresh-all already syncs (and prunes) the whole list.
		return
	}
	instances, err := model.ListCpaInstances()
	if err != nil {
		return
	}
	for i := range instances {
		ctx, cancel := context.WithTimeout(context.Background(), cpaCredentialSyncTimeout)
		if err := syncCpaCredentialsNoPrune(ctx, &instances[i]); err != nil {
			logCpaCredentialSyncError(instances[i].ID, err)
		} else {
			cpaCredentialSyncErrLogged.Delete(instances[i].ID)
		}
		cancel()
	}
}

// cpaCredentialSyncErrLogged keeps a 5s loop from flooding the log while a
// CPA is down: one line when it starts failing, one per error change.
var cpaCredentialSyncErrLogged sync.Map // instanceID -> last error text

func logCpaCredentialSyncError(instanceID int, err error) {
	text := err.Error()
	if last, ok := cpaCredentialSyncErrLogged.Load(instanceID); ok && last.(string) == text {
		return
	}
	cpaCredentialSyncErrLogged.Store(instanceID, text)
	common.SysLog(fmt.Sprintf("cpa credential sync instance %d failed: %v", instanceID, err))
}

// syncCpaCredentialsNoPrune updates cached credentials of one CPA without
// deleting accounts missing from the list.
func syncCpaCredentialsNoPrune(ctx context.Context, instance *model.CpaInstance) error {
	client, err := newCpaClient(instance)
	if err != nil {
		return err
	}
	files, err := client.listCredentials(ctx)
	if err != nil {
		return err
	}
	for _, raw := range files {
		cred := filterCredential(raw)
		name := credentialString(cred, "name")
		if name == "" {
			continue
		}
		unlock, ok := tryLockAccount(instance.ID, name)
		if !ok {
			continue
		}
		updateCachedCredential(instance.ID, name, cred)
		unlock()
	}
	return nil
}

// updateCachedCredential saves the credential only when it differs from the
// cached one. The caller holds the account lock.
func updateCachedCredential(instanceID int, name string, cred map[string]any) {
	snap, err := loadSnapshot(instanceID, name)
	if err != nil {
		return
	}
	isClaude := isClaudeCredential(cred)
	if snap.Credential != nil && snap.IsClaude == isClaude && sameCredential(snap.Credential, cred) {
		return
	}
	snap.Credential = cred
	snap.CredentialSyncedAt = common.GetTimestamp()
	snap.IsClaude = isClaude
	_ = saveSnapshot(snap)
}

// sameCredential compares by JSON encoding (map keys are sorted), which also
// normalises numbers decoded from the cache vs. from CPA.
func sameCredential(a, b map[string]any) bool {
	left, errA := common.Marshal(a)
	right, errB := common.Marshal(b)
	return errA == nil && errB == nil && string(left) == string(right)
}

func tryLockAccount(instanceID int, name string) (func(), bool) {
	v, _ := cpaAccountLocks.LoadOrStore(accountKey(instanceID, name), &sync.Mutex{})
	mu := v.(*sync.Mutex)
	if !mu.TryLock() {
		return nil, false
	}
	return mu.Unlock, true
}
