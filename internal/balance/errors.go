package balance

import "errors"

var (
	ErrUserNotFound = errors.New("balance: user not found")
	ErrInsufficientBalance = errors.New("balance: insufficient balance")
	ErrInvalidAmount = errors.New("balance: invalid amount")
)
