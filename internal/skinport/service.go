package skinport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/ioaiy/kbtg/internal/cache"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

// cacheStore — интерфейс кеша на стороне consumer'а.
// Возвращает cache.ErrCacheMiss при отсутствии ключа.
type cacheStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	GetStale(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, val []byte, freshTTL, staleTTL time.Duration) error
}

type upstreamFetcher interface {
	FetchItems(ctx context.Context, appID int, currency string, tradable bool) ([]upstreamItem, error)
}

type Service struct {
	client      upstreamFetcher
	cache       cacheStore
	freshTTL    time.Duration
	staleTTL    time.Duration
	defaultApp  int
	defaultCurr string
	log         *slog.Logger
	sf          singleflight.Group
}

type Config struct {
	FreshTTL        time.Duration
	StaleTTL        time.Duration
	DefaultAppID    int
	DefaultCurrency string
}

func NewService(client upstreamFetcher, c cacheStore, cfg Config, log *slog.Logger) *Service {
	return &Service{
		client:      client,
		cache:       c,
		freshTTL:    cfg.FreshTTL,
		staleTTL:    cfg.StaleTTL,
		defaultApp:  cfg.DefaultAppID,
		defaultCurr: cfg.DefaultCurrency,
		log:         log,
	}
}

type CacheStatus string

const (
	StatusHit   CacheStatus = "hit"
	StatusMiss  CacheStatus = "miss"
	StatusStale CacheStatus = "stale"
)

type Result struct {
	Items  []Item
	Status CacheStatus
}

// GetItems: HIT → cache; MISS → upstream через singleflight; upstream FAIL → stale.
func (s *Service) GetItems(ctx context.Context, appID int, currency string) (*Result, error) {
	if appID <= 0 {
		appID = s.defaultApp
	}
	if currency == "" {
		currency = s.defaultCurr
	}

	key := s.cacheKey(appID, currency)

	if data, err := s.cache.Get(ctx, key); err == nil {
		var items []Item
		if json.Unmarshal(data, &items) == nil {
			return &Result{Items: items, Status: StatusHit}, nil
		}
		s.log.Warn("cache value unmarshal failed, falling through", "key", key)
	} else if !errors.Is(err, cache.ErrCacheMiss) {
		s.log.Warn("cache get error, degraded mode", "err", err)
	}

	raw, err, _ := s.sf.Do(key, func() (any, error) {
		if data, gerr := s.cache.Get(ctx, key); gerr == nil {
			var items []Item
			if json.Unmarshal(data, &items) == nil {
				return &Result{Items: items, Status: StatusHit}, nil
			}
		}

		items, fetchErr := s.fetchAndMerge(ctx, appID, currency)
		if fetchErr != nil {
			if staleData, sErr := s.cache.GetStale(ctx, key); sErr == nil {
				var staleItems []Item
				if json.Unmarshal(staleData, &staleItems) == nil {
					s.log.Warn("upstream failed, serving stale", "err", fetchErr, "key", key)
					return &Result{Items: staleItems, Status: StatusStale}, nil
				}
			}
			return nil, fetchErr
		}

		raw, _ := json.Marshal(items)
		if setErr := s.cache.Set(ctx, key, raw, s.freshTTL, s.staleTTL); setErr != nil {
			s.log.Warn("cache set error", "err", setErr, "key", key)
		}
		return &Result{Items: items, Status: StatusMiss}, nil
	})
	if err != nil {
		return nil, err
	}
	res, ok := raw.(*Result)
	if !ok || res == nil {
		return nil, fmt.Errorf("skinport: unexpected nil result from singleflight")
	}
	return res, nil
}

func (s *Service) fetchAndMerge(ctx context.Context, appID int, currency string) ([]Item, error) {
	var (
		tradable    []upstreamItem
		nonTradable []upstreamItem
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		out, err := s.client.FetchItems(gctx, appID, currency, true)
		if err != nil {
			return fmt.Errorf("fetch tradable: %w", err)
		}
		tradable = out
		return nil
	})
	g.Go(func() error {
		out, err := s.client.FetchItems(gctx, appID, currency, false)
		if err != nil {
			return fmt.Errorf("fetch non-tradable: %w", err)
		}
		nonTradable = out
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return mergeItems(tradable, nonTradable, currency), nil
}

func mergeItems(tradable, nonTradable []upstreamItem, currency string) []Item {
	idx := make(map[string]int, len(tradable)+len(nonTradable))
	out := make([]Item, 0, len(tradable))

	for _, t := range tradable {
		idx[t.MarketHashName] = len(out)
		out = append(out, Item{
			MarketHashName:   t.MarketHashName,
			Currency:         currency,
			TradableMinPrice: t.MinPrice,
		})
	}
	for _, n := range nonTradable {
		if i, ok := idx[n.MarketHashName]; ok {
			out[i].NonTradableMinPrice = n.MinPrice
			continue
		}
		idx[n.MarketHashName] = len(out)
		out = append(out, Item{
			MarketHashName:      n.MarketHashName,
			Currency:            currency,
			NonTradableMinPrice: n.MinPrice,
		})
	}
	return out
}

func (s *Service) cacheKey(appID int, currency string) string {
	return "skinport:items:" + strconv.Itoa(appID) + ":" + currency
}
