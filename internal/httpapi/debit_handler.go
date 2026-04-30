package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/shopspring/decimal"

	"github.com/ioaiy/kbtg/internal/balance"
)

// debitRequest принимает amount строкой или числом — оба валидны.
// json.RawMessage позволяет различить эти случаи и отдать корректную ошибку.
type debitRequest struct {
	Amount json.RawMessage `json:"amount"`
}

// debitResponse сериализует суммы строками для сохранения точности на клиенте.
type debitResponse struct {
	UserID        int64  `json:"user_id"`
	BalanceBefore string `json:"balance_before"`
	Amount        string `json:"amount"`
	BalanceAfter  string `json:"balance_after"`
}

//	@Summary      Списание с баланса пользователя
//	@Description  Атомарно списывает amount, пишет в balance_history. SELECT FOR UPDATE.
//	@Tags         balance
//	@Accept       json
//	@Produce      json
//	@Param        id       path   int             true   "User ID"
//	@Param        request  body   debitRequest    true   "Amount to debit"
//	@Success      200  {object}  debitResponse
//	@Failure      400  {object}  httpapi.APIError  "INVALID_REQUEST | INVALID_AMOUNT | INVALID_USER_ID | INSUFFICIENT_BALANCE"
//	@Failure      404  {object}  httpapi.APIError  "USER_NOT_FOUND"
//	@Failure      500  {object}  httpapi.APIError
//	@Router       /v1/users/{id}/debit [post]
func (s *Server) handleDebit(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, r, s.log, http.StatusBadRequest, codeInvalidUserID, "id must be positive integer")
		return
	}

	var req debitRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, r, s.log, http.StatusBadRequest, codeInvalidRequest, "invalid JSON body")
		return
	}

	amount, perr := parseAmount(req.Amount)
	if perr != nil {
		writeError(w, r, s.log, http.StatusBadRequest, codeInvalidAmount, perr.Error())
		return
	}

	res, err := s.balance.Debit(r.Context(), userID, amount)
	if err != nil {
		switch {
		case errors.Is(err, balance.ErrUserNotFound):
			writeError(w, r, s.log, http.StatusNotFound, codeUserNotFound, "user not found")
		case errors.Is(err, balance.ErrInsufficientBalance):
			writeError(w, r, s.log, http.StatusBadRequest, codeInsufficientBalance, "insufficient balance for the requested amount")
		case errors.Is(err, balance.ErrInvalidAmount):
			writeError(w, r, s.log, http.StatusBadRequest, codeInvalidAmount, "invalid amount")
		default:
			s.log.Error("debit unexpected error", "err", err)
			writeError(w, r, s.log, http.StatusInternalServerError, codeInternalError, "internal error")
		}
		return
	}

	writeJSON(w, s.log, http.StatusOK, debitResponse{
		UserID:        res.UserID,
		BalanceBefore: res.BalanceBefore.StringFixed(2),
		Amount:        res.Amount.StringFixed(2),
		BalanceAfter:  res.BalanceAfter.StringFixed(2),
	})
}

func parseAmount(raw json.RawMessage) (decimal.Decimal, error) {
	if len(raw) == 0 {
		return decimal.Zero, errors.New("amount is required")
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return decimal.Zero, errors.New("amount must be a valid number string")
		}
		d, err := decimal.NewFromString(s)
		if err != nil {
			return decimal.Zero, errors.New("amount is not a valid decimal")
		}
		return d, nil
	}
	d, err := decimal.NewFromString(string(raw))
	if err != nil {
		return decimal.Zero, errors.New("amount is not a valid decimal")
	}
	return d, nil
}
