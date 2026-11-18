package ippanel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/drup/platform/internal/dependencybudget"
	"github.com/drup/platform/internal/observability"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var sharedBudget = func() *dependencybudget.Executor {
	p := dependencybudget.DefaultPolicy(8, 8)
	p.Observe = func(ctx context.Context, outcome string, d time.Duration) {
		observability.LogOperation(ctx, "provider", "ippanel_request", outcome, d)
	}
	return dependencybudget.New(p)
}()
var sharedHTTP = &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second, KeepAlive: 30 * time.Second}).DialContext, ResponseHeaderTimeout: 3 * time.Second, MaxResponseHeaderBytes: 64 * 1024, MaxConnsPerHost: 8, MaxIdleConns: 16, MaxIdleConnsPerHost: 8, IdleConnTimeout: 60 * time.Second, TLSHandshakeTimeout: 2 * time.Second}}

type Client struct {
	BaseURL string
	Token   string
	Sender  string
	HTTP    *http.Client
}

type flexID string

func (f *flexID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err == nil {
		*f = flexID(n.String())
		return nil
	}
	var arr []flexID
	if err := json.Unmarshal(b, &arr); err == nil && len(arr) > 0 {
		*f = arr[0]
		return nil
	}
	*f = ""
	return nil
}

func (f flexID) String() string { return string(f) }

type sendResponse struct {
	Data struct {
		MessageOutboxIDs []flexID `json:"message_outbox_ids"`
		MessageOutboxID  any      `json:"message_outbox_id"`
		BulkID           any      `json:"bulk_id"`
		BulkIDAlt        any      `json:"bulkId"`
	} `json:"data"`
	Meta struct {
		Status  *bool  `json:"status"`
		Message string `json:"message"`
	} `json:"meta"`
}
type creditResponse struct {
	Data map[string]any `json:"data"`
	Meta struct {
		Status  bool   `json:"status"`
		Message string `json:"message"`
	} `json:"meta"`
}

func New(baseURL, token, sender string) *Client {
	// IPPanel must bypass any process-level HTTP(S)_PROXY. VPN routing remains an OS/network concern.
	if parsed, err := url.Parse(baseURL); err == nil && parsed.Scheme != "https" && parsed.Scheme != "http" {
		baseURL = "https://edge.ippanel.com/v1"
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, Sender: sender, HTTP: sharedHTTP}
}

func (c *Client) request(ctx context.Context, method, path string, body any, out any) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	safe := method == http.MethodGet
	reply, err := sharedBudget.Do(ctx, c.HTTP, func(callCtx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(callCtx, method, c.BaseURL+path, bytes.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", c.Token)
		req.Header.Set("Content-Type", "application/json")
		// OBS-01 D-09: propagate the request correlation ID so a slow
		// request can be followed into the provider call. Empty when
		// the caller runs outside an HTTP request; never invent one.
		if id := observability.RequestID(callCtx); id != "" {
			req.Header.Set("X-Request-ID", id)
		}
		return req, nil
	}, safe, 4<<10)
	if err != nil {
		return err
	}
	raw := reply.Body
	if out == nil {
		return nil
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		err = fmt.Errorf("ippanel: empty response")
	} else {
		err = json.Unmarshal(raw, out)
	}
	if err != nil {
		if !safe {
			return dependencybudget.MarkAmbiguous(fmt.Errorf("ippanel: incomplete acknowledgement"))
		}
		return fmt.Errorf("ippanel: invalid response")
	}
	return nil

}

func normalizeRecipient(m string) string {
	m = strings.TrimSpace(m)
	m = strings.ReplaceAll(m, " ", "")
	m = strings.ReplaceAll(m, "-", "")
	if strings.HasPrefix(m, "+") {
		m = m[1:]
	}
	if strings.HasPrefix(m, "00") {
		m = m[2:]
	}
	if strings.HasPrefix(m, "0") {
		m = "98" + m[1:]
	}
	return m
}

func (c *Client) SendPattern(ctx context.Context, recipient, pattern string, params map[string]string) ([]string, error) {
	var out sendResponse
	recipient = normalizeRecipient(recipient)
	err := c.request(ctx, http.MethodPost, "/api/send", map[string]any{"sending_type": "pattern", "from_number": c.Sender, "code": pattern, "recipients": []string{recipient}, "params": params}, &out)
	if err != nil {
		return nil, err
	}
	if out.Meta.Status == nil {
		return nil, dependencybudget.MarkAmbiguous(fmt.Errorf("ippanel: missing send acknowledgement"))
	}
	ids := []string{}
	for _, f := range out.Data.MessageOutboxIDs {
		if s := f.String(); s != "" && s != "0" {
			ids = append(ids, s)
		}
	}
	for _, v := range []any{out.Data.MessageOutboxID, out.Data.BulkID, out.Data.BulkIDAlt} {
		switch t := v.(type) {
		case string:
			if t != "" {
				ids = append(ids, t)
			}
		case float64:
			ids = append(ids, fmt.Sprintf("%.0f", t))
		case []any:
			for _, e := range t {
				if s, ok := e.(string); ok && s != "" {
					ids = append(ids, s)
				}
			}
		}
	}
	if !*out.Meta.Status {
		if len(ids) > 0 {
			return nil, dependencybudget.MarkAmbiguous(fmt.Errorf("ippanel: conflicting send acknowledgement"))
		}
		if out.Meta.Message == "" {
			return nil, fmt.Errorf("ippanel: send rejected without message")
		}
		return nil, fmt.Errorf("ippanel: %s", out.Meta.Message)
	}
	if len(ids) == 0 {
		return nil, dependencybudget.MarkAmbiguous(fmt.Errorf("ippanel: missing message reference"))
	}
	return ids, nil
}

func (c *Client) Credit(ctx context.Context) (map[string]any, error) {
	var out creditResponse
	if err := c.request(ctx, http.MethodGet, "/api/payment/credit/mine", nil, &out); err != nil {
		return nil, err
	}
	if !out.Meta.Status {
		return nil, fmt.Errorf("ippanel: %s", out.Meta.Message)
	}
	return out.Data, nil
}

func (c *Client) Recipients(ctx context.Context, bulkID string) (any, error) {
	var out any
	err := c.request(ctx, http.MethodGet, "/api/report/recipients?bulk_id="+bulkID+"&page=1&per_page=100", nil, &out)
	return out, err
}
