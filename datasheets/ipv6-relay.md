# IPv6 relay addresses and STUN allocation

## Sources

- https://wacrg.org/spec/relay/relay-candidates/#te2-endpoint : `18 bytes → IPv6` followed by a big-endian 16-bit port. Merge addresses sharing relay ID and name.
- https://www.rfc-editor.org/rfc/rfc8489.html#section-14.2 : XOR address encoding, family 2, port XOR 0x2112, IPv6 XOR magic cookie concatenated with the transaction ID.
- https://www.rfc-editor.org/rfc/rfc5769.html#section-2.3 : independent IPv6 known-answer vector used by `stun/ipv6_test.go`.
- Existing pinned WhatsApp STUN builder: `stun/stun.go`, attr 0x0016, IPv4 family 1 and existing golden vectors.

## Observed evidence

Dev call `e67acd0b-417f-412d-9a8d-9e7ee04dd3b1`, profile `783160ee-a291`, on 2026-10-06: three packed IPv4 and three packed IPv6 candidates, no unknown lengths. Existing parser retained only IPv4. Raw keying/signaling/media was not recorded.

## Go interface and scope

- Preserve existing IPv4 public STUN builders, including their wire bytes.
- `EncodeXorRelayAddress(netip.AddrPort, [12]byte) ([]byte,error)` produces the complete standard XOR-address attribute value.
- `BuildWasmStunAllocateRequestForAddress` uses that value in the existing 0x0016 attribute and existing stream descriptors/MI.
- Retain IPv6 in the direct-call parser, merge address families, prefer IPv6 for media through a configured proxy. Direct calls retain IPv4 preference. Group-call builders remain unchanged.
- No configured proxy means direct routing; configured proxy always remains the media route even if only IPv4 candidates are provided.
- If IPv6 transport setup fails, retry a usable IPv4 candidate of the same endpoint through the same SOCKS5 proxy. Each attempt has a 12-second setup limit; cancellation stops retries. No direct socket fallback.

## Validation status

The standard IPv6 XOR encoding is checked against RFC 5769. WhatsApp attr 0x0016 requires a different transaction-word order, described below. A unit vector alone does not establish server acceptance.

## WhatsApp IPv6 transaction-word order

Live call `7c9f4c61-854d-43d2-8b57-06aff2eb264e` opened IPv6 DTLS/SCTP through the profile's SOCKS5 proxy. Relay rejected Allocate with code 452 and an address mismatch. Extracted address metadata, without tokens or keys, gave a lower-96-bit XOR difference `4d37374d bfcfcfbf ed9999ed`. Each 32-bit difference is palindromic, as produced by a word XOR its byte reversal. This is evidence for a byte-order mismatch in the transaction-ID words used to mask IPv6.

The WhatsApp builder swaps bytes in each of the three transaction-ID words **for the address mask only**. The STUN header transaction ID and MESSAGE-INTEGRITY use the original bytes. The generic RFC encoder is unchanged.

Synthetic regression vector: endpoint `[2001:db8::1]:3478`, transaction ID `b7e7a701bc34d686fa87dfae`, WhatsApp address value `00022c840113a9fa01a7e7b786d634bcaedf87fb`.

Live call `2b894808-c1e3-4c1b-b771-9f20c4a480a8` on 2026-10-06: profile proxy, IPv6 selected, DataChannel opened, Allocate Success (`0x0103`) and pong received. The recipient rejected before acceptance, so this proves transport/allocation, not bidirectional audio.

## Answered IPv6 call verification

Call `9bc17738-247d-48e9-914a-13e86bccf51f`, 2026-10-06 after rebuilding/restarting dev services: selected IPv6 through the profile SOCKS5 proxy, Allocate Success, peer accepted, authenticated audio decoded and Caller connected. Maintained connection for 26 seconds, then the test hung up normally. Worker: 1191 frames to browser and 1895 from browser; nonzero-frame counters 1052 and 639. Caller history: `ended / local_hangup`, duration 26, accepted/connected/ended timestamps populated. These confirm bidirectional media transport; subjective audibility is awaiting recipient feedback.
