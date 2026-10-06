# SOCKS5 UDP relay transport

**Status:** implemented; local packet vectors and lifecycle tests pass. Live
WhatsApp relay compatibility over production proxies is not validated.

**Reference pinned at:** RFC 1928 (March 1996), sections 3–7; RFC 1929
(March 1996), section 2. This adapter is a new Go transport, not a Rust port.

## Reference source

Normative sources: [RFC 1928](https://datatracker.ietf.org/doc/html/rfc1928#section-7)
and [RFC 1929](https://datatracker.ietf.org/doc/html/rfc1929#section-2).

RFC 1928: “A UDP association terminates when the TCP connection that the UDP
ASSOCIATE request arrived on terminates.”

The client negotiates a method, authenticates if configured, and sends command 3.
The proxy returns BND.ADDR/BND.PORT; those identify the UDP relay socket, not the
WhatsApp destination. A datagram contains two zero reserved bytes, fragment byte,
address type, destination address and big-endian port, followed by the payload.
The Go implementation does not reassemble fragments; nonzero FRAG is discarded.

## Go envelope

`ValidateSOCKS5Proxy(string) error` validates URL/auth without echoing credentials.
`DialSOCKS5UDP(context.Context, string) (net.PacketConn, error)` returns a
connected UDP proxy socket with a live TCP control session. PacketConn exposes
original destination/source IPs to Pion while adding/removing SOCKS5 headers.

`WithSOCKS5Proxy` and `ConnectRelayMediaContext` adapt the existing relay stack.
`WithCancelOnClose` gives a successfully built channel ownership of its context;
the caller retains ownership on construction failure.

`CallWithMediaProxy` snapshots the URL into outgoing engineCall state. No new
Whatsmeow client, session, authorization, codec, or incoming-call policy is added.

## Validation and boundaries

IPv4 KAT prefix: `00 00 00 01 c0 00 02 09 0d 96` for 192.0.2.9:3478.
IPv6 frames contain ATYP=4, 16 address bytes and a two-byte port.
Local authenticated SOCKS fixtures verify roundtrip, TCP EOF, cancellation,
deadlines, denied authentication/ASSOCIATE, malformed frames and no direct send.
Terminal tests verify cancellation, first reason, and the correct terminate.

UDP datagrams are bounded to 65507 wire bytes. Buffer reuse is serialized per
direction. Context cancellation closes control and UDP; relay setup has the
existing 12-second limit. A configured proxy failure has no direct fallback.
IPv6 setup failures may retry IPv4 through the same proxy (at most two setup
attempts). The calling engine applies `WithReadIdleTimeout(30*time.Second)` to
relay reads. Every received relay message refreshes the deadline, including
keepalive responses, so silent microphone input does not trigger this timeout.
When UDP stops while SOCKS TCP remains open, the read timeout propagates a
transport error and tears down the direct call. Library users without this
option retain the previous receive behavior.
HTTP CONNECT is not a UDP transport and is rejected. Receiving domain-name
datagram headers only supports numeric IP literals, avoiding direct DNS for
WhatsApp destinations; a domain in proxy BND.ADDR uses the host resolver to find
the proxy relay itself. Incoming calls retain the existing API and policy.

Run `go test ./...`, `go test -race ./...` and `go vet ./...`. These verify local
implementation, not delivery through a particular external proxy or handset.
