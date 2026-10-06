package meowcaller

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/purpshell/meowcaller/relay"
)

func TestConnectRelayCandidatesRetriesIPv4AfterIPv6TransportFailure(t *testing.T) {
	ep := &relayEndpoint{addresses: []relayAddress{{ipv6: "2001:db8::1", ipv4: "192.0.2.1", port: 3478}}}
	want := &relay.RelayMediaChannel{}
	var attempts []netip.AddrPort
	ch, selected, err := connectRelayCandidates(context.Background(), ep, true, func(_ context.Context, addr netip.AddrPort) (*relay.RelayMediaChannel, error) {
		attempts = append(attempts, addr)
		if addr.Addr().Is6() {
			return nil, &relay.CallTransportError{Op: "connect", Err: errors.New("unreachable")}
		}
		return want, nil
	})
	if err != nil || ch != want || !selected.Addr().Is4() || len(attempts) != 2 || !attempts[0].Addr().Is6() {
		t.Fatalf("ch=%v selected=%v attempts=%v err=%v", ch, selected, attempts, err)
	}
}

func TestConnectRelayCandidatesDoesNotRetryCancellationOrNonTransportErrors(t *testing.T) {
	ep := &relayEndpoint{addresses: []relayAddress{{ipv6: "2001:db8::1", ipv4: "192.0.2.1", port: 3478}}}
	for _, tc := range []struct {
		name   string
		proxy  bool
		cancel bool
		err    error
	}{
		{"direct", false, false, &relay.CallTransportError{Op: "connect", Err: errors.New("failed")}},
		{"canceled", true, true, &relay.CallTransportError{Op: "connect", Err: errors.New("failed")}},
		{"configuration", true, false, errors.New("invalid configuration")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			attempts := 0
			_, _, err := connectRelayCandidates(ctx, ep, tc.proxy, func(context.Context, netip.AddrPort) (*relay.RelayMediaChannel, error) {
				attempts++
				if tc.cancel {
					cancel()
				}
				return nil, tc.err
			})
			if attempts != 1 || err == nil {
				t.Fatalf("attempts=%d err=%v", attempts, err)
			}
		})
	}
}
