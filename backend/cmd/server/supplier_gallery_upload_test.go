package main

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestSupplierProductGalleryRejectedBatchLeavesNoFiles(t *testing.T) {
	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	valid, err := multipartWriter.CreateFormFile("images", "a.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(valid, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	invalid, err := multipartWriter.CreateFormFile("images", "b.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invalid.Write([]byte("not an image")); err != nil {
		t.Fatal(err)
	}
	if err := multipartWriter.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/supplier/products/product/images", &body)
	r.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	r = r.WithContext(context.WithValue(r.Context(), supplierIDKey, "supplier"))
	w := httptest.NewRecorder()
	uploadDir := t.TempDir()
	(&SupplierHandlers{}).HandleUploadProductImages(w, r, "product", uploadDir)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("rejected batch status = %d, want 400", w.Code)
	}
	files, err := os.ReadDir(uploadDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("invalid batch left %d file(s) in the product gallery storage", len(files))
	}
}
