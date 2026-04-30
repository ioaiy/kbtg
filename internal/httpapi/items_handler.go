package httpapi

import (
	"net/http"
	"strconv"
)

//	@Summary      Список предметов Skinport
//	@Description  Минимальные цены для tradable и non-tradable. Кешируется в Redis.
//	@Tags         items
//	@Produce      json
//	@Param        app_id    query  int     false  "Steam App ID (default 730)"
//	@Param        currency  query  string  false  "ISO-4217 валюта (default USD)"
//	@Success      200  {array}   skinport.Item
//	@Failure      400  {object}  httpapi.APIError
//	@Failure      502  {object}  httpapi.APIError
//	@Router       /v1/items [get]
func (s *Server) handleGetItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	appID := 0
	if v := q.Get("app_id"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeError(w, r, s.log, http.StatusBadRequest, codeInvalidRequest, "app_id must be positive integer")
			return
		}
		appID = n
	}

	currency := q.Get("currency")
	if currency != "" && !isCurrencyCode(currency) {
		writeError(w, r, s.log, http.StatusBadRequest, codeInvalidRequest, "currency must be ISO-4217 code (3 uppercase letters)")
		return
	}

	res, err := s.skinport.GetItems(r.Context(), appID, currency)
	if err != nil {
		s.log.Error("skinport get items failed", "err", err)
		writeError(w, r, s.log, http.StatusBadGateway, codeUpstreamError, "skinport upstream unavailable")
		return
	}

	w.Header().Set("X-Cache", string(res.Status))
	writeJSON(w, s.log, http.StatusOK, res.Items)
}

func isCurrencyCode(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}
