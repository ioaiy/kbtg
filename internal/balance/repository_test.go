//go:build integration

// Integration-тесты PgRepo с реальным Postgres через testcontainers-go.
//
// Запуск: go test -tags=integration -race -count=1 ./internal/balance/...
//
// Главный сценарий — race на параллельных списаниях. Без SELECT FOR UPDATE
// этот тест падает 100% (баланс уходит в минус, в истории больше записей,
// чем должно быть).
package balance_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ioaiy/kbtg/internal/balance"
)

// setupPostgres поднимает Postgres-контейнер, применяет схему и сидит юзера id=1.
func setupPostgres(t *testing.T, initialBalance string) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = container.Terminate(ctx)
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	// Накатываем схему вручную (вместо migrate, чтобы тест был самодостаточным).
	_, err = pool.Exec(ctx, `
		CREATE TABLE users (
			id      BIGINT PRIMARY KEY,
			balance NUMERIC(20, 2) NOT NULL DEFAULT 0
				CHECK (balance >= 0)
		);
		CREATE TABLE balance_history (
			id             BIGSERIAL PRIMARY KEY,
			user_id        BIGINT         NOT NULL REFERENCES users(id),
			amount         NUMERIC(20, 2) NOT NULL CHECK (amount > 0),
			balance_before NUMERIC(20, 2) NOT NULL,
			balance_after  NUMERIC(20, 2) NOT NULL,
			created_at     TIMESTAMPTZ    NOT NULL DEFAULT NOW()
		);
		INSERT INTO users (id, balance) VALUES (1, ` + initialBalance + `);
	`)
	require.NoError(t, err)
	return pool
}

func TestPgRepo_DebitWithHistory_Success(t *testing.T) {
	pool := setupPostgres(t, "500.00")
	repo := balance.NewPgRepo(pool, 5*time.Second)

	res, err := repo.DebitWithHistory(context.Background(), 1, decimal.RequireFromString("100.00"))
	require.NoError(t, err)
	require.Equal(t, "500", res.BalanceBefore.String())
	require.Equal(t, "400", res.BalanceAfter.String())

	var rows int
	err = pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM balance_history`).Scan(&rows)
	require.NoError(t, err)
	require.Equal(t, 1, rows)
}

func TestPgRepo_DebitWithHistory_Insufficient(t *testing.T) {
	pool := setupPostgres(t, "50.00")
	repo := balance.NewPgRepo(pool, 5*time.Second)

	_, err := repo.DebitWithHistory(context.Background(), 1, decimal.RequireFromString("100.00"))
	require.Error(t, err)
	require.True(t, errors.Is(err, balance.ErrInsufficientBalance))

	// История не должна получить записи при отказе.
	var rows int
	err = pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM balance_history`).Scan(&rows)
	require.NoError(t, err)
	require.Equal(t, 0, rows)
}

func TestPgRepo_DebitWithHistory_UserNotFound(t *testing.T) {
	pool := setupPostgres(t, "100.00")
	repo := balance.NewPgRepo(pool, 5*time.Second)

	_, err := repo.DebitWithHistory(context.Background(), 999, decimal.RequireFromString("10.00"))
	require.Error(t, err)
	require.True(t, errors.Is(err, balance.ErrUserNotFound))
}

func TestPgRepo_GetBalance_Success(t *testing.T) {
	pool := setupPostgres(t, "250.50")
	repo := balance.NewPgRepo(pool, 5*time.Second)

	bal, err := repo.GetBalance(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "250.5", bal.String())
}

func TestPgRepo_GetBalance_NotFound(t *testing.T) {
	pool := setupPostgres(t, "100.00")
	repo := balance.NewPgRepo(pool, 5*time.Second)

	_, err := repo.GetBalance(context.Background(), 999)
	require.Error(t, err)
	require.True(t, errors.Is(err, balance.ErrUserNotFound))
}

func TestPgRepo_GetBalance_AfterDebit(t *testing.T) {
	pool := setupPostgres(t, "100.00")
	repo := balance.NewPgRepo(pool, 5*time.Second)
	ctx := context.Background()

	_, err := repo.DebitWithHistory(ctx, 1, decimal.RequireFromString("30.00"))
	require.NoError(t, err)

	bal, err := repo.GetBalance(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "70", bal.String())
}

// TestPgRepo_RaceCondition — главный тест на корректность.
//
// При балансе 100 и 50 параллельных запросах по $10 успешными должны быть
// ровно 10. Без FOR UPDATE этот тест падает: успешных будет больше,
// либо CHECK на уровне БД отклонит транзакции с записанной историей.
func TestPgRepo_RaceCondition(t *testing.T) {
	pool := setupPostgres(t, "100.00")
	repo := balance.NewPgRepo(pool, 5*time.Second)

	const (
		concurrency = 50
		amount      = "10.00"
		initial     = 100
	)

	var (
		successCount int32
		insuffCount  int32
		otherErr     int32
		wg           sync.WaitGroup
	)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, err := repo.DebitWithHistory(ctx, 1, decimal.RequireFromString(amount))
			switch {
			case err == nil:
				atomic.AddInt32(&successCount, 1)
			case errors.Is(err, balance.ErrInsufficientBalance):
				atomic.AddInt32(&insuffCount, 1)
			default:
				atomic.AddInt32(&otherErr, 1)
				t.Logf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	require.EqualValues(t, 0, atomic.LoadInt32(&otherErr), "no infrastructure errors expected")
	require.EqualValues(t, initial/10, atomic.LoadInt32(&successCount), "exact number of successful debits")
	require.EqualValues(t, concurrency-(initial/10), atomic.LoadInt32(&insuffCount), "rest must be insufficient balance")

	// Проверяем итоговое состояние БД.
	var (
		finalBalanceStr string
		historyRows     int
	)
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT balance::text FROM users WHERE id=1`).Scan(&finalBalanceStr))
	finalBalance, err := decimal.NewFromString(finalBalanceStr)
	require.NoError(t, err)
	require.True(t, finalBalance.IsZero(), "expected zero balance, got %s", finalBalance.String())

	require.NoError(t, pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM balance_history`).Scan(&historyRows))
	require.Equal(t, initial/10, historyRows, "history must have exactly N successful entries")
}
