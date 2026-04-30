package skinport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string, httpClient *http.Client) *Client {
	return &Client{baseURL: baseURL, httpClient: httpClient}
}

// FetchItems — GET /v1/items с фильтром по tradable.
//
// Skinport API требует Accept-Encoding: br (brotli) — без него возвращает
// HTTP 406. Go stdlib brotli не поддерживает, поэтому распаковываем вручную
// через github.com/andybalholm/brotli.
func (c *Client) FetchItems(ctx context.Context, appID int, currency string, tradable bool) ([]upstreamItem, error) {
	u, err := url.Parse(c.baseURL + "/v1/items")
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	q := u.Query()
	q.Set("app_id", strconv.Itoa(appID))
	q.Set("currency", currency)
	q.Set("tradable", boolToParam(tradable))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "br")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("skinport upstream status %d", resp.StatusCode)
	}

	// Если мы явно попросили brotli, Go не распакует автоматически
	// (auto-decompress работает только для gzip и только когда
	// Accept-Encoding не задан явно).
	var reader io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "br") {
		reader = brotli.NewReader(resp.Body)
	}

	var items []upstreamItem
	if err := json.NewDecoder(reader).Decode(&items); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return items, nil
}

func boolToParam(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
