package customdomain

import (
	"context"
	"errors"
	"net"
	"regexp"
	"strings"
)

var label = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func Normalize(raw string) (string, error) {
	d := strings.ToLower(strings.TrimSpace(raw))
	if len(d) > 253 || len(d) < 4 || strings.ContainsAny(d, "/:@?#\\\x00") || net.ParseIP(d) != nil {
		return "", errors.New("نام دامنه معتبر نیست؛ فقط نام دامنه بدون مسیر و پروتکل را وارد کنید")
	}
	parts := strings.Split(d, ".")
	if len(parts) < 2 {
		return "", errors.New("public domain required")
	}
	for _, p := range parts {
		if !label.MatchString(p) {
			return "", errors.New("invalid domain label; use the punycode form for international domains")
		}
	}
	if len(parts[len(parts)-1]) < 2 || strings.ContainsAny(parts[len(parts)-1], "0123456789") || strings.HasSuffix(d, ".localhost") || strings.HasSuffix(d, ".local") || strings.HasSuffix(d, ".internal") {
		return "", errors.New("public domain required")
	}
	return d, nil
}

type Resolver interface {
	LookupTXT(context.Context, string) ([]string, error)
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

// DNS ownership AND routing are both required. No HTTP request to an
// untrusted hostname is made (there is no SSRF verification endpoint).
func Verify(ctx context.Context, r Resolver, domain, token, edgeHost string) error {
	txt, err := r.LookupTXT(ctx, "_sellersho-verification."+domain)
	if err != nil {
		return errors.New("رکورد TXT تأیید مالکیت پیدا نشد")
	}
	found := false
	for _, v := range txt {
		if v == token {
			found = true
		}
	}
	if !found {
		return errors.New("رکورد TXT با کد تأیید مطابقت ندارد")
	}
	if edgeHost == "" {
		return errors.New("نشانی سرور دامنه اختصاصی هنوز پیکربندی نشده است")
	}
	edge, err := r.LookupIPAddr(ctx, edgeHost)
	if ip := net.ParseIP(edgeHost); ip != nil {
		edge = []net.IPAddr{{IP: ip}}
		err = nil
	}
	if err != nil || len(edge) == 0 {
		return errors.New("نشانی سرور دامنه قابل resolve نیست")
	}
	ips, err := r.LookupIPAddr(ctx, domain)
	if err != nil || len(ips) == 0 {
		return errors.New("رکورد A یا AAAA دامنه پیدا نشد")
	}
	for _, ip := range ips {
		match := false
		if ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() || !ip.IP.IsGlobalUnicast() {
			return errors.New("دامنه باید به سرور عمومی اشاره کند")
		}
		for _, e := range edge {
			if ip.IP.Equal(e.IP) {
				match = true
			}
		}
		if !match {
			return errors.New("تمام رکوردهای A و AAAA باید به سرور پلتفرم اشاره کنند")
		}
	}
	return nil
}
