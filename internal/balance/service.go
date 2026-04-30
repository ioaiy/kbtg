package balance

import (
	"context"
	"log/slog"

	"github.com/shopspring/decimal"
)

// UserRepo — интерфейс репозитория на стороне consumer'а.
type UserRepo interface {
	DebitWithHistory(ctx context.Context, userID int64, amount decimal.Decimal) (DebitResult, error)
	GetBalance(ctx context.Context, userID int64) (decimal.Decimal, error)
}

type Service struct {
	repo UserRepo
	log  *slog.Logger
}

func NewService(repo UserRepo, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log}
}

// Debit выполняет валидацию и делегирует списание репозиторию.
// Проверки: userID > 0, amount > 0, amount помещается в NUMERIC(20, 2) без потери точности.
func (s *Service) Debit(ctx context.Context, userID int64, amount decimal.Decimal) (DebitResult, error) {
	if userID <= 0 {
		return DebitResult{}, ErrUserNotFound
	}
	if !amount.IsPositive() {
		return DebitResult{}, ErrInvalidAmount
	}
	// Проверяем «округление до 2 знаков не меняет значение». Это пропускает
	// и "100.10", и "100.100" (математически равны), но режет "100.123".
	if !amount.Equal(amount.Truncate(2)) {
		return DebitResult{}, ErrInvalidAmount
	}

	res, err := s.repo.DebitWithHistory(ctx, userID, amount)
	if err != nil {
		s.log.Warn("debit failed", "user_id", userID, "amount", amount.String(), "err", err)
		return DebitResult{}, err
	}
	s.log.Info("debit success",
		"user_id", res.UserID,
		"amount", res.Amount.String(),
		"balance_before", res.BalanceBefore.String(),
		"balance_after", res.BalanceAfter.String(),
	)
	return res, nil
}

// GetBalance возвращает текущий баланс пользователя.
func (s *Service) GetBalance(ctx context.Context, userID int64) (decimal.Decimal, error) {
	if userID <= 0 {
		return decimal.Zero, ErrUserNotFound
	}
	return s.repo.GetBalance(ctx, userID)
}
