package meowcaller

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"time"

	"github.com/purpshell/meowcaller/relay"
	"github.com/rs/zerolog"
)

// connectRelayCandidates retries the other family through the same dialer.
func connectRelayCandidates(ctx context.Context, ep *relayEndpoint, preferIPv6 bool, dial func(context.Context, netip.AddrPort) (*relay.RelayMediaChannel, error)) (*relay.RelayMediaChannel, netip.AddrPort, error) {
	// Source of truth: datasheets/ipv6-relay.md
	selected, err := selectMediaRelayAddress(ep, preferIPv6)
	if err != nil {
		return nil, netip.AddrPort{}, err
	}
	ch, err := dial(ctx, selected)
	if err == nil {
		return ch, selected, nil
	}
	var transportErr *relay.CallTransportError
	if !preferIPv6 || !selected.Addr().Is6() || ctx.Err() != nil || !errors.As(err, &transportErr) {
		return nil, selected, err
	}
	alternative, selectionErr := selectMediaRelayAddress(ep, false)
	if selectionErr != nil || !alternative.Addr().Is4() {
		return nil, selected, err
	}
	ch, err = dial(ctx, alternative)
	return ch, alternative, err
}

// connectMediaRelay bounds setup while leaving successful transport alive.
func connectMediaRelay(ctx context.Context, selected netip.AddrPort, proxyURL string, log zerolog.Logger) (*relay.RelayMediaChannel, error) {
	// Source of truth: datasheets/socks5_udp.md
	connectCtx, cancel := context.WithCancel(ctx)
	type result struct {
		ch  *relay.RelayMediaChannel
		err error
	}
	done := make(chan result)
	go func() {
		ch, err := relay.ConnectRelayMediaContext(connectCtx, net.UDPAddrFromAddrPort(selected), relay.WithLogger(log), relay.WithSOCKS5Proxy(proxyURL), relay.WithCancelOnClose(cancel), relay.WithReadIdleTimeout(30*time.Second))
		select {
		case done <- result{ch, err}:
		case <-connectCtx.Done():
			if ch != nil {
				_ = ch.Close()
			}
		}
	}()
	timer := time.NewTimer(12 * time.Second)
	defer timer.Stop()
	select {
	case r := <-done:
		if r.err != nil {
			cancel()
		}
		return r.ch, r.err
	case <-timer.C:
		cancel()
		return nil, &relay.CallTransportError{Op: "connect", Err: errors.New("relay setup timed out")}
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}
}
