package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupCpaCredentialSyncDB(t *testing.T, baseURL string) *model.CpaInstance {
	t.Helper()
	previousDB, previousSecret := model.DB, common.CryptoSecret
	t.Cleanup(func() {
		model.DB = previousDB
		common.CryptoSecret = previousSecret
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.CpaInstance{}, &model.CpaAccountCache{}))
	model.DB = db
	common.CryptoSecret = "cpa-test-secret"

	enc, err := EncryptCpaSecret("k")
	require.NoError(t, err)
	instance := &model.CpaInstance{Name: "t", BaseURL: baseURL, SecretEnc: enc}
	require.NoError(t, model.CreateCpaInstance(instance))
	return instance
}

// The single-card refresh only re-reads the target account's credential and
// must never touch or prune other cached accounts.
func TestSyncCpaAccountCredentialOnlyTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v8/management/credentials" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"files":[{"name":"a.json","success":10,"failed":2,"access_token":"secret"}]}`))
	}))
	defer server.Close()
	instance := setupCpaCredentialSyncDB(t, server.URL)

	a := &CpaAccountSnapshot{InstanceID: instance.ID, Name: "a.json", Credential: map[string]any{"name": "a.json", "success": 1}, ProxyURL: "socks5h://x"}
	b := &CpaAccountSnapshot{InstanceID: instance.ID, Name: "b.json", Credential: map[string]any{"name": "b.json", "success": 7}}
	require.NoError(t, saveSnapshot(a))
	require.NoError(t, saveSnapshot(b))

	require.NoError(t, syncCpaAccountCredential(context.Background(), instance.ID, "a.json"))

	gotA, err := loadSnapshot(instance.ID, "a.json")
	require.NoError(t, err)
	assert.EqualValues(t, 10, gotA.Credential["success"])
	assert.EqualValues(t, 2, gotA.Credential["failed"])
	assert.NotContains(t, gotA.Credential, "access_token", "tokens must be filtered out")
	assert.NotZero(t, gotA.CredentialSyncedAt)
	assert.Equal(t, "socks5h://x", gotA.ProxyURL, "other snapshot fields are kept")

	// b.json is absent from the CPA list but must not be pruned or changed.
	gotB, err := model.GetCpaAccountCache(instance.ID, "b.json")
	require.NoError(t, err)
	require.NotNil(t, gotB)
	snapB, err := loadSnapshot(instance.ID, "b.json")
	require.NoError(t, err)
	assert.EqualValues(t, 7, snapB.Credential["success"])
	assert.Zero(t, snapB.CredentialSyncedAt)
}

// A failing CPA list read returns an error and leaves the cache untouched,
// so the caller can still run the normal refresh.
func TestSyncCpaAccountCredentialListFailureKeepsCache(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	instance := setupCpaCredentialSyncDB(t, server.URL)
	a := &CpaAccountSnapshot{InstanceID: instance.ID, Name: "a.json", Credential: map[string]any{"name": "a.json", "success": 1}}
	require.NoError(t, saveSnapshot(a))

	assert.Error(t, syncCpaAccountCredential(context.Background(), instance.ID, "a.json"))
	assert.Positive(t, calls.Load())
	got, err := loadSnapshot(instance.ID, "a.json")
	require.NoError(t, err)
	assert.EqualValues(t, 1, got.Credential["success"])
}

// The 5s background sync must never prune, must not rewrite unchanged
// snapshots, and must skip accounts whose refresh is in flight.
func TestSyncCpaCredentialsNoPrune(t *testing.T) {
	success := 10
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v8/management/credentials" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(fmt.Sprintf(`{"files":[{"name":"a.json","success":%d},{"name":"c.json","success":3}]}`, success)))
	}))
	defer server.Close()
	instance := setupCpaCredentialSyncDB(t, server.URL)
	require.NoError(t, saveSnapshot(&CpaAccountSnapshot{InstanceID: instance.ID, Name: "a.json", Credential: map[string]any{"name": "a.json", "success": 1}}))
	require.NoError(t, saveSnapshot(&CpaAccountSnapshot{InstanceID: instance.ID, Name: "b.json", Credential: map[string]any{"name": "b.json"}}))
	ctx := context.Background()

	require.NoError(t, syncCpaCredentialsNoPrune(ctx, instance))
	gotA, err := loadSnapshot(instance.ID, "a.json")
	require.NoError(t, err)
	assert.EqualValues(t, 10, gotA.Credential["success"])
	gotC, err := loadSnapshot(instance.ID, "c.json")
	require.NoError(t, err)
	assert.EqualValues(t, 3, gotC.Credential["success"], "new accounts are added")
	cacheB, err := model.GetCpaAccountCache(instance.ID, "b.json")
	require.NoError(t, err)
	assert.NotNil(t, cacheB, "accounts missing from the list are not pruned")

	// Unchanged credential: no write.
	cacheA, err := model.GetCpaAccountCache(instance.ID, "a.json")
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.CpaAccountCache{}).Where("id = ?", cacheA.ID).Update("updated_at", 1).Error)
	require.NoError(t, syncCpaCredentialsNoPrune(ctx, instance))
	cacheA, err = model.GetCpaAccountCache(instance.ID, "a.json")
	require.NoError(t, err)
	assert.EqualValues(t, 1, cacheA.UpdatedAt, "unchanged snapshot must not be rewritten")

	// Account lock held (refresh in flight): skipped this round.
	success = 20
	unlock := lockAccount(instance.ID, "a.json")
	require.NoError(t, syncCpaCredentialsNoPrune(ctx, instance))
	unlock()
	gotA, err = loadSnapshot(instance.ID, "a.json")
	require.NoError(t, err)
	assert.EqualValues(t, 10, gotA.Credential["success"])
	require.NoError(t, syncCpaCredentialsNoPrune(ctx, instance))
	gotA, err = loadSnapshot(instance.ID, "a.json")
	require.NoError(t, err)
	assert.EqualValues(t, 20, gotA.Credential["success"])
}
