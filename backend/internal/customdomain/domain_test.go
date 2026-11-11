package customdomain

import (
	"context"
	"net"
	"testing"
)

type fakeDNS struct {
	txt []string
	ips []net.IPAddr
}

func (f fakeDNS) LookupTXT(context.Context, string) ([]string, error)        { return f.txt, nil }
func (f fakeDNS) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) { return f.ips, nil }
func TestOperationsDomainProof(t *testing.T) {
	for _, d := range []string{"https://shop.example.com", "localhost", "127.0.0.1", "shop.local", "a..com", "*.example.com", "example.com/path", "-bad.com"} {
		if _, e := Normalize(d); e == nil {
			t.Fatalf("accepted %q", d)
		}
	}
	if d, e := Normalize(" Shop.Example.COM "); e != nil || d != "shop.example.com" {
		t.Fatal(d, e)
	}
	ctx := context.Background()
	f := fakeDNS{[]string{"proof"}, []net.IPAddr{{IP: net.ParseIP("203.0.113.2")}}}
	if e := Verify(ctx, f, "shop.example.com", "proof", "203.0.113.2"); e != nil {
		t.Fatal(e)
	}
	if e := Verify(ctx, f, "shop.example.com", "wrong", "203.0.113.2"); e == nil {
		t.Fatal("wrong TXT accepted")
	}
	if e := Verify(ctx, f, "shop.example.com", "proof", "203.0.113.3"); e == nil {
		t.Fatal("wrong target accepted")
	}
	f.ips = append(f.ips, net.IPAddr{IP: net.ParseIP("127.0.0.1")})
	if e := Verify(ctx, f, "shop.example.com", "proof", "203.0.113.2"); e == nil {
		t.Fatal("private AAAA/A accepted")
	}
}
