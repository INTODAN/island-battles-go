package testutils

import (
	"context"
	"errors"
	"net/netip"
)

// HostTable resolves host names from a table, as a hostedbot.Resolver, so
// no test looks up a real name. A name it doesn't list has no address.
type HostTable map[string]string

func (table HostTable) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	address, found := table[host]
	if !found {
		return nil, errors.New("no such host")
	}
	return []netip.Addr{netip.MustParseAddr(address)}, nil
}

// BotHosts are the hosts of the tests' hosted bots: two public ones, and
// two that a hosted bot can't use, on a private network and on this
// machine.
var BotHosts = HostTable{
	"bot.example.com":     "203.0.113.7",
	"seat.example.com":    "2001:db8::7",
	"private.example.com": "10.0.0.1",
	"loop.example.com":    "::1",
}
