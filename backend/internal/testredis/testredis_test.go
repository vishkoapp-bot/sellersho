package testredis

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestIdentityRejectsUnsafeTargets(t *testing.T) {
	secret := strings.Repeat("b", 48)
	token := strings.Repeat("a", 24)
	valid := "redis://:" + secret + "@127.0.0.1:49152/15"
	if _, err := Identity(valid, token); err != nil {
		t.Fatal(err)
	}
	for _, dsn := range []string{"", "redis://127.0.0.1:6379/0", strings.Replace(valid, "127.0.0.1", "localhost", 1), strings.Replace(valid, "/15", "/0", 1), valid + "?db=0", valid + "#fragment", strings.Replace(valid, ":49152", ":6379", 1)} {
		if _, err := Identity(dsn, token); err == nil || strings.Contains(err.Error(), secret) {
			t.Fatal("unsafe target accepted or credential leaked")
		}
	}
	if _, err := Identity(valid, ""); err == nil {
		t.Fatal("missing disposable marker accepted")
	}
}

func TestUnsafeTargetAndMissingOptInNeverConnect(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for _, mode := range []string{"no-opt-in", "invalid-explicit"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRedisGuardChild$")
		cmd.Env = append(os.Environ(), "SELLERSHO_REDIS_GUARD_CHILD="+mode, "REDIS_URL=redis://"+listener.Addr().String()+"/0", "REDIS_GUARD_SENTINEL="+listener.Addr().String())
		out, err := cmd.CombinedOutput()
		if mode == "no-opt-in" && err != nil {
			t.Fatalf("absent opt-in failed: %s", out)
		}
		if mode == "invalid-explicit" && (err == nil || !strings.Contains(string(out), "test Redis requires")) {
			t.Fatalf("unsafe target not rejected: %s", out)
		}
	}
	listener.(*net.TCPListener).SetDeadline(time.Now().Add(100 * time.Millisecond))
	if conn, err := listener.Accept(); err == nil {
		conn.Close()
		t.Fatal("test connected to application Redis sentinel")
	}
}

func TestRedisGuardChild(t *testing.T) {
	switch os.Getenv("SELLERSHO_REDIS_GUARD_CHILD") {
	case "no-opt-in":
		Open(t, "", "")
	case "invalid-explicit":
		Open(t, "redis://"+os.Getenv("REDIS_GUARD_SENTINEL")+"/0", "")
	case "wrong-marker":
		Open(t, os.Getenv("SELLERSHO_TEST_REDIS_URL"), os.Getenv("REDIS_GUARD_WRONG_TOKEN"))
	}
}

func TestLiveMarkerMismatchRefusesTarget(t *testing.T) {
	dsn, token := os.Getenv("SELLERSHO_TEST_REDIS_URL"), os.Getenv("SELLERSHO_TEST_REDIS_TOKEN")
	client := Open(t, dsn, token)
	wrong := strings.Repeat("a", 24)
	if token == wrong {
		wrong = strings.Repeat("b", 24)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRedisGuardChild$")
	cmd.Env = append(os.Environ(), "SELLERSHO_REDIS_GUARD_CHILD=wrong-marker", "REDIS_GUARD_WRONG_TOKEN="+wrong)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "disposable identity/marker verification failed") {
		t.Fatal("unmatched Redis marker did not fail closed")
	}
	if strings.Contains(string(out), dsn) {
		t.Fatal("Redis connection secret leaked")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if actual, err := client.Get(ctx, MarkerKey).Result(); err != nil || actual != token {
		t.Fatal("refused request changed disposable marker")
	}
}
