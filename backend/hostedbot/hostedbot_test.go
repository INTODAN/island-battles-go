package hostedbot

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

// fakeResolver answers from a table, without a network.
type fakeResolver map[string][]string

func (resolver fakeResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	answers, found := resolver[host]
	if !found {
		return nil, errors.New("no such host")
	}
	addresses := make([]netip.Addr, 0, len(answers))
	for _, answer := range answers {
		addresses = append(addresses, netip.MustParseAddr(answer))
	}
	return addresses, nil
}

var resolver = fakeResolver{
	"bot.example.com":       {"203.0.113.7"},
	"dual.example.com":      {"203.0.113.8", "2001:db8::8"},
	"ten.example.com":       {"10.0.0.1"},
	"loop6.example.com":     {"::1"},
	"loop4.example.com":     {"127.0.0.1"},
	"mixed.example.com":     {"203.0.113.9", "192.168.1.20"},
	"linklocal.example":     {"169.254.169.254"},
	"uniquelocal.example":   {"fd12:3456::1"},
	"mapped.example.com":    {"::ffff:10.1.2.3"},
	"unspecified.example":   {"0.0.0.0"},
	"linklocal6.example":    {"fe80::1"},
	"private172.example":    {"172.16.4.4"},
	"empty.example.com":     {},
	"localhost":             {"127.0.0.1"},
	"bot.example.com.":      {"203.0.113.7"},
	"upper.example.com":     {"203.0.113.10"},
	"public6.example.com":   {"2606:4700::1111"},
	"carrier.example.com":   {"100.64.0.1"},
	"multicast.example.com": {"224.0.0.1"},
}

func TestCheckAcceptsHTTPSURLsOnPublicHosts(t *testing.T) {
	for raw, wantHost := range map[string]string{
		"https://bot.example.com/turn":            "bot.example.com",
		"https://bot.example.com":                 "bot.example.com",
		"https://bot.example.com:443/turn?team=1": "bot.example.com",
		"https://dual.example.com/a/b":            "dual.example.com",
		"https://UPPER.example.com/turn":          "upper.example.com",
		"HTTPS://bot.example.com/turn":            "bot.example.com",
		"https://public6.example.com/":            "public6.example.com",
		// Carrier-grade NAT space is not one of the ranges refused.
		"https://carrier.example.com/turn": "carrier.example.com",
	} {
		host, err := Check(context.Background(), resolver, raw)
		if err != nil || host != wantHost {
			t.Errorf("Check(%q) = %q, %v; want %q", raw, host, err, wantHost)
		}
	}
}

func TestCheckRefusesEveryRule(t *testing.T) {
	for raw, wantReason := range map[string]string{
		// Scheme.
		"http://bot.example.com/turn": "must start with https://",
		"ftp://bot.example.com/turn":  "must start with https://",
		"bot.example.com/turn":        "must start with https://",
		"//bot.example.com/turn":      "must start with https://",
		"https:bot.example.com":       "not a URL",
		// IP literals.
		"https://203.0.113.7/turn":        "not an IP address",
		"https://10.0.0.1/turn":           "not an IP address",
		"https://[::1]/turn":              "not an IP address",
		"https://[2001:db8::8]:443/turn":  "not an IP address",
		"https://[fe80::1%25eth0]/turn":   "not an IP address",
		"https://[::ffff:127.0.0.1]/turn": "not an IP address",
		// Names that resolve to an address on a private network.
		"https://ten.example.com/turn":       "resolves to 10.0.0.1",
		"https://loop6.example.com/turn":     "resolves to ::1",
		"https://loop4.example.com/turn":     "resolves to 127.0.0.1",
		"https://mixed.example.com/turn":     "resolves to 192.168.1.20",
		"https://linklocal.example/turn":     "resolves to 169.254.169.254",
		"https://uniquelocal.example/turn":   "resolves to fd12:3456::1",
		"https://mapped.example.com/turn":    "resolves to 10.1.2.3",
		"https://unspecified.example/turn":   "resolves to 0.0.0.0",
		"https://linklocal6.example/turn":    "resolves to fe80::1",
		"https://private172.example/turn":    "resolves to 172.16.4.4",
		"https://localhost/turn":             "resolves to 127.0.0.1",
		"https://bot.example.com./turn":      "",
		"https://multicast.example.com/turn": "resolves to 224.0.0.1",
		"https://missing.example.com/turn":   "could not be looked up",
		"https://empty.example.com/turn":     "has no address",
		// Credentials.
		"https://user:secret@bot.example.com/turn": "user name or password",
		"https://user@bot.example.com/turn":        "user name or password",
		// Ports.
		"https://bot.example.com:8443/turn": "port 443",
		"https://bot.example.com:80/turn":   "port 443",
		"https://bot.example.com:/turn":     "port 443",
		// No host.
		"https:///turn": "names no host",
		// Length.
		"https://bot.example.com/" + strings.Repeat("a", MaxURLLength): "longer than 2048 characters",
	} {
		host, err := Check(context.Background(), resolver, raw)
		if raw == "https://bot.example.com./turn" {
			// A name with its root dot resolves like the name; this one is public.
			if err != nil {
				t.Errorf("Check(%q) = %v", raw, err)
			}
			continue
		}
		if err == nil || !IsInvalid(err) || !strings.Contains(err.Error(), wantReason) {
			t.Errorf("Check(%q) = %q, %v; want a refusal that says %q", raw, host, err, wantReason)
		}
	}
}

// A URL of exactly the longest length is accepted.
func TestCheckAcceptsTheLongestURL(t *testing.T) {
	prefix := "https://bot.example.com/"
	raw := prefix + strings.Repeat("a", MaxURLLength-len(prefix))
	if _, err := Check(context.Background(), resolver, raw); err != nil {
		t.Fatalf("a URL of %d characters: %v", len(raw), err)
	}
}

// A cancelled request is reported as such, not as a refused URL.
func TestCheckReportsACancelledLookup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Check(ctx, resolver, "https://missing.example.com/turn")
	if !errors.Is(err, context.Canceled) || IsInvalid(err) {
		t.Fatalf("err = %v", err)
	}
}
