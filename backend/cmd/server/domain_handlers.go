package main

import (
	"context"
	"encoding/json"
	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/customdomain"
	"github.com/drup/platform/internal/observability"
	"github.com/drup/platform/internal/repository"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func handleOwnedDomains(w http.ResponseWriter, r *http.Request, repo *repository.StoreRepository) {
	claims := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	if repo == nil {
		http.Error(w, "Unavailable", 503)
		return
	}
	switch r.Method {
	case http.MethodGet:
		rows, e := repo.GetOwnedDomains(r.Context(), claims.UserID)
		if e != nil {
			writeInternalError(w, e)
			return
		}
		t01WriteJSON(w, 200, rows)
	case http.MethodPost:
		var p struct {
			Slug   string `json:"slug"`
			Domain string `json:"domain"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&p) != nil {
			http.Error(w, "Invalid domain", 400)
			return
		}
		d, e := repo.RequestDomain(r.Context(), claims.UserID, p.Slug, p.Domain)
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		t01WriteJSON(w, 201, d)
	default:
		writeMethodNotAllowed(w)
	}
}
func handleOwnedDomainAction(w http.ResponseWriter, r *http.Request, repo *repository.StoreRepository) {
	claims := r.Context().Value(userClaimsKey).(*auth.UserClaims)
	raw := strings.TrimPrefix(r.URL.Path, "/api/v1/seller/domains/")
	verify := strings.HasSuffix(raw, "/verify")
	raw = strings.TrimSuffix(raw, "/verify")
	domain, e := customdomain.Normalize(raw)
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if verify && r.Method == http.MethodPost {
		e = repo.VerifyOwnedDomain(ctx, claims.UserID, domain, os.Getenv("DOMAIN_EDGE_HOST"), net.DefaultResolver)
	} else if !verify && r.Method == http.MethodDelete {
		e = repo.DisableOwnedDomain(ctx, claims.UserID, domain)
	} else {
		writeMethodNotAllowed(w)
		return
	}
	if e != nil {
		http.Error(w, e.Error(), 409)
		return
	}
	t01WriteJSON(w, 200, map[string]bool{"success": true})
}
func handleDomainPermission(w http.ResponseWriter, r *http.Request, repo *repository.StoreRepository) {
	// EDGE-01 step 1: every decision is counted (allowed/denied/error)
	// and the handler latency is histogrammed. The duration defer
	// covers all invocations including 405s, so the histogram is the
	// handler's latency, not only the decision paths'; 405s on this
	// endpoint are ~zero in practice (Caddy only sends GET).
	start := time.Now()
	defer func() { observability.Default.ObserveDomainAllow(time.Since(start)) }()
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	d, e := customdomain.Normalize(r.URL.Query().Get("domain"))
	if e != nil {
		observability.Default.RecordDomainAllow("denied")
		http.Error(w, "Domain denied", 403)
		return
	}
	for _, host := range strings.Split(os.Getenv("PLATFORM_DOMAINS"), ",") {
		if strings.TrimSpace(host) == d {
			observability.Default.RecordDomainAllow("allowed")
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	if repo == nil {
		observability.Default.RecordDomainAllow("error")
		http.Error(w, "Unavailable", 503)
		return
	}
	if _, e = repo.AllowedDomain(r.Context(), d); e != nil {
		observability.Default.RecordDomainAllow("denied")
		http.Error(w, "Domain denied", 403)
		return
	}
	observability.Default.RecordDomainAllow("allowed")
	w.WriteHeader(http.StatusNoContent)
}
func handleDomainStore(w http.ResponseWriter, r *http.Request, repo *repository.StoreRepository) {
	if repo == nil {
		http.Error(w, "Unavailable", http.StatusServiceUnavailable)
		return
	}
	host := r.Host
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	d, e := customdomain.Normalize(host)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	slug, e := repo.AllowedDomain(r.Context(), d)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	t01WriteJSON(w, 200, map[string]string{"slug": slug})
}
