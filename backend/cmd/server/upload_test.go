package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/drup/platform/internal/auth"
)

func TestUploadProductImage_Unauthorized(t *testing.T) {
	jwtService := auth.NewJWTService("drup_jwt_secret_key_2026_dev_secure")

	// Endpoint wrapped with RequireRoles("supplier", "admin")
	handler := RequireRoles(jwtService, "supplier", "admin")(func(w http.ResponseWriter, r *http.Request) {
		HandleProductImageUpload(w, r, t.TempDir())
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload/product-image", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 Unauthorized for unauthenticated upload, got %d", w.Code)
	}
}

func TestUploadProductImage_InvalidContentMagicBytes(t *testing.T) {
	// A file with .jpg extension but containing plain text instead of JPEG magic bytes
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	part, err := w.CreateFormFile("image", "malicious_script.jpg")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	// Write plain text content
	_, _ = part.Write([]byte("<?php echo 'malicious code'; ?> This is not a JPEG"))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload/product-image", &b)
	req.Header.Set("Content-Type", w.FormDataContentType())

	rec := httptest.NewRecorder()
	HandleProductImageUpload(rec, req, t.TempDir())

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 Bad Request for text content with .jpg extension, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestUploadProductImage_ValidJPEG(t *testing.T) {
	// Valid JPEG file with magic bytes FF D8 FF E0
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	part, err := w.CreateFormFile("image", "test_camera.jpg")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	// JPEG magic bytes: FF D8 FF
	jpegBytes := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01, 0x01, 0x01, 0x00, 0x60, 0x00, 0x60, 0x00, 0x00}
	_, _ = part.Write(jpegBytes)
	w.Close()

	tempDir := t.TempDir()
	defer os.RemoveAll(tempDir)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload/product-image", &b)
	req.Header.Set("Content-Type", w.FormDataContentType())

	rec := httptest.NewRecorder()
	HandleProductImageUpload(rec, req, tempDir)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK for valid JPEG upload, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}
