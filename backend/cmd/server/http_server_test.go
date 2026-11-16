package main

import (
	"net"
	"net/http"
	"testing"
	"time"
)

func TestProductionSlowHeaderSocketDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newHTTPServer("", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("partial header reached handler") }))
	go server.Serve(listener)
	defer server.Close()
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	start := time.Now()
	conn.SetDeadline(start.Add(6500 * time.Millisecond))
	if _, err = conn.Write([]byte("GET /api/live HTTP/1.1\r\nHost: fixture\r\nX-Slow: ")); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	_, err = conn.Read(b[:])
	elapsed := time.Since(start)
	if elapsed < 4*time.Second || elapsed > 6*time.Second {
		t.Fatal("slow header escaped production5s socket budget", elapsed, err)
	}
	if server.ReadTimeout != 65*time.Second || server.WriteTimeout != 70*time.Second || server.IdleTimeout != 60*time.Second || server.MaxHeaderBytes != 64<<10 {
		t.Fatal("production socket ceilings changed")
	}
}
