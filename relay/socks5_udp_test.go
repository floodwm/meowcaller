package relay

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"
)

func fakeSOCKSUDP(t *testing.T, authStatus, associateStatus byte) (string, chan []byte, chan struct{}) {
	t.Helper()
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	packets := make(chan []byte, 16)
	shutdown := make(chan struct{})
	t.Cleanup(func() { tcp.Close(); udp.Close() })
	go func() {
		c, e := tcp.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(3 * time.Second))
		var greeting [2]byte
		if _, e = io.ReadFull(c, greeting[:]); e != nil {
			return
		}
		methods := make([]byte, int(greeting[1]))
		if _, e = io.ReadFull(c, methods); e != nil {
			return
		}
		if greeting[0] != 5 || !bytes.Contains(methods, []byte{2}) {
			return
		}
		c.Write([]byte{5, 2})
		var h [2]byte
		if _, e = io.ReadFull(c, h[:]); e != nil {
			return
		}
		user := make([]byte, int(h[1]))
		if _, e = io.ReadFull(c, user); e != nil {
			return
		}
		var n [1]byte
		if _, e = io.ReadFull(c, n[:]); e != nil {
			return
		}
		pass := make([]byte, int(n[0]))
		if _, e = io.ReadFull(c, pass); e != nil {
			return
		}
		if h[0] != 1 || string(user) != "test-user" || string(pass) != "test-secret" {
			return
		}
		c.Write([]byte{1, authStatus})
		if authStatus != 0 {
			return
		}
		var req [10]byte
		if _, e = io.ReadFull(c, req[:]); e != nil {
			return
		}
		if !bytes.Equal(req[:], []byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}) {
			return
		}
		addr := udp.LocalAddr().(*net.UDPAddr)
		reply := []byte{5, associateStatus, 0, 1, 0, 0, 0, 0, byte(addr.Port >> 8), byte(addr.Port)}
		c.Write(reply)
		if associateStatus != 0 {
			return
		}
		c.SetDeadline(time.Time{})
		closed := make(chan struct{})
		go func() { io.Copy(io.Discard, c); close(closed) }()
		select {
		case <-closed:
		case <-shutdown:
		}
	}()
	go func() {
		b := make([]byte, 65535)
		for {
			n, from, e := udp.ReadFromUDP(b)
			if e != nil {
				return
			}
			p := append([]byte(nil), b[:n]...)
			select {
			case packets <- p:
			default:
			}
			udp.WriteToUDP(p, from)
		}
	}()
	u := url.URL{Scheme: "socks5", Host: tcp.Addr().String(), User: url.UserPassword("test-user", "test-secret")}
	return u.String(), packets, shutdown
}

func TestSOCKSUDPAuthenticatedRoundTrip(t *testing.T) {
	proxy, packets, _ := fakeSOCKSUDP(t, 0, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, err := DialSOCKS5UDP(ctx, proxy)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	targets := []*net.UDPAddr{{IP: net.IPv4(192, 0, 2, 9), Port: 3478}, {IP: net.ParseIP("2001:db8::9"), Port: 5001}}
	for _, target := range targets {
		c.SetDeadline(time.Now().Add(time.Second))
		payload := []byte{0x16, 0xfe, 0xfd, 0, 1, 2, 3}
		n, e := c.WriteTo(payload, target)
		if e != nil || n != len(payload) {
			t.Fatalf("write: n=%d err=%v", n, e)
		}
		wire := <-packets
		if !bytes.Equal(wire[:3], []byte{0, 0, 0}) {
			t.Fatal("wrong reserved/fragment bytes")
		}
		if target.IP.To4() != nil {
			want := []byte{0, 0, 0, 1, 192, 0, 2, 9, 13, 150}
			if !bytes.Equal(wire[:10], want) {
				t.Fatalf("IPv4 framing=%x", wire[:10])
			}
		} else if wire[3] != 4 || !bytes.Equal(wire[4:20], target.IP.To16()) || binary.BigEndian.Uint16(wire[20:22]) != 5001 {
			t.Fatal("IPv6 framing")
		}
		b := make([]byte, 128)
		n, from, e := c.ReadFrom(b)
		if e != nil || !bytes.Equal(b[:n], payload) || from.String() != target.String() {
			t.Fatalf("read n=%d from=%v err=%v", n, from, e)
		}
	}
}

func TestSOCKSUDPErrorDoesNotLeakCredentialsOrFallback(t *testing.T) {
	for _, tc := range []struct {
		name            string
		auth, associate byte
	}{{"auth", 1, 0}, {"unsupported-udp", 0, 7}} {
		t.Run(tc.name, func(t *testing.T) {
			proxy, packets, _ := fakeSOCKSUDP(t, tc.auth, tc.associate)
			c, e := DialSOCKS5UDP(context.Background(), proxy)
			if e == nil || c != nil {
				t.Fatal("failed proxy must not return a direct socket")
			}
			if strings.Contains(e.Error(), "test-secret") || strings.Contains(e.Error(), proxy) {
				t.Fatal("credentials exposed")
			}
			select {
			case <-packets:
				t.Fatal("unexpected UDP send after failed association")
			default:
			}
		})
	}
	for _, raw := range []string{"http://test-user:test-secret@localhost:3128", "socks5://test-user:test-secret@localhost", "socks5://localhost:0", "socks5://localhost:99999", "socks5://localhost:1080/path", "socks5://localhost:1080?q=secret", "socks5://localhost:1080#fragment", "socks5://user@localhost:1080", "socks5://localhost:abc"} {
		if e := ValidateSOCKS5Proxy(raw); e == nil {
			t.Errorf("invalid proxy accepted: %s", raw)
		}
	}
}

func TestSOCKSUDPControlClosureAndCancellationCloseSocket(t *testing.T) {
	for _, byContext := range []bool{false, true} {
		t.Run(map[bool]string{false: "tcp-close", true: "context-cancel"}[byContext], func(t *testing.T) {
			proxy, _, shutdown := fakeSOCKSUDP(t, 0, 0)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c, e := DialSOCKS5UDP(ctx, proxy)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			result := make(chan error, 1)
			go func() { _, _, e := c.ReadFrom(make([]byte, 256)); result <- e }()
			if byContext {
				cancel()
			} else {
				close(shutdown)
			}
			select {
			case e = <-result:
				if e == nil {
					t.Fatal("closed proxy returned success")
				}
			case <-time.After(time.Second):
				t.Fatal("read not interrupted")
			}
			if e = c.Close(); e != nil {
				t.Fatal("Close must be idempotent")
			}
		})
	}
}

func TestSOCKSUDPReadDeadline(t *testing.T) {
	proxy, _, _ := fakeSOCKSUDP(t, 0, 0)
	c, e := DialSOCKS5UDP(context.Background(), proxy)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	_, _, e = c.ReadFrom(make([]byte, 256))
	if ne, ok := e.(net.Error); !ok || !ne.Timeout() {
		t.Fatalf("expected deadline, got %v", e)
	}
}

func TestSOCKSUDPHeaderVectors(t *testing.T) {
	for _, b := range [][]byte{nil, {0, 0, 1, 1, 192, 0, 2, 9, 0, 1}, {1, 0, 0, 1, 192, 0, 2, 9, 0, 1}, {0, 0, 0, 1}, {0, 0, 0, 4}, {0, 0, 0, 3, 0}, {0, 0, 0, 7}} {
		if _, _, e := decodeSOCKSUDP(b); e == nil {
			t.Fatalf("accepted malformed/fragmented %x", b)
		}
	}
	addr, payload, e := decodeSOCKSUDP([]byte{0, 0, 0, 3, 9, '1', '9', '2', '.', '0', '.', '2', '.', '9', 13, 150, 42})
	if e != nil || addr.String() != "192.0.2.9:3478" || !bytes.Equal(payload, []byte{42}) {
		t.Fatalf("domain IP literal: %v %x %v", addr, payload, e)
	}
	if _, _, e = decodeSOCKSUDP([]byte{0, 0, 0, 3, 11, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm', 0, 1, 42}); e == nil {
		t.Fatal("reply must not trigger direct domain lookup")
	}
}

func TestRelayProxyAssociationFailureDoesNotContactTarget(t *testing.T) {
	proxy, _, _ := fakeSOCKSUDP(t, 0, 7)
	target, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ch, e := ConnectRelayMediaContext(ctx, target.LocalAddr().(*net.UDPAddr), WithSOCKS5Proxy(proxy))
	if e == nil || ch != nil || !strings.Contains(e.Error(), "association rejected") {
		t.Fatalf("ch=%v err=%v", ch, e)
	}
	target.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	if _, _, e = target.ReadFromUDP(make([]byte, 128)); e == nil {
		t.Fatal("proxy failure contacted target directly")
	}
}
