package tapin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fixedServer answers every request with the given body (HTTP 200,
// like Tapin does even for business failures).
func fixedServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCallSuccess20(t *testing.T) {
	// Register-shaped success, guide p.19.
	srv := fixedServer(t, `{"returns":{"status":20,"message":"موفق"},"entries":{"uuid":"0238b344-5bb2-451f-af09-dd4964b294a4","status":0,"contract_code":"ba219299518ir","contract_id":"12553800","total_send_price":602828}}`)
	c := NewClient(srv.URL)
	res, err := c.TipaxRegisterOrder(context.Background(), "tok", TipaxRegisterRequest{ShopID: "shop-1"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if res.ContractCode != "ba219299518ir" || res.UUID != "0238b344-5bb2-451f-af09-dd4964b294a4" || res.TotalSendPrice != 602828 {
		t.Fatalf("unexpected register result: %+v", res)
	}
}

func TestCallSuccess21IsSuccess(t *testing.T) {
	// change-status success shape, guide p.21.
	srv := fixedServer(t, `{"returns":{"status":21,"message":"عملیات با موفقیت انجام شد"},"entries":{"uuid":"3141ee07-c9e4-49d6-b465-2f785e41ab00","status":4}}`)
	c := NewClient(srv.URL)
	entries, err := c.Call(context.Background(), "tok", "/api/v4/tipax/public/user/order/change-status/cancel", map[string]string{"shop_id": "s", "order_uuid": "u"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	var got struct {
		Status int `json:"status"`
	}
	if err := json.Unmarshal(entries, &got); err != nil || got.Status != 4 {
		t.Fatalf("entries = %s", entries)
	}
}

// Real body captured live from prod (owner's shop token, 2026-11-05):
// HTTP 200 envelope, returns.status 332.
func TestCallTokenNotFound332(t *testing.T) {
	srv := fixedServer(t, `{"returns": {"status": 332, "message": "توکن مورد نظر یافت نشد."}, "entries": {}}`)
	c := NewClient(srv.URL)
	err := c.Options(context.Background(), "dead-token", "shop-1")
	ue, ok := err.(*UpstreamError)
	if !ok {
		t.Fatalf("err type = %T, want *UpstreamError (%v)", err, err)
	}
	if ue.Status != StatusTokenNotFound || ue.Message != "توکن مورد نظر یافت نشد." {
		t.Fatalf("upstream error = %+v", ue)
	}
	if !IsTokenNotFound(err) || IsServiceNotActivated(err) {
		t.Fatalf("classification wrong for 332")
	}
}

// Real body captured live from prod (token + empty body, 2026-11-05).
func TestCallNoShopSelected338(t *testing.T) {
	srv := fixedServer(t, `{"returns": {"status": 338, "message": "شما هیچ فروشگاه فعالی را انتخاب نکرده اید."}, "entries": {}}`)
	c := NewClient(srv.URL)
	_, err := c.Call(context.Background(), "tok", "/api/v4/tipax/public/user/order/options/", map[string]string{})
	ue, ok := err.(*UpstreamError)
	if !ok || ue.Status != StatusNoShopSelected {
		t.Fatalf("err = %v, want status 338", err)
	}
}

// Real body captured live from prod (valid token + shop, 2026-11-05):
// credentials are valid, the Tipax service is not activated.
func TestCallServiceNotActivated30129(t *testing.T) {
	srv := fixedServer(t, `{"returns": {"status": 30129, "message": "فروشگاه شما دسترسی استفاده از این سرویس را ندارد. جهت فعال سازی با پشتیبانی تماس حاصل فرمایید."}, "entries": {}}`)
	c := NewClient(srv.URL)
	err := c.Options(context.Background(), "tok", "21956c9d-f1ff-4ce2-956a-4fe9cf9d2f3d")
	if !IsServiceNotActivated(err) {
		t.Fatalf("err = %v, want 30129 classification", err)
	}
	ue := err.(*UpstreamError)
	if !strings.Contains(ue.Message, "فعال سازی") {
		t.Fatalf("message not verbatim: %q", ue.Message)
	}
}

// The guide's delete-failure example carries the status as a STRING
// (p.22) — the parser must accept it.
func TestCallStringStatus(t *testing.T) {
	srv := fixedServer(t, `{"returns": {"status": "99125", "message": "امکان حذف این سفارش وجود ندارد"}, "entries": null}`)
	c := NewClient(srv.URL)
	_, err := c.Call(context.Background(), "tok", "/x", nil)
	ue, ok := err.(*UpstreamError)
	if !ok || ue.Status != 99125 {
		t.Fatalf("err = %v, want status 99125", err)
	}
}

func TestCheckPriceParsesGuideExample(t *testing.T) {
	// Guide p.18 response, verbatim figures.
	srv := fixedServer(t, `{"returns":{"status":20,"message":"موفق"},"entries":{"send_price":618636,"send_tax":30896,"total_send_price":618636,"total_product_price":10000000,"total_panel_price":330000,"total_receive_price":948636,"total_weight":2000,"total_discount":0,"courier_discount":0}}`)
	c := NewClient(srv.URL)
	res, err := c.TipaxCheckPrice(context.Background(), "tok", TipaxCheckPriceRequest{ShopID: "s"})
	if err != nil {
		t.Fatalf("check price: %v", err)
	}
	if res.TotalSendPrice != 618636 || res.TotalPanelPrice != 330000 || res.TotalWeight != 2000 {
		t.Fatalf("result = %+v", res)
	}
}

func TestCallSendsShopAndAuthHeader(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"returns":{"status":20,"message":"موفق"},"entries":{}}`))
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	if err := c.Options(context.Background(), "secret-token", "shop-42"); err != nil {
		t.Fatalf("options: %v", err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if !strings.Contains(gotBody, `"shop_id":"shop-42"`) {
		t.Fatalf("body = %q, want shop_id inside", gotBody)
	}
}

func TestExtractHashRawShapes(t *testing.T) {
	// Guide p.31: single order object.
	if got := ExtractHashRaw(json.RawMessage(`{"uuid":"u","hash_raw":"abc123"}`)); got != "abc123" {
		t.Fatalf("single: %q", got)
	}
	if got := ExtractHashRaw(json.RawMessage(`[{"uuid":"u","hash_raw":"def456"}]`)); got != "def456" {
		t.Fatalf("list: %q", got)
	}
	if got := ExtractHashRaw(json.RawMessage(`{"list":[{"hash_raw":"ghi789"}],"page":1}`)); got != "ghi789" {
		t.Fatalf("wrapped: %q", got)
	}
	if got := ExtractHashRaw(json.RawMessage(`{"uuid":"u"}`)); got != "" {
		t.Fatalf("missing: %q", got)
	}
	if got := ExtractHashRaw(nil); got != "" {
		t.Fatalf("nil: %q", got)
	}
}

func TestRegisterWithoutContractCodeFails(t *testing.T) {
	srv := fixedServer(t, `{"returns":{"status":20,"message":"موفق"},"entries":{"uuid":"u","status":0}}`)
	c := NewClient(srv.URL)
	if _, err := c.TipaxRegisterOrder(context.Background(), "tok", TipaxRegisterRequest{}); err == nil {
		t.Fatalf("expected error for missing contract_code")
	}
}
