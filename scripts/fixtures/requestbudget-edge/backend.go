// Local-only upstream for proxy budget acceptance. No database or provider access.
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"
)

func main() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
		delay, _ := strconv.Atoi(r.Header.Get("X-Synthetic-Delay-Ms"))
		if delay < 0 || delay > 20000 {
			w.WriteHeader(400)
			return
		}
		if delay > 0 {
			select {
			case <-time.After(time.Duration(delay) * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"method": r.Method, "path": r.URL.Path, "receivedBody": string(body)})
	})
	server := &http.Server{Addr: ":8080", Handler: handler, ReadHeaderTimeout: 3 * time.Second, WriteTimeout: 25 * time.Second}
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}
}
