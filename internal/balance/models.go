package balance

import "github.com/shopspring/decimal"

type DebitResult struct {
	UserID        int64
	BalanceBefore decimal.Decimal
	Amount        decimal.Decimal
	BalanceAfter  decimal.Decimal
}
