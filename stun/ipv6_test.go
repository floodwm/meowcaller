package stun

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"net/netip"
	"testing"
)

func TestEncodeXorRelayAddressRFC5769IPv6(t *testing.T) {
	// RFC 5769 section 2.3: independent published XOR-address vector.
	txBytes, _ := hex.DecodeString("b7e7a701bc34d686fa87dfae")
	var tx [12]byte
	copy(tx[:], txBytes)
	got, err := EncodeXorRelayAddress(netip.MustParseAddrPort("[2001:db8:1234:5678:11:2233:4455:6677]:32853"), tx)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got) != "0002a1470113a9faa5d3f179bc25f4b5bed2b9d9" {
		t.Fatalf("RFC vector mismatch: %x", got)
	}
	tx[11] ^= 1
	next, _ := EncodeXorRelayAddress(netip.MustParseAddrPort("[2001:db8:1234:5678:11:2233:4455:6677]:32853"), tx)
	if bytes.Equal(next, got) {
		t.Fatal("IPv6 XOR must depend on transaction ID")
	}
}

func TestWasmAllocateAddressIPv4Compatibility(t *testing.T) {
	k := loadStunKat(t)
	tx := tx12(t, k)
	token, key := mustHex(t, k.Stun.RelayToken), mustHex(t, k.Stun.MiKey)
	ssrcs := [9]uint32{1, 2, 3, 4, 5, 6, 7, 8, 9}
	xor, ok := EncodeXorRelayEndpoint("157.240.226.133", 3478)
	if !ok {
		t.Fatal("IPv4 encode failed")
	}
	want := BuildWasmStunAllocateRequestWithStreamSsrcs(tx, token, xor, ssrcs, key)
	got, err := BuildWasmStunAllocateRequestForAddress(tx, token, netip.MustParseAddrPort("157.240.226.133:3478"), ssrcs, key)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("changed IPv4 wire format")
	}
}

func TestWasmAllocateIPv6AttributeAndIntegrity(t *testing.T) {
	tx := [12]byte{1, 2, 3}
	key := []byte("synthetic-test-key")
	packet, err := BuildWasmStunAllocateRequestForAddress(tx, []byte("test-token"), netip.MustParseAddrPort("[2001:db8::1]:3478"), [9]uint32{1}, key)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(packet[2:4]) != uint16(len(packet)-20) {
		t.Fatal("bad STUN length")
	}
	found, integrityFound := false, false
	for off := 20; off+4 <= len(packet); {
		typ, n := binary.BigEndian.Uint16(packet[off:]), int(binary.BigEndian.Uint16(packet[off+2:]))
		value := packet[off+4 : off+4+n]
		if typ == attrWasmRelayEndpoint {
			found = true
			if n != 20 || value[0] != 0 || value[1] != 2 {
				t.Fatal("bad IPv6 address attribute")
			}
		}
		if typ == attrMessageIntegrity {
			integrityFound = true
			mac := hmac.New(sha1.New, key)
			mac.Write(packet[:off])
			if !hmac.Equal(mac.Sum(nil), value) {
				t.Fatal("invalid MESSAGE-INTEGRITY")
			}
		}
		off += 4 + (n+3)&^3
	}
	if !found {
		t.Fatal("missing address attribute")
	}
	if !integrityFound {
		t.Fatal("missing MESSAGE-INTEGRITY")
	}
	if _, err := EncodeXorRelayAddress(netip.AddrPort{}, tx); err == nil {
		t.Fatal("accepted invalid endpoint")
	}
}

func TestWasmAllocateIPv6TransactionWords(t *testing.T) {
	var tx [12]byte
	copy(tx[:], mustHex(t, "b7e7a701bc34d686fa87dfae"))
	packet, err := BuildWasmStunAllocateRequestForAddress(tx, []byte("synthetic-token"), netip.MustParseAddrPort("[2001:db8::1]:3478"), [9]uint32{1}, []byte("synthetic-key"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(packet[8:20], tx[:]) {
		t.Fatal("transaction ID on wire must remain unchanged")
	}
	for _, attr := range ParseStunAttributes(packet) {
		if attr.AttrType == attrWasmRelayEndpoint {
			if got := hex.EncodeToString(attr.Value); got != "00022c840113a9fa01a7e7b786d634bcaedf87fb" {
				t.Fatalf("WhatsApp XOR mask = %s", got)
			}
			return
		}
	}
	t.Fatal("missing address attribute")
}
