// Package tapin is the client for Tapin's upstream API
// (https://api.tapin.ir), built ONLY from contracts captured in
// Tapin's official Tipax implementation guide — see
// ~/workspace/tapin-spec/tapin-contract-capture.md for the verbatim
// evidence (page-cited). The Post (irpost) service contract is NOT
// published in that guide, so this package deliberately has no Post
// methods: callers must keep Post behind an explicit stub until the
// panel's یکپارچه‌سازی ← وب سرویس material is captured.
//
// Every upstream method answers with the envelope
// { "returns": { "status": <int>, "message": <string> },
//
//	"entries": <object|null> }.
//
// The guide's own response examples show exactly two success codes:
// 20 («موفق») and 21 («عملیات با موفقیت انجام شد»). Any other status
// is an upstream failure whose Persian message is surfaced verbatim —
// never swallowed, never replaced by a local guess.
//
// The client NEVER logs the token, the request body, or the response
// body: UpstreamError carries only the numeric status and the
// upstream message, which are safe to show the supplier.
package tapin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/drup/platform/internal/dependencybudget"
	"github.com/drup/platform/internal/observability"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is Tapin's production API host (guide pp.7, 16–31).
const DefaultBaseURL = "https://api.tapin.ir"

// Envelope success codes, from the guide's response examples:
// 20 on the read/register methods (pp.16, 18, 19, 24, 26, 29) and
// 21 on the change-status methods (pp.20, 21, 23).
const (
	StatusSuccessOK        = 20
	StatusSuccessOperation = 21
)

// Real failure codes observed live from the prod server with the
// owner's own shop credentials (2026-11-05 probes):
//
//	332   «توکن مورد نظر یافت نشد.» — the token is not recognised.
//	338   «شما هیچ فروشگاه فعالی را انتخاب نکرده اید.» — no shop_id
//	      was supplied in the request body; auth itself passed.
//	30129 «فروشگاه شما دسترسی استفاده از این سرویس را ندارد...» —
//	      token + shop are valid and paired, but the Tipax service
//	      is not activated for the shop (activation is a Tapin
//	      support ticket + city review, guide p.2).
const (
	StatusTokenNotFound       = 332
	StatusNoShopSelected      = 338
	StatusServiceNotActivated = 30129
)

// IsSuccessStatus reports whether an envelope returns.status is one
// of the guide's two success codes.
func IsSuccessStatus(status int) bool {
	return status == StatusSuccessOK || status == StatusSuccessOperation
}

// UpstreamError is a business failure answered inside a 200 envelope:
// the numeric returns.status plus the upstream Persian message,
// verbatim. It is the ONLY error shape callers surface to users.
type UpstreamError struct {
	Status  int
	Message string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("tapin upstream status %d: %s", e.Status, e.Message)
}

// IsServiceNotActivated reports whether err is the 30129 verdict:
// credentials valid, Tipax service not activated for the shop.
func IsServiceNotActivated(err error) bool {
	var ue *UpstreamError
	return errAs(err, &ue) && ue.Status == StatusServiceNotActivated
}

// IsTokenNotFound reports whether err is the 332 verdict.
func IsTokenNotFound(err error) bool {
	var ue *UpstreamError
	return errAs(err, &ue) && ue.Status == StatusTokenNotFound
}

// errAs is errors.As without importing errors in every caller.
func errAs(err error, target **UpstreamError) bool {
	for err != nil {
		if ue, ok := err.(*UpstreamError); ok {
			*target = ue
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// flexInt parses a JSON number or a numeric string (the guide's
// delete example shows returns.status as the string "99125", p.22,
// while live responses carry a number).
type flexInt int

func (f *flexInt) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*f = flexInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		v, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("tapin: non-numeric status %q", s)
		}
		*f = flexInt(v)
		return nil
	}
	return fmt.Errorf("tapin: cannot parse status from %s", string(data))
}

type envelopeReturns struct {
	Status  *flexInt `json:"status"`
	Message string   `json:"message"`
}

type envelope struct {
	Returns envelopeReturns `json:"returns"`
	Entries json.RawMessage `json:"entries"`
}

// Client talks to one Tapin API host with a bounded timeout. The
// zero-value base falls back to DefaultBaseURL; tests point the
// client at an httptest server.
var sharedBudget = func() *dependencybudget.Executor {
	p := dependencybudget.DefaultPolicy(16, 32)
	p.Observe = func(ctx context.Context, outcome string, d time.Duration) {
		observability.LogOperation(ctx, "provider", "tapin_request", outcome, d)
	}
	return dependencybudget.New(p)
}()
var sharedHTTP = func() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = (&net.Dialer{Timeout: time.Second, KeepAlive: 30 * time.Second}).DialContext
	tr.ResponseHeaderTimeout = 3 * time.Second
	tr.MaxResponseHeaderBytes = 64 * 1024
	tr.MaxConnsPerHost = 16
	tr.MaxIdleConnsPerHost = 16
	return &http.Client{Timeout: 6 * time.Second, Transport: tr}
}()

func safeReadPath(path string) bool {
	switch path {
	case "/api/v4/tipax/public/user/order/options/", "/api/v4/tipax/public/user/order/check-price/", "/api/v4/tipax/public/user/order/filter/":
		return true
	}
	return false
}

type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient builds a client for baseURL ("" = DefaultBaseURL) with
// a shared connection pool and the bounded five-second logical-call budget.
func NewClient(baseURL string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		http:    sharedHTTP,
	}
}

// BaseURL returns the client's API host (no trailing slash).
func (c *Client) BaseURL() string { return c.baseURL }

// Call POSTs payload as JSON with the shop token and returns the
// envelope's entries on success. A non-success envelope status is an
// *UpstreamError; a transport failure is a plain error (callers map
// it to an "unreachable" verdict). The response body is read with a
// 1MB bound and never logged.
func (c *Client) Call(ctx context.Context, token, path string, payload interface{}) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("tapin: encode request: %w", err)
	}
	safe := safeReadPath(path)
	reply, err := sharedBudget.Do(ctx, c.http, func(callCtx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		// OBS-01 D-09: propagate the request correlation ID so a slow
		// request can be followed into the provider call. Empty when
		// the caller runs outside an HTTP request (background jobs
		// carry their own fresh ID from the runner, so this is set
		// there too); never invent one here.
		if id := observability.RequestID(callCtx); id != "" {
			req.Header.Set("X-Request-ID", id)
		}
		return req, nil
	}, safe, 1<<20)
	if err != nil {
		return nil, err
	}
	raw := reply.Body
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		err := fmt.Errorf("tapin: undecodable response (http %d)", reply.StatusCode)
		if !safe {
			return nil, dependencybudget.MarkAmbiguous(err)
		}
		return nil, err
	}
	if env.Returns.Status == nil || *env.Returns.Status <= 0 {
		err := fmt.Errorf("tapin: missing or invalid acknowledgement status")
		if !safe {
			return nil, dependencybudget.MarkAmbiguous(err)
		}
		return nil, err
	}
	if !IsSuccessStatus(int(*env.Returns.Status)) {
		return nil, &UpstreamError{Status: int(*env.Returns.Status), Message: env.Returns.Message}
	}
	return env.Entries, nil
}

// GetRaw performs an authenticated GET and returns the upstream
// status, content type and body verbatim (bounded at 8MB). It exists
// for the parcel-label proxy: the guide documents the label URL and
// the hash_raw lookup but not the response format (capture file,
// NOT-CAPTURED item 2), so the bytes are streamed through untouched.
func (c *Client) GetRaw(ctx context.Context, token, path string) (int, string, []byte, error) {
	reply, err := sharedBudget.Do(ctx, c.http, func(callCtx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(callCtx, http.MethodGet, c.baseURL+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		// OBS-01 D-09: propagate the request correlation ID (see Call).
		if id := observability.RequestID(callCtx); id != "" {
			req.Header.Set("X-Request-ID", id)
		}
		return req, nil
	}, true, 8<<20)
	if err != nil && (reply.StatusCode == 0 || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) {
		return 0, "", nil, err
	}
	return reply.StatusCode, reply.Header.Get("Content-Type"), reply.Body, nil

}

// Options calls the Tipax options method (guide p.7) with the shop
// id in the body — an empty body is answered 338 (no shop selected),
// so the shop id is part of the connectivity verdict. Only the
// envelope verdict is used; the entries payload is discarded.
func (c *Client) Options(ctx context.Context, token, shopID string) error {
	_, err := c.Call(ctx, token, "/api/v4/tipax/public/user/order/options/", map[string]string{"shop_id": shopID})
	return err
}

// TipaxProduct is one goods line of a check-price / register call
// (guide pp.17–19). Amounts are in Rials, weights in grams. Title is
// only sent by register (check-price omits it upstream too, hence
// omitempty).
type TipaxProduct struct {
	Title            string `json:"title,omitempty"`
	DiscountPerCount int64  `json:"discount_per_count"`
	AmountPerCount   int64  `json:"amount_per_count"`
	WeightPerCount   int    `json:"weight_per_count"`
	Count            int    `json:"count"`
}

// TipaxCheckPriceRequest is the price-inquiry payload (guide p.17).
type TipaxCheckPriceRequest struct {
	ShopID             string         `json:"shop_id"`
	ReceiverProvinceID int            `json:"receiver_province_id"`
	ReceiverCityID     int            `json:"receiver_city_id"`
	ProductTypeID      int            `json:"product_type_id"`
	PackingTypeID      int            `json:"packing_type_id"`
	PaymentType        int            `json:"payment_type"`
	ServiceType        int            `json:"service_type"`
	DeliveryType       int            `json:"delivery_type"`
	PickupType         int            `json:"pickup_type"`
	Products           []TipaxProduct `json:"products"`
	Length             int            `json:"length"`
	Width              int            `json:"width"`
	Height             int            `json:"height"`
	PackageWeight      int            `json:"package_weight"`
}

// TipaxCheckPriceResult is the price-inquiry entries object (guide
// p.18). All money fields are Rials. The customer-facing freight is
// TotalSendPrice (freight incl. tax); TotalPanelPrice is Tapin's own
// panel service fee charged to the shop and is NOT part of the
// freight quote.
type TipaxCheckPriceResult struct {
	SendPrice         int64 `json:"send_price"`
	SendTax           int64 `json:"send_tax"`
	TotalSendPrice    int64 `json:"total_send_price"`
	TotalProductPrice int64 `json:"total_product_price"`
	TotalPanelPrice   int64 `json:"total_panel_price"`
	TotalReceivePrice int64 `json:"total_receive_price"`
	TotalWeight       int   `json:"total_weight"`
	TotalDiscount     int64 `json:"total_discount"`
	CourierDiscount   int64 `json:"courier_discount"`
}

// TipaxCheckPrice runs the Tipax price inquiry (guide p.17).
func (c *Client) TipaxCheckPrice(ctx context.Context, token string, req TipaxCheckPriceRequest) (*TipaxCheckPriceResult, error) {
	entries, err := c.Call(ctx, token, "/api/v4/tipax/public/user/order/check-price/", req)
	if err != nil {
		return nil, err
	}
	var out TipaxCheckPriceResult
	if err := json.Unmarshal(entries, &out); err != nil {
		return nil, fmt.Errorf("tapin: decode check-price entries: %w", err)
	}
	return &out, nil
}

// TipaxRegisterRequest is the order-registration payload (guide
// pp.18–19). There are deliberately NO sender fields: the sender is
// the shop identified by shop_id + token (capture file, NOT-CAPTURED
// item 5). ReceiverLat/ReceiverLong are pointers so a missing
// location is omitted rather than sent as 0,0.
type TipaxRegisterRequest struct {
	ShopID             string         `json:"shop_id"`
	ReceiverProvinceID int            `json:"receiver_province_id"`
	ReceiverCityID     int            `json:"receiver_city_id"`
	ReceiverFirstName  string         `json:"receiver_first_name"`
	ReceiverLastName   string         `json:"receiver_last_name"`
	ReceiverMobile     string         `json:"receiver_mobile"`
	ReceiverAddress    string         `json:"receiver_address"`
	ReceiverPlaque     string         `json:"receiver_plaque"`
	ReceiverUnit       string         `json:"receiver_unit"`
	ReceiverFloor      string         `json:"receiver_floor"`
	ReceiverPostalCode string         `json:"receiver_postal_code"`
	ProductTypeID      int            `json:"product_type_id"`
	PackingTypeID      int            `json:"packing_type_id"`
	PaymentType        int            `json:"payment_type"`
	ServiceType        int            `json:"service_type"`
	DeliveryType       int            `json:"delivery_type"`
	PickupType         int            `json:"pickup_type"`
	EnableLabelPrivacy bool           `json:"enable_label_privacy"`
	Products           []TipaxProduct `json:"products"`
	RegisterType       int            `json:"register_type"`
	ManualID           string         `json:"manual_id"`
	PackageWeight      int            `json:"package_weight"`
	ReceiverLat        *float64       `json:"receiver_lat,omitempty"`
	ReceiverLong       *float64       `json:"receiver_long,omitempty"`
	Length             int            `json:"length"`
	Width              int            `json:"width"`
	Height             int            `json:"height"`
}

// TipaxRegisterResult is the register entries object (guide p.19).
// ContractCode is the Tipax barcode — the shipment's tracking code.
type TipaxRegisterResult struct {
	UUID              string  `json:"uuid"`
	Status            int     `json:"status"`
	ContractCode      string  `json:"contract_code"`
	ContractID        flexInt `json:"contract_id"` // guide example sends a number; flexInt also accepts a string
	PanelServicePrice int64   `json:"panel_service_price"`
	PanelServiceTax   int64   `json:"panel_service_tax"`
	PanelSMSPrice     int64   `json:"panel_sms_price"`
	PanelSMSTax       int64   `json:"panel_sms_tax"`
	SendPrice         int64   `json:"send_price"`
	SendTax           int64   `json:"send_tax"`
	TotalSendPrice    int64   `json:"total_send_price"`
	TotalPanelPrice   int64   `json:"total_panel_price"`
	TotalReceivePrice int64   `json:"total_receive_price"`
	TotalProductPrice int64   `json:"total_product_price"`
	TotalWeight       int     `json:"total_weight"`
}

// TipaxRegisterOrder registers an order with Tipax (guide p.18).
func (c *Client) TipaxRegisterOrder(ctx context.Context, token string, req TipaxRegisterRequest) (*TipaxRegisterResult, error) {
	entries, err := c.Call(ctx, token, "/api/v4/tipax/public/user/order/register/", req)
	if err != nil {
		return nil, err
	}
	var out TipaxRegisterResult
	if err := json.Unmarshal(entries, &out); err != nil {
		return nil, fmt.Errorf("tapin: decode register entries: %w", err)
	}
	if strings.TrimSpace(out.ContractCode) == "" {
		return nil, fmt.Errorf("tapin: register succeeded but returned no contract_code")
	}
	return &out, nil
}

// TipaxFilterHashRaw looks up an order's label hash_raw via the
// filter method (guide pp.20, 30–31), matching by contract_code.
// Returns "" when no entry carries a hash.
func (c *Client) TipaxFilterHashRaw(ctx context.Context, token, shopID, contractCode string) (string, error) {
	entries, err := c.Call(ctx, token, "/api/v4/tipax/public/user/order/filter/", map[string]interface{}{
		"shop_id":       shopID,
		"contract_code": contractCode,
		"page":          1,
		"count":         1,
	})
	if err != nil {
		return "", err
	}
	return ExtractHashRaw(entries), nil
}

// ExtractHashRaw pulls hash_raw out of a filter entries payload. The
// guide's example shows a single order object; a list shape is
// handled too (capture file, NOT-CAPTURED item 3 — parse
// defensively, take the first hash found).
func ExtractHashRaw(entries json.RawMessage) string {
	if len(entries) == 0 {
		return ""
	}
	var single struct {
		HashRaw string `json:"hash_raw"`
	}
	if err := json.Unmarshal(entries, &single); err == nil && single.HashRaw != "" {
		return single.HashRaw
	}
	var list []struct {
		HashRaw string `json:"hash_raw"`
	}
	if err := json.Unmarshal(entries, &list); err == nil {
		for _, item := range list {
			if item.HashRaw != "" {
				return item.HashRaw
			}
		}
	}
	var wrapped struct {
		List []struct {
			HashRaw string `json:"hash_raw"`
		} `json:"list"`
	}
	if err := json.Unmarshal(entries, &wrapped); err == nil {
		for _, item := range wrapped.List {
			if item.HashRaw != "" {
				return item.HashRaw
			}
		}
	}
	return ""
}

// TipaxLabelPath builds the parcel-label path (guide pp.20, 31).
func TipaxLabelPath(hashRaw string) string {
	return "/api/v4/tipax/public/all/order/parcel/label/" + hashRaw
}
