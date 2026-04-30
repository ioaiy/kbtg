package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	DSN             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
}

func NewPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	pgxCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}

	if cfg.MaxConns > 0 {
		pgxCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		pgxCfg.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		pgxCfg.MaxConnLifetime = cfg.MaxConnLifetime
	}

	pool, err := pgxpool.NewWithConfig(ctx, pgxCfg)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	return pool, nil
}
