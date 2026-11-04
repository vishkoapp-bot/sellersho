package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// HandleProductImageUpload handles POST /api/v1/upload/product-image
// Requires authenticated supplier or admin role. Enforces strict content magic-bytes verification (fail-closed).
func HandleProductImageUpload(w http.ResponseWriter, r *http.Request, uploadDir string) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if uploadDir == "" {
		uploadDir = "./static/uploads/products"
	}

	// Limit request body to 10MB to prevent memory exhaustion
	r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024)

	err := r.ParseMultipartForm(10 * 1024 * 1024)
	if err != nil {
		http.Error(w, `{"error":"خطا در پردازش فرم یا حجم بیش از حد مجاز"}`, http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, `{"error":"فایل تصویر یافت نشد"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	// 1. Validation: File extension whitelist
	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowedExts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true}
	if !allowedExts[ext] {
		http.Error(w, `{"error":"پسوند تصویر نامعتبر است. فقط JPG، PNG و WEBP مجاز است."}`, http.StatusBadRequest)
		return
	}

	// 2. Validation: Max size 5MB
	if header.Size > 5*1024*1024 {
		http.Error(w, `{"error":"حجم فایل بیش از ۵ مگابایت است"}`, http.StatusBadRequest)
		return
	}

	// 3. Validation: Strict Magic-Bytes content inspection
	headerBuf := make([]byte, 512)
	n, readErr := file.Read(headerBuf)
	if readErr != nil && readErr != io.EOF {
		http.Error(w, `{"error":"خطا در خواندن محتوای فایل"}`, http.StatusBadRequest)
		return
	}
	headerBuf = headerBuf[:n]

	var safeExt string
	switch {
	case len(headerBuf) >= 3 && bytes.Equal(headerBuf[:3], []byte{0xFF, 0xD8, 0xFF}):
		safeExt = ".jpg"
	case len(headerBuf) >= 8 && bytes.Equal(headerBuf[:8], []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}):
		safeExt = ".png"
	case len(headerBuf) >= 12 && string(headerBuf[0:4]) == "RIFF" && string(headerBuf[8:12]) == "WEBP":
		safeExt = ".webp"
	default:
		http.Error(w, `{"error":"محتوای فایل با فرمت‌های مجاز تصویری (JPEG, PNG, WEBP) تطابق ندارد"}`, http.StatusBadRequest)
		return
	}

	// Reset stream position for copying
	if seeker, ok := file.(io.Seeker); ok {
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			http.Error(w, `{"error":"خطا در پردازش جریان فایل"}`, http.StatusInternalServerError)
			return
		}
	}

	// 4. Safe filename generation: UUID with strict validated extension, ignoring raw client filename
	filename := fmt.Sprintf("%s_%d%s", uuid.New().String(), time.Now().Unix(), safeExt)

	// Ensure upload directory exists
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		writeInternalError(w, err)
		return
	}

	destPath := filepath.Join(uploadDir, filename)

	// Write file to destination
	dst, err := os.Create(destPath)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer dst.Close()

	if _, err = io.Copy(dst, file); err != nil {
		writeInternalError(w, err)
		return
	}

	// 5. Construct public URL path
	imageURL := fmt.Sprintf("/static/uploads/products/%s", filename)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"imageUrl": imageURL,
		"filename": filename,
	})
}

