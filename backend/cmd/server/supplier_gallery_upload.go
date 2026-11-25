package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/drup/platform/internal/repository"
	"github.com/google/uuid"
)

func (h *SupplierHandlers) uploadSupplierGallery(w http.ResponseWriter, r *http.Request, uploadDir, supplierID, productID, variantID string) {
	if uploadDir == "" {
		uploadDir = "./static/uploads/products"
	}
	r.Body = http.MaxBytesReader(w, r.Body, 48*1024*1024)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, `{"error":"خطا در پردازش فرم یا حجم بیش از حد مجاز"}`, http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()
	files := r.MultipartForm.File["images"]
	if len(files) == 0 {
		http.Error(w, `{"error":"فایل تصویر یافت نشد"}`, http.StatusBadRequest)
		return
	}

	// Validate the entire batch before any file enters gallery storage.
	extensions := make([]string, 0, len(files))
	for _, file := range files {
		if file.Size > 5*1024*1024 {
			http.Error(w, `{"error":"حجم هر فایل نباید بیشتر از ۵ مگابایت باشد."}`, http.StatusBadRequest)
			return
		}
		src, err := file.Open()
		if err != nil {
			writeInternalError(w, err)
			return
		}
		var header [512]byte
		n, err := io.ReadFull(src, header[:])
		src.Close()
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			writeInternalError(w, err)
			return
		}
		ext, valid := fix47ImageExt(header[:n], file.Filename)
		if !valid {
			http.Error(w, `{"error":"فرمت نامعتبر. فقط JPG، PNG و WEBP مجاز است."}`, http.StatusBadRequest)
			return
		}
		extensions = append(extensions, ext)
	}

	ctx := r.Context()
	var checkErr error
	if variantID == "" {
		checkErr = h.supplierRepo.CheckProductGalleryRoom(ctx, supplierID, productID, len(files))
	} else {
		checkErr = h.supplierRepo.CheckVariantGalleryRoom(ctx, supplierID, productID, variantID, len(files))
	}
	if checkErr != nil {
		if !writeSupplierGalleryRejection(w, checkErr, variantID != "") {
			writeInternalError(w, checkErr)
		}
		return
	}
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		writeInternalError(w, err)
		return
	}

	paths := make([]string, 0, len(files))
	urls := make([]string, 0, len(files))
	keepFiles := false
	defer func() {
		if !keepFiles {
			for _, path := range paths {
				_ = os.Remove(path)
			}
		}
	}()
	for i, file := range files {
		src, err := file.Open()
		if err != nil {
			writeInternalError(w, err)
			return
		}
		name := uuid.NewString() + extensions[i]
		path := filepath.Join(uploadDir, name)
		dst, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			src.Close()
			writeInternalError(w, err)
			return
		}
		paths = append(paths, path)
		_, copyErr := io.Copy(dst, src)
		closeErr := dst.Close()
		src.Close()
		if err := errors.Join(copyErr, closeErr); err != nil {
			writeInternalError(w, err)
			return
		}
		urls = append(urls, "/static/uploads/products/"+name)
	}

	// Keep the candidate files on ambiguous DB errors, such as a lost commit
	// response. The gallery repository reports known pre-commit rejections
	// separately so the request can remove those unreferenced files.
	keepFiles = true
	var list []repository.SupplierProductImage
	var err error
	if variantID == "" {
		list, err = h.supplierRepo.AddProductImages(ctx, supplierID, productID, urls)
	} else {
		list, err = h.supplierRepo.AddVariantImages(ctx, supplierID, productID, variantID, urls)
	}
	if err != nil {
		if writeSupplierGalleryRejection(w, err, variantID != "") {
			keepFiles = false
		} else {
			writeInternalError(w, err)
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"images": list})
}

func writeSupplierGalleryRejection(w http.ResponseWriter, err error, variant bool) bool {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if variant {
			http.Error(w, `{"error":"variant not found"}`, http.StatusNotFound)
		} else {
			http.Error(w, `{"error":"product not found"}`, http.StatusNotFound)
		}
	case errors.Is(err, repository.ErrGalleryFull):
		http.Error(w, `{"error":"حداکثر ۸ تصویر برای هر محصول مجاز است."}`, http.StatusBadRequest)
	case errors.Is(err, repository.ErrVariantGalleryFull):
		http.Error(w, `{"error":"حداکثر ۴ تصویر برای هر تنوع مجاز است."}`, http.StatusBadRequest)
	default:
		return false
	}
	return true
}
