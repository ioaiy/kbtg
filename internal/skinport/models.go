package skinport

import "github.com/shopspring/decimal"

type upstreamItem struct {
	MarketHashName string           `json:"market_hash_name"`
	Currency       string           `json:"currency"`
	MinPrice       *decimal.Decimal `json:"min_price"`
}

// Item — результат после слияния tradable / non-tradable.
// Поля цен — указатели для корректной сериализации null.
type Item struct {
	MarketHashName      string           `json:"market_hash_name"`
	Currency            string           `json:"currency"`
	TradableMinPrice    *decimal.Decimal `json:"tradable_min_price"`
	NonTradableMinPrice *decimal.Decimal `json:"non_tradable_min_price"`
}
