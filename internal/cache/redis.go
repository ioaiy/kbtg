package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrCacheMiss = errors.New("cache: miss")

// RedisCache хранит значение в двух ключах: fresh (короткий TTL) и stale (длинный TTL).
// При недоступности upstream caller может прочитать stale через GetStale.
type RedisCache struct {
	client      *redis.Client
	staleSuffix string
}

func NewRedisCache(client *redis.Client) *RedisCache {
	return &RedisCache{client: client, staleSuffix: ":stale"}
}

func (c *RedisCache) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrCacheMiss
		}
		return nil, fmt.Errorf("redis get: %w", err)
	}
	return val, nil
}

func (c *RedisCache) GetStale(ctx context.Context, key string) ([]byte, error) {
	val, err := c.client.Get(ctx, key+c.staleSuffix).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrCacheMiss
		}
		return nil, fmt.Errorf("redis get stale: %w", err)
	}
	return val, nil
}

func (c *RedisCache) Set(ctx context.Context, key string, val []byte, freshTTL, staleTTL time.Duration) error {
	pipe := c.client.Pipeline()
	pipe.Set(ctx, key, val, freshTTL)
	pipe.Set(ctx, key+c.staleSuffix, val, staleTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}
	return nil
}
