package main

// Storage-fee manual run (gap 7): POST /api/v1/admin/storage-fees/run
// executes one bounded pass over known warehouse receipt histories, including
// pending accrual on closed positions, and returns its summary. The engine itself lives in the
// repository (SupplierRepository.RunStorageFeeAssessment) as a plain
// callable, so a scheduler could invoke it daily later; no scheduler
// registration ships with this change. While the storage-fee flag is
// OFF (the shipped default; activation is the owner's decision) the
// run refuses with a Persian message and charges nothing.

import (
	"errors"
	"net/http"

	"github.com/drup/platform/internal/repository"
)

func handleAdminStorageFeeRun(w http.ResponseWriter, r *http.Request, supplierRepo *repository.SupplierRepository) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	summary, err := supplierRepo.RunStorageFeeAssessment(r.Context())
	if errors.Is(err, repository.ErrStorageFeeDisabled) {
		http.Error(w, `{"error":"هزینه انبارداری فعال نیست؛ ابتدا آن را از تنظیمات هزینه انبارداری فعال کنید."}`, http.StatusConflict)
		return
	}
	if err != nil {
		writeInternalError(w, err)
		return
	}
	t01WriteJSON(w, http.StatusOK, summary)
}
