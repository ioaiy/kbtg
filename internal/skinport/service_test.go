package skinport_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ioaiy/kbtg/internal/cache"
	"github.com/ioaiy/kbtg/internal/skinport"
)

func newSilentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// upstreamSpy — управляемый mock-сервер Skinport.
// Возвращает разные данные для tradable=1 и tradable=0.
type upstreamSpy struct {
	tradable    []map[string]any
	nonTradable []map[string]any
	calls       int32
	failOnce    bool
	failed      atomic.Bool
}

func (u *upstreamSpy) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&u.calls, 1)
		if u.failOnce && !u.failed.Load() {
			u.failed.Store(true)
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		if r.URL.Query().Get("tradable") == "1" {
			_ = json.NewEncoder(w).Encode(u.tradable)
		} else {
			_ = json.NewEncoder(w).Encode(u.nonTradable)
		}
	})
}

func newService(t *testing.T, srv *httptest.Server, c *cache.MemoryCache) *skinport.Service {
	t.Helper()
	client := skinport.NewClient(srv.URL, srv.Client())
	return skinport.NewService(client, c, skinport.Config{
		FreshTTL:        2 * time.Second,
		StaleTTL:        10 * time.Second,
		DefaultAppID:    730,
		DefaultCurrency: "USD",
	}, newSilentLogger())
}

func TestService_GetItems_MergesTradableAndNonTradable(t *testing.T) {
	spy := &upstreamSpy{
		tradable: []map[string]any{
			{"market_hash_name": "AK-47 | Redline", "currency": "USD", "min_price": 120.50},
			{"market_hash_name": "AWP | Asiimov", "currency": "USD", "min_price": 200.00},
		},
		nonTradable: []map[string]any{
			{"market_hash_name": "AK-47 | Redline", "currency": "USD", "min_price": 115.30},
			{"market_hash_name": "M4A4 | Howl", "currency": "USD", "min_price": 5000.00},
		},
	}
	server := httptest.NewServer(spy.handler())
	defer server.Close()

	svc := newService(t, server, cache.NewMemoryCache())
	res, err := svc.GetItems(context.Background(), 730, "USD")
	require.NoError(t, err)
	assert.Equal(t, skinport.StatusMiss, res.Status)
	require.Len(t, res.Items, 3)

	// AK-47 присутствует в обоих списках — обе цены заданы.
	ak := findItem(t, res.Items, "AK-47 | Redline")
	assert.Equal(t, "120.5", ak.TradableMinPrice.String())
	assert.Equal(t, "115.3", ak.NonTradableMinPrice.String())

	// AWP только в tradable — non_tradable_min_price = nil.
	awp := findItem(t, res.Items, "AWP | Asiimov")
	require.NotNil(t, awp.TradableMinPrice)
	assert.Nil(t, awp.NonTradableMinPrice)

	// M4A4 только в non-tradable.
	m4 := findItem(t, res.Items, "M4A4 | Howl")
	assert.Nil(t, m4.TradableMinPrice)
	require.NotNil(t, m4.NonTradableMinPrice)
}

func TestService_GetItems_CacheHitAvoidsUpstream(t *testing.T) {
	spy := &upstreamSpy{
		tradable:    []map[string]any{{"market_hash_name": "X", "currency": "USD", "min_price": 1.0}},
		nonTradable: []map[string]any{},
	}
	server := httptest.NewServer(spy.handler())
	defer server.Close()
	svc := newService(t, server, cache.NewMemoryCache())

	_, err := svc.GetItems(context.Background(), 730, "USD")
	require.NoError(t, err)
	firstCalls := atomic.LoadInt32(&spy.calls)

	res, err := svc.GetItems(context.Background(), 730, "USD")
	require.NoError(t, err)
	assert.Equal(t, skinport.StatusHit, res.Status)
	assert.Equal(t, firstCalls, atomic.LoadInt32(&spy.calls), "upstream must not be hit on cache HIT")
}

func TestService_GetItems_StaleOnUpstreamFailure(t *testing.T) {
	spy := &upstreamSpy{
		tradable:    []map[string]any{{"market_hash_name": "X", "currency": "USD", "min_price": 1.0}},
		nonTradable: []map[string]any{},
	}
	server := httptest.NewServer(spy.handler())
	defer server.Close()

	mc := cache.NewMemoryCache()
	// Положим stale-данные напрямую, имитируя предыдущий успешный fetch.
	stale := []skinport.Item{{MarketHashName: "OLD_ITEM", Currency: "USD"}}
	raw, _ := json.Marshal(stale)
	require.NoError(t, mc.Set(context.Background(), "skinport:items:730:USD", raw, 1*time.Nanosecond, 1*time.Hour))
	time.Sleep(2 * time.Millisecond) // фреш истёк, stale жив.

	// Заставим upstream падать.
	server.Close()
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server2.Close()
	svc := newService(t, server2, mc)

	res, err := svc.GetItems(context.Background(), 730, "USD")
	require.NoError(t, err)
	assert.Equal(t, skinport.StatusStale, res.Status)
	require.Len(t, res.Items, 1)
	assert.Equal(t, "OLD_ITEM", res.Items[0].MarketHashName)
}

func TestService_GetItems_UpstreamFailWithoutStale(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()
	svc := newService(t, server, cache.NewMemoryCache())

	_, err := svc.GetItems(context.Background(), 730, "USD")
	require.Error(t, err)
}

func findItem(t *testing.T, items []skinport.Item, name string) skinport.Item {
	t.Helper()
	for _, it := range items {
		if it.MarketHashName == name {
			return it
		}
	}
	t.Fatalf("item %q not found", name)
	return skinport.Item{}
}

