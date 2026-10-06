package relay

import (
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

func TestRelayReceiveIdleTimeoutUnblocksSilentConnection(t *testing.T) {
	reader, writer := net.Pipe()
	defer reader.Close()
	defer writer.Close()
	ch := &RelayMediaChannel{dc: reader, readIdleTimeout: 25 * time.Millisecond}
	_, err := ch.Recv(make([]byte, 128))
	var transport *CallTransportError
	if !errors.As(err, &transport) || transport.Op != "recv" || !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("silent channel did not return a transport timeout: %v", err)
	}
}

func TestRelayReceiveIdleTimeoutRefreshesAfterTraffic(t *testing.T) {
	reader, writer := net.Pipe()
	defer reader.Close()
	defer writer.Close()
	ch := &RelayMediaChannel{dc: reader, readIdleTimeout: 100 * time.Millisecond}
	go func() {
		for i := 0; i < 3; i++ {
			time.Sleep(40 * time.Millisecond)
			if _, err := writer.Write([]byte{1}); err != nil {
				return
			}
		}
	}()
	for i := 0; i < 3; i++ {
		if n, err := ch.Recv(make([]byte, 128)); err != nil || n != 1 {
			t.Fatalf("live channel failed at %d: n=%d err=%v", i, n, err)
		}
	}
}
