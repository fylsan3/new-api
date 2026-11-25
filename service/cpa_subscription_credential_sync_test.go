package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

func TestContainerCPARoutesAndQuotaRefresh(t *testing.T) {
	percent := 90.0
	var calls []string
	credential := func() map[string]any {
		return map[string]any{"id": "container-a", "name": "a", "source": "container", "provider": "claude", "auth_index": "opaque-a", "container_observation": map[string]any{
			"usage":   map[string]any{"observed_at": "2026-11-25T12:00:00Z", "values": map[string]any{"cedar_ember": map[string]any{"eligible": true, "grants": []any{map[string]any{"id": "fixture-grant", "resets_left": 2}}}, "limits": []any{map[string]any{"kind": "weekly_scoped", "percent": percent, "scope": map[string]any{"model": map[string]any{"display_name": "Fable 5"}}}}}},
			"profile": map[string]any{"values": map[string]any{"subscription_type": "pro"}},
		}}
	}
	failRefresh := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		require.Equal(t, "Bearer k", r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/v8/management/credentials":
			raw, _ := common.Marshal(map[string]any{"files": []any{credential()}})
			_, _ = w.Write(raw)
		case "/v8/management/account-environments/action":
			var body map[string]any
			require.NoError(t, common.DecodeJson(r.Body, &body))
			assert.Equal(t, "a", body["slug"])
			if body["action"] == "account-update" {
				if failRefresh {
					w.WriteHeader(503)
					_, _ = w.Write([]byte(`{"error":"network_unavailable"}`))
					return
				}
				percent = 5
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Errorf("unexpected legacy/credential API: %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	instance := setupCpaCredentialSyncDB(t, server.URL)
	require.NoError(t, syncCpaAccountCredential(context.Background(), instance.ID, "a"))
	snap, err := loadSnapshot(instance.ID, "a")
	require.NoError(t, err)
	require.Equal(t, "plan_pro", snap.PlanType)
	require.NotNil(t, snap.Usage["limits"])
	snap, err = RefreshCpaAccount(context.Background(), instance.ID, "a")
	require.NoError(t, err)
	limits := snap.Usage["limits"].([]any)
	assert.Equal(t, 5.0, limits[0].(map[string]any)["percent"])
	assert.NotNil(t, snap.ResetGrants)
	require.NoError(t, RefreshCpaAccountToken(context.Background(), instance.ID, "a"))
	before := len(calls)
	_, err = ClaimCpaResetGrant(context.Background(), instance.ID, "a")
	require.ErrorContains(t, err, "does not support reset grants")
	assert.Len(t, calls, before)
	failRefresh = true
	snap, err = RefreshCpaAccount(context.Background(), instance.ID, "a")
	require.NoError(t, err)
	assert.NotEmpty(t, snap.QuotaError)
	assert.Equal(t, 5.0, snap.Usage["limits"].([]any)[0].(map[string]any)["percent"])
	for _, path := range calls {
		assert.NotContains(t, path, "api-call")
		assert.NotContains(t, path, "download")
	}
}

// Run only against the isolated CPA A/B fixture started by its acceptance runner.
func TestContainerCPAIntegration(t *testing.T) {
	endpoint := os.Getenv("CPA_AB_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("isolated CPA A/B fixture not running")
	}
	require.True(t, strings.HasPrefix(endpoint, "http://127.0.0.1:"))
	instance := setupCpaCredentialSyncDB(t, endpoint)
	secret, err := EncryptCpaSecret(os.Getenv("CPA_AB_TEST_KEY"))
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(instance).Update("secret_enc", secret).Error)
	instance.SecretEnc = secret
	credentials, err := SyncCpaInstance(context.Background(), instance)
	require.NoError(t, err)
	require.Len(t, credentials, 2)
	assert.Equal(t, "container", credentials["a"]["source"])
	snap, err := RefreshCpaAccount(context.Background(), instance.ID, "a")
	require.NoError(t, err)
	assert.Empty(t, snap.QuotaError)
	assert.Equal(t, "plan_pro", snap.PlanType)
	require.NotNil(t, snap.Usage["limits"])
	assert.Equal(t, 5.0, snap.Usage["limits"].([]any)[0].(map[string]any)["percent"])
	require.NoError(t, SetCpaAccountDisabled(context.Background(), instance.ID, "a", true))
	snap, err = loadSnapshot(instance.ID, "a")
	require.NoError(t, err)
	assert.True(t, credentialBool(snap.Credential, "disabled"))
	require.NoError(t, SetCpaAccountDisabled(context.Background(), instance.ID, "a", false))
	proxy, err := RevealCpaAccountProxy(context.Background(), instance.ID, "a")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(proxy, "socks5h://"))
	models, err := ListCpaAccountModels(context.Background(), instance.ID, "a")
	require.NoError(t, err)
	assert.NotEmpty(t, models)
}
