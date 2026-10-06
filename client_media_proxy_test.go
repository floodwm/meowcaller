package meowcaller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/purpshell/meowcaller/relay"
)

func TestCallWithMediaProxyRejectsUnsupportedBeforeSignaling(t *testing.T) {
	c := &Client{}
	for _, raw := range []string{"http://user:secret@localhost:3128", "socks5://user:secret@localhost:99999"} {
		call, err := c.CallWithMediaProxy(context.Background(), "79115551314", raw)
		if call != nil || err == nil {
			t.Fatal("must reject bad media proxy before WhatsApp access")
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatal("credentials exposed")
		}
	}
}

func TestMediaFailureReasonOnlyBlamesProxyForTransportErrors(t *testing.T) {
	transport := &relay.CallTransportError{Op: "connect", Err: errors.New("DTLS timeout")}
	for _, tc := range []struct {
		proxy string
		err   error
		want  string
	}{
		{"socks5://localhost:1080", transport, "media_proxy_failed"},
		{"socks5://localhost:1080", fmt.Errorf("relay connect: %w", transport), "media_proxy_failed"},
		{"socks5://localhost:1080", errors.New("codec initialization failed"), "media_failed"},
		{"", transport, "media_failed"},
	} {
		if got := mediaFailureReason(tc.proxy, tc.err); got != tc.want {
			t.Fatalf("reason=%q want=%q", got, tc.want)
		}
	}
}
