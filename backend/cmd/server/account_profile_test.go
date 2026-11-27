package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/drup/platform/internal/auth"
)

type testAccountNames map[string]string

func (names testAccountNames) GetProfileName(_ context.Context, id string) (string, error) {
	return names[id], nil
}

func (names testAccountNames) SetProfileName(_ context.Context, id, name string) (string, error) {
	names[id] = name
	return name, nil
}

func TestAccountProfileUsesCurrentNameAndOnlyUpdatesAuthenticatedUser(t *testing.T) {
	names := testAccountNames{"account": legacyProfileName, "other": "حساب دیگر"}
	profile := handleAccountProfile(names)
	claims := &auth.UserClaims{UserID: "account", FullName: legacyProfileName, Role: "customer", Capabilities: []string{"customer", "seller"}}
	request := func(method, payload string, signedIn bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v1/auth/me", strings.NewReader(payload))
		if signedIn {
			r = r.WithContext(context.WithValue(r.Context(), userClaimsKey, claims))
		}
		w := httptest.NewRecorder()
		profile(w, r)
		return w
	}
	assertName := func(w *httptest.ResponseRecorder, expected string) {
		t.Helper()
		var got auth.UserClaims
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.FullName != expected || !got.HasCapability("seller") {
			t.Fatalf("status=%d body=%s, want name %q with seller capability", w.Code, w.Body.String(), expected)
		}
	}
	assertName(request(http.MethodGet, "", true), "")
	payload := `{"fullName":"  مریم احمدی  "}`
	if got := request(http.MethodPatch, payload, false).Code; got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated patch status=%d", got)
	}
	if got := request(http.MethodPatch, `{"fullName":"نام دیگر","userId":"other"}`, true).Code; got != http.StatusBadRequest {
		t.Fatalf("patch with unknown fields status=%d", got)
	}
	assertName(request(http.MethodPatch, payload, true), "مریم احمدی")
	assertName(request(http.MethodGet, "", true), "مریم احمدی")
	if names["other"] != "حساب دیگر" || claims.FullName != legacyProfileName {
		t.Fatal("changing profile name modified another account or the original token claims")
	}
}
