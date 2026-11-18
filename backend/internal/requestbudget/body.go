package requestbudget

import (
	"bytes"
	"errors"
	"io"
	"net/http"
)

// BodyLimit uses the same exact route classification as the upload deadline.
func BodyLimit(r *http.Request) int64 {
	if IsUpload(r) {
		if r.URL.Path == "/api/v1/upload/product-image" {
			return 10 << 20
		}
		return 48 << 20
	}
	return 2 << 20
}

// Fully validate the bounded body before entering a handler. A decoder that
// accepts only the first JSON value cannot commit before a later streamed limit
// error. Admission slots bound the number of resident bodies; no detached reader.
func readBody(w *deadlineWriter, r *http.Request, reporter RejectReporter) bool {
	limit := BodyLimit(r)
	reject := func() {
		if reporter != nil {
			reporter.RecordReject("request_body_too_large")
		}
		w.Header().Set("Connection", "close")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		io.WriteString(w, `{"code":"request_body_too_large","error":"Request body exceeds the route limit."}`)
	}
	if r.ContentLength > limit {
		reject()
		return false
	}
	if r.Body == nil || r.Body == http.NoBody {
		return true
	}
	original := r.Body
	// net/http owns and closes the original request body. Closing it here can
	// synchronously drain a chunked remainder before the 413 is delivered.
	body, err := io.ReadAll(http.MaxBytesReader(w, original, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			reject()
		} else if w.expired() {
			w.timeout()
		} else {
			w.Header().Set("Connection", "close")
			http.Error(w, "Request body could not be read", http.StatusBadRequest)
		}
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return true
}
