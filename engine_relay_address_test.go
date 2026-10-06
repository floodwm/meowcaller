package meowcaller

import (
	"encoding/binary"
	waBinary "go.mau.fi/whatsmeow/binary"
	"net"
	"testing"
)

func TestRelayCandidateCounts(t *testing.T) {
	n := &waBinary.Node{Tag: "relay", Content: []waBinary.Node{
		{Tag: "key", Content: []byte("secret-not-an-address")},
		{Tag: "te2", Content: make([]byte, 6)},
		{Tag: "te2", Content: make([]byte, 18)},
		{Tag: "te2", Content: make([]byte, 18)},
		{Tag: "te2", Content: make([]byte, 17)},
		{Tag: "te2"},
	}}
	v4, v6, other := relayCandidateCounts(n)
	if v4 != 1 || v6 != 2 || other != 2 {
		t.Fatalf("counts = %d/%d/%d, want 1/2/2", v4, v6, other)
	}
}

func TestParseRelayDataRetainsIPv6AndMergesEndpoint(t *testing.T) {
	pack := func(ip string) []byte {
		b := net.ParseIP(ip).To16()
		if v4 := net.ParseIP(ip).To4(); v4 != nil {
			b = v4
		}
		return binary.BigEndian.AppendUint16(append([]byte(nil), b...), 3478)
	}
	attrs := waBinary.Attrs{"relay_id": "1", "relay_name": "test", "token_id": "2", "auth_token_id": "3"}
	n := &waBinary.Node{Tag: "relay", Content: []waBinary.Node{
		{Tag: "te2", Attrs: attrs, Content: pack("2001:db8::1")},
		{Tag: "te2", Attrs: attrs, Content: pack("192.0.2.1")},
		{Tag: "te2", Attrs: waBinary.Attrs{"relay_id": "2", "relay_name": "other"}, Content: pack("2001:db8::2")},
		{Tag: "te2", Attrs: attrs, Content: make([]byte, 17)},
	}}
	rd := parseRelayData(n)
	if len(rd.endpoints) != 2 || len(rd.endpoints[0].addresses) != 2 {
		t.Fatalf("endpoint/address count wrong: %+v", rd.endpoints)
	}
	ep := &rd.endpoints[0]
	if ep.tokenID != 2 || ep.authTokenID != 3 {
		t.Fatal("lost endpoint token indexes")
	}
	a, err := selectMediaRelayAddress(ep, true)
	if err != nil || a.String() != "[2001:db8::1]:3478" {
		t.Fatalf("IPv6 selection: %v %v", a, err)
	}
	a, err = selectMediaRelayAddress(ep, false)
	if err != nil || a.String() != "192.0.2.1:3478" {
		t.Fatalf("direct IPv4 selection: %v %v", a, err)
	}
}

func TestSelectMediaRelayAddressFallbackAndMalformed(t *testing.T) {
	ep := &relayEndpoint{addresses: []relayAddress{{ipv4: "192.0.2.1", port: 3478}}}
	if a, err := selectMediaRelayAddress(ep, true); err != nil || !a.Addr().Is4() {
		t.Fatalf("IPv4-only candidate failed: %v", err)
	}
	ep.addresses = []relayAddress{{ipv6: "2001:db8::1", port: 3478}}
	if a, err := selectMediaRelayAddress(ep, false); err != nil || !a.Addr().Is6() {
		t.Fatalf("IPv6-only candidate failed: %v", err)
	}
	for _, addresses := range [][]relayAddress{
		nil, {{ipv6: "not an IP", port: 3478}}, {{ipv6: "::", port: 3478}},
		{{ipv4: "192.0.2.1", port: 0}}, {{ipv6: "fe80::1%eth0", port: 3478}},
	} {
		ep.addresses = addresses
		if _, err := selectMediaRelayAddress(ep, true); err == nil {
			t.Fatal("accepted unusable address")
		}
	}
}
