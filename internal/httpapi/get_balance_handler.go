package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ioaiy/kbtg/internal/balance"
)

type balanceResponse struct {
	UserID  int64  `json:"user_id"`
	Balance string `json:"balance"`
}

//	@Summary      Текущий баланс пользователя
//	@Description  Возвращает баланс пользователя по ID.
//	@Tags         balance
//	@Produce      json
//	@Param        id   path   int   true   "User ID"
//	@Success      200  {object}  balanceResponse
//	@Failure      400  {object}  httpapi.APIError  "INVALID_USER_ID"
//	@Failure      404  {object}  httpapi.APIError  "USER_NOT_FOUND"
//	@Failure      500  {object}  httpapi.APIError
//	@Router       /v1/users/{id}/balance [get]
func (s *Server) handleGetBalance(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, r, s.log, http.StatusBadRequest, codeInvalidUserID, "id must be positive integer")
		return
	}

	bal, err := s.balance.GetBalance(r.Context(), userID)
	if err != nil {
		switch {
		case errors.Is(err, balance.ErrUserNotFound):
			writeError(w, r, s.log, http.StatusNotFound, codeUserNotFound, "user not found")
		default:
			s.log.Error("get balance unexpected error", "err", err)
			writeError(w, r, s.log, http.StatusInternalServerError, codeInternalError, "internal error")
		}
		return
	}

	writeJSON(w, s.log, http.StatusOK, balanceResponse{
		UserID:  userID,
		Balance: bal.StringFixed(2),
	})
}
