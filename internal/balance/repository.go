package balance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// PgRepo — Postgres-реализация UserRepo.
//
// queryTimeout применяется двумя способами:
//   - read-операции (GetBalance) — context.WithTimeout на весь вызов;
//   - write-транзакция (DebitWithHistory) — SET LOCAL statement_timeout
//     внутри tx + общий context.WithTimeout (с двойным окном на серию
//     statement'ов в транзакции).
//
// NUMERIC ↔ decimal.Decimal: pgx/v5 не сканит NUMERIC в decimal.Decimal
// без отдельного codec, поэтому читаем через ::text + decimal.NewFromString,
// пишем через decimal.String() (Postgres парсит NUMERIC из строки сам).
type PgRepo struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func NewPgRepo(pool *pgxpool.Pool, queryTimeout time.Duration) *PgRepo {
	return &PgRepo{pool: pool, queryTimeout: queryTimeout}
}

// GetBalance читает текущий баланс пользователя.
// Без FOR UPDATE — это read-only операция, eventual consistency приемлема.
func (r *PgRepo) GetBalance(ctx context.Context, userID int64) (decimal.Decimal, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	var balanceStr string
	err := r.pool.QueryRow(ctx,
		`SELECT balance::text FROM users WHERE id = $1`,
		userID,
	).Scan(&balanceStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return decimal.Zero, ErrUserNotFound
		}
		return decimal.Zero, fmt.Errorf("select balance: %w", err)
	}
	balance, err := decimal.NewFromString(balanceStr)
	if err != nil {
		return decimal.Zero, fmt.Errorf("parse balance: %w", err)
	}
	return balance, nil
}

// DebitWithHistory атомарно списывает amount и пишет запись в balance_history.
//
// SELECT FOR UPDATE — обязательная защита от race conditions при параллельных
// списаниях. lock_timeout/statement_timeout прерывают зависшие транзакции.
func (r *PgRepo) DebitWithHistory(ctx context.Context, userID int64, amount decimal.Decimal) (DebitResult, error) {
	// На всю транзакцию даём 2x query-timeout — внутри несколько запросов.
	ctx, cancel := context.WithTimeout(ctx, 2*r.queryTimeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return DebitResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// lock_timeout оставляем 3s — это про ожидание блокировки строки,
	// семантически отличается от statement_timeout.
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '3s'`); err != nil {
		return DebitResult{}, fmt.Errorf("set lock_timeout: %w", err)
	}
	stmtTO := fmt.Sprintf(`SET LOCAL statement_timeout = '%dms'`, r.queryTimeout.Milliseconds())
	if _, err := tx.Exec(ctx, stmtTO); err != nil {
		return DebitResult{}, fmt.Errorf("set statement_timeout: %w", err)
	}

	var balanceBeforeStr string
	err = tx.QueryRow(ctx,
		`SELECT balance::text FROM users WHERE id = $1 FOR UPDATE`,
		userID,
	).Scan(&balanceBeforeStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DebitResult{}, ErrUserNotFound
		}
		return DebitResult{}, fmt.Errorf("select for update: %w", err)
	}

	balanceBefore, err := decimal.NewFromString(balanceBeforeStr)
	if err != nil {
		return DebitResult{}, fmt.Errorf("parse balance: %w", err)
	}

	if balanceBefore.LessThan(amount) {
		return DebitResult{}, ErrInsufficientBalance
	}

	balanceAfter := balanceBefore.Sub(amount)

	if _, err := tx.Exec(ctx,
		`UPDATE users SET balance = $1::numeric WHERE id = $2`,
		balanceAfter.String(), userID,
	); err != nil {
		return DebitResult{}, fmt.Errorf("update balance: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO balance_history
			(user_id, amount, balance_before, balance_after)
		 VALUES ($1, $2::numeric, $3::numeric, $4::numeric)`,
		userID, amount.String(), balanceBefore.String(), balanceAfter.String(),
	); err != nil {
		return DebitResult{}, fmt.Errorf("insert history: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return DebitResult{}, fmt.Errorf("commit: %w", err)
	}

	return DebitResult{
		UserID:        userID,
		BalanceBefore: balanceBefore,
		Amount:        amount,
		BalanceAfter:  balanceAfter,
	}, nil
}
