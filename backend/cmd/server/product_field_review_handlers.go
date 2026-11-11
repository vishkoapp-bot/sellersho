package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/drup/platform/internal/repository"
	"net/http"
	"strings"
)

func handleProductFieldReview(w http.ResponseWriter, r *http.Request, repo *repository.SupplierRepository) {
	w.Header().Set("Content-Type", "application/json")
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/product-field-reviews/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	fail := func(e error) {
		status := http.StatusBadRequest
		if errors.Is(e, sql.ErrNoRows) {
			status = 404
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"error": e.Error()})
	}
	if r.Method == http.MethodPut {
		var body struct {
			Fields []repository.FieldDecision `json:"fields"`
		}
		if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&body); e != nil {
			fail(e)
			return
		}
		if e := repo.SaveFieldReview(r.Context(), id, callerUserID(r), body.Fields); e != nil {
			fail(e)
			return
		}
	} else if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	data, e := repo.GetAdminFieldReview(r.Context(), id)
	if e != nil {
		fail(e)
		return
	}
	json.NewEncoder(w).Encode(data)
}
