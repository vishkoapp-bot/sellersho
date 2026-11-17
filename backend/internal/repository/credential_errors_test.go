package repository

import (
	"strings"
	"testing"
)

func TestMalformedDependencyCredentialsAreNeverReturned(t *testing.T) {
	sentinel := "synthetic-source-credential-canary"
	for _, tc := range []struct {
		name string
		run  func() error
	}{{"postgres", func() error {
		_, e := NewPostgresDB("postgres://user:" + sentinel + "%zz@127.0.0.1:1/fixture")
		return e
	}}, {"redis", func() error { _, e := NewRedisClient("redis://:" + sentinel + "%zz@127.0.0.1:1/0"); return e }}} {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.run()
			if e == nil {
				t.Fatal("malformed input accepted")
			}
			if strings.Contains(e.Error(), sentinel) {
				t.Fatal("dependency error exposed credential value")
			}
		})
	}
}
