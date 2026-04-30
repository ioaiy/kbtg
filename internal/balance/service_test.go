package balance_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ioaiy/kbtg/internal/balance"
)

// mockRepo — простой мок UserRepo. Позволяет навесить произвольное
// поведение для конкретного теста без сторонних мок-фреймворков.
type mockRepo struct {
	debit      func(ctx context.Context, userID int64, amount decimal.Decimal) (balance.DebitResult, error)
	getBalance func(ctx context.Context, userID int64) (decimal.Decimal, error)
}

func (m *mockRepo) DebitWithHistory(ctx context.Context, userID int64, amount decimal.Decimal) (balance.DebitResult, error) {
	return m.debit(ctx, userID, amount)
}

func (m *mockRepo) GetBalance(ctx context.Context, userID int64) (decimal.Decimal, error) {
	return m.getBalance(ctx, userID)
}

func newSilentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestService_Debit_Success(t *testing.T) {
	repo := &mockRepo{
		debit: func(_ context.Context, userID int64, amount decimal.Decimal) (balance.DebitResult, error) {
			return balance.DebitResult{
				UserID:        userID,
				BalanceBefore: decimal.NewFromInt(500),
				Amount:        amount,
				BalanceAfter:  decimal.NewFromInt(500).Sub(amount),
			}, nil
		},
	}
	svc := balance.NewService(repo, newSilentLogger())

	res, err := svc.Debit(context.Background(), 1, decimal.NewFromInt(100))
	require.NoError(t, err)
	assert.Equal(t, int64(1), res.UserID)
	assert.Equal(t, "400", res.BalanceAfter.String())
}

func TestService_Debit_InsufficientBalance(t *testing.T) {
	repo := &mockRepo{
		debit: func(_ context.Context, _ int64, _ decimal.Decimal) (balance.DebitResult, error) {
			return balance.DebitResult{}, balance.ErrInsufficientBalance
		},
	}
	svc := balance.NewService(repo, newSilentLogger())

	_, err := svc.Debit(context.Background(), 1, decimal.NewFromInt(100))
	require.Error(t, err)
	assert.True(t, errors.Is(err, balance.ErrInsufficientBalance))
}

func TestService_Debit_UserNotFound(t *testing.T) {
	repo := &mockRepo{
		debit: func(_ context.Context, _ int64, _ decimal.Decimal) (balance.DebitResult, error) {
			return balance.DebitResult{}, balance.ErrUserNotFound
		},
	}
	svc := balance.NewService(repo, newSilentLogger())

	_, err := svc.Debit(context.Background(), 999, decimal.NewFromInt(100))
	require.Error(t, err)
	assert.True(t, errors.Is(err, balance.ErrUserNotFound))
}

func TestService_Debit_PreValidation(t *testing.T) {
	cases := []struct {
		name   string
		userID int64
		amount decimal.Decimal
		want   error
	}{
		{"userID = 0", 0, decimal.NewFromInt(100), balance.ErrUserNotFound},
		{"userID < 0", -1, decimal.NewFromInt(100), balance.ErrUserNotFound},
		{"amount = 0", 1, decimal.Zero, balance.ErrInvalidAmount},
		{"amount < 0", 1, decimal.NewFromInt(-50), balance.ErrInvalidAmount},
		{"amount > 2 decimals", 1, decimal.RequireFromString("100.123"), balance.ErrInvalidAmount},
	}

	repo := &mockRepo{
		debit: func(_ context.Context, _ int64, _ decimal.Decimal) (balance.DebitResult, error) {
			t.Fatal("repository must not be called when pre-validation fails")
			return balance.DebitResult{}, nil
		},
	}
	svc := balance.NewService(repo, newSilentLogger())

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Debit(context.Background(), tc.userID, tc.amount)
			require.Error(t, err)
			assert.True(t, errors.Is(err, tc.want), "want %v, got %v", tc.want, err)
		})
	}
}

func TestService_GetBalance_Success(t *testing.T) {
	repo := &mockRepo{
		getBalance: func(_ context.Context, userID int64) (decimal.Decimal, error) {
			return decimal.RequireFromString("250.50"), nil
		},
	}
	svc := balance.NewService(repo, newSilentLogger())

	got, err := svc.GetBalance(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, "250.5", got.String())
}

func TestService_GetBalance_UserNotFound(t *testing.T) {
	repo := &mockRepo{
		getBalance: func(_ context.Context, _ int64) (decimal.Decimal, error) {
			return decimal.Zero, balance.ErrUserNotFound
		},
	}
	svc := balance.NewService(repo, newSilentLogger())

	_, err := svc.GetBalance(context.Background(), 999)
	require.Error(t, err)
	assert.True(t, errors.Is(err, balance.ErrUserNotFound))
}

func TestService_GetBalance_InvalidUserID(t *testing.T) {
	repo := &mockRepo{
		getBalance: func(_ context.Context, _ int64) (decimal.Decimal, error) {
			t.Fatal("repository must not be called for invalid userID")
			return decimal.Zero, nil
		},
	}
	svc := balance.NewService(repo, newSilentLogger())

	for _, id := range []int64{0, -1, -999} {
		_, err := svc.GetBalance(context.Background(), id)
		require.Error(t, err)
		assert.True(t, errors.Is(err, balance.ErrUserNotFound))
	}
}

func TestService_Debit_AcceptedAmounts(t *testing.T) {
	cases := []string{
		"100",       // целое
		"100.1",     // 1 знак
		"100.99",    // 2 знака — граница
		"100.10",    // 2 знака с trailing zero
		"100.100",   // 3 знака, но trailing zero — мат. = 100.10, должны принять
		"0.01",      // минимальное значимое
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			called := false
			repo := &mockRepo{
				debit: func(_ context.Context, _ int64, amount decimal.Decimal) (balance.DebitResult, error) {
					called = true
					return balance.DebitResult{
						UserID:        1,
						BalanceBefore: decimal.NewFromInt(500),
						Amount:        amount,
						BalanceAfter:  decimal.NewFromInt(500).Sub(amount),
					}, nil
				},
			}
			svc := balance.NewService(repo, newSilentLogger())

			_, err := svc.Debit(context.Background(), 1, decimal.RequireFromString(raw))
			require.NoError(t, err)
			assert.True(t, called, "repository must be called for valid amount %q", raw)
		})
	}
}
