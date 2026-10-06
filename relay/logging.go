package relay

import (
	"context"
	"time"

	"github.com/rs/zerolog"
)

// Option configures the relay channel's transport and diagnostic logger.
type Option func(*config)

type config struct {
	log             zerolog.Logger
	proxyURL        string
	cancelOnClose   context.CancelFunc
	readIdleTimeout time.Duration
}

// WithReadIdleTimeout bounds a receive wait while the relay stops replying.
func WithReadIdleTimeout(timeout time.Duration) Option {
	// Source of truth: datasheets/socks5_udp.md
	return func(c *config) { c.readIdleTimeout = timeout }
}

// WithCancelOnClose releases a transport context when a successfully constructed
// channel closes. The caller retains ownership if construction fails.
func WithCancelOnClose(cancel context.CancelFunc) Option {
	return func(c *config) { c.cancelOnClose = cancel }
}

// WithSOCKS5Proxy routes relay datagrams through a SOCKS5 UDP association.
func WithSOCKS5Proxy(proxyURL string) Option {
	return func(c *config) { c.proxyURL = proxyURL }
}

func resolveConfig(opts []Option) config {
	c := config{log: zerolog.Nop()}
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

// WithLogger sets the zerolog logger for debug/trace diagnostics. The library never
// configures logging itself; without this option the channel is silent at zero cost.
// Pass the logger from a context, e.g. WithLogger(*zerolog.Ctx(ctx)).
func WithLogger(l zerolog.Logger) Option {
	return func(c *config) { c.log = l }
}

func pickLog(log []zerolog.Logger) zerolog.Logger {
	if len(log) > 0 {
		return log[0]
	}
	return zerolog.Nop()
}
