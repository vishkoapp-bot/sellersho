package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLifecycleTimeoutConfiguration(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	for _, key := range []string{"STARTUP_TIMEOUT_SECONDS", "SHUTDOWN_GRACE_SECONDS"} {
		old, ok := os.LookupEnv(key)
		os.Unsetenv(key)
		t.Cleanup(func() {
			if ok {
				os.Setenv(key, old)
			} else {
				os.Unsetenv(key)
			}
		})
	}
	cfg, err := LoadConfig()
	if err != nil || cfg.StartupTimeout != 30*time.Second || cfg.ShutdownGrace != 20*time.Second {
		t.Fatal("invalid lifecycle timeout defaults or configuration error")
	}
	for _, c := range []struct{ key, value string }{{"STARTUP_TIMEOUT_SECONDS", "0"}, {"STARTUP_TIMEOUT_SECONDS", "601"}, {"STARTUP_TIMEOUT_SECONDS", "secret-marker"}, {"SHUTDOWN_GRACE_SECONDS", "26"}, {"SHUTDOWN_GRACE_SECONDS", ""}, {"SHUTDOWN_GRACE_SECONDS", "1.5"}} {
		t.Run(c.key+c.value, func(t *testing.T) {
			t.Setenv(c.key, c.value)
			_, err := LoadConfig()
			if err == nil || !strings.Contains(err.Error(), c.key) || strings.Contains(err.Error(), "secret-marker") {
				t.Fatal("unsafe timeout validation", err)
			}
		})
	}
	for _, c := range []struct{ startup, shutdown string }{{"1", "1"}, {"600", "25"}} {
		t.Setenv("STARTUP_TIMEOUT_SECONDS", c.startup)
		t.Setenv("SHUTDOWN_GRACE_SECONDS", c.shutdown)
		if _, err := LoadConfig(); err != nil {
			t.Fatal(err)
		}
	}
}
