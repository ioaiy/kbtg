// Skinport Backend.
//
//	@title           Skinport Backend
//	@version         1.0
//	@description     Skinport items + balance debit.
//	@BasePath        /
//	@contact.name    9SAINT
package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	httpSwagger "github.com/swaggo/http-swagger/v2"

	"github.com/ioaiy/kbtg/internal/balance"
	"github.com/ioaiy/kbtg/internal/cache"
	"github.com/ioaiy/kbtg/internal/config"
	"github.com/ioaiy/kbtg/internal/httpapi"
	"github.com/ioaiy/kbtg/internal/platform/logger"
	pgplatform "github.com/ioaiy/kbtg/internal/platform/postgres"
	redisplatform "github.com/ioaiy/kbtg/internal/platform/redis"
	"github.com/ioaiy/kbtg/internal/skinport"

	_ "github.com/ioaiy/kbtg/docs"
)

func main() {
	if err := run(); err != nil {
		_, _ = os.Stderr.WriteString("fatal: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logger.New(os.Stdout, logger.Config{
		Level:  cfg.Log.Level,
		Format: cfg.Log.Format,
	})
	log.Info("config loaded", "config", cfg.String())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgplatform.NewPool(ctx, pgplatform.Config{
		DSN:             cfg.Postgres.DSN(),
		MaxConns:        cfg.Postgres.MaxConns,
		MinConns:        cfg.Postgres.MinConns,
		MaxConnLifetime: cfg.Postgres.MaxConnLifetime,
	})
	if err != nil {
		return err
	}
	defer pool.Close()
	log.Info("postgres connected")

	redisClient, err := redisplatform.NewClient(ctx, redisplatform.Config{
		Addr:             cfg.Redis.Addr,
		Password:         cfg.Redis.Password,
		DB:               cfg.Redis.DB,
		DialTimeout:      cfg.Redis.DialTimeout,
		OperationTimeout: cfg.Redis.OperationTimeout,
	})
	if err != nil {
		return err
	}
	defer func() { _ = redisClient.Close() }()
	log.Info("redis connected")

	cacheImpl := cache.NewRedisCache(redisClient)

	skinportClient := skinport.NewClient(cfg.Skinport.BaseURL, &http.Client{
		Timeout: cfg.Skinport.Timeout,
	})
	skinportSvc := skinport.NewService(skinportClient, cacheImpl, skinport.Config{
		FreshTTL:        cfg.Skinport.CacheTTL,
		StaleTTL:        cfg.Skinport.CacheStaleTTL,
		DefaultAppID:    cfg.Skinport.DefaultAppID,
		DefaultCurrency: cfg.Skinport.DefaultCurrency,
	}, log.With("component", "skinport"))

	balanceRepo := balance.NewPgRepo(pool, cfg.Postgres.QueryTimeout)
	balanceSvc := balance.NewService(balanceRepo, log.With("component", "balance"))

	srv := httpapi.NewServer(balanceSvc, skinportSvc, log)

	// Swagger UI монтируем только вне production: в проде он раскрывает
	// контракт API и внутренние схемы, что нежелательно с точки зрения
	// security. Управляется через APP_ENV.
	if !cfg.IsProduction() {
		srv.Router().Get("/v1/swagger/*", httpSwagger.Handler(
			httpSwagger.URL("/v1/swagger/doc.json"),
		))
		log.Info("swagger UI enabled", "path", "/v1/swagger/index.html", "env", cfg.HTTP.Env)
	} else {
		log.Info("swagger UI disabled in production")
	}

	addr := ":" + strconv.Itoa(cfg.HTTP.Port)
	if err := srv.Run(ctx, addr,
		cfg.HTTP.ReadTimeout,
		cfg.HTTP.WriteTimeout,
		cfg.HTTP.IdleTimeout,
		cfg.HTTP.ShutdownTimeout,
	); err != nil {
		log.Error("server exited with error", "err", err)
		return err
	}
	log.Info("server stopped cleanly")
	return nil
}
