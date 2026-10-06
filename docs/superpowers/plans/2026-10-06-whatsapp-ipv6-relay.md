# WhatsApp IPv6 relay verification and implementation

**Goal:** preserve the profile's IPv6 egress when WhatsApp provides a usable IPv6 media relay.

**Architecture:** existing Whatsmeow session and Caller worker; existing SOCKS5 UDP association. Production proxy stays read-only. No commit or push.

## Steps

- [x] Add tested, metadata-only relay-family diagnostics in `engine.go`; forward only bounded counters in gowapi `caller_wa_relay_log.go`. Keep raw diagnostics disabled.
- [x] Build with `/tmp/caller-wa-socks-workspace/go.work`, restart only the dev gowapi container, call the authorized test recipient `79115551314` and inspect actual advertised families.
- [x] If IPv6 is advertised, retain and merge packed 18-byte endpoints in the fork, select a usable IPv6 address for proxied media and extend the WhatsApp STUN address attribute using its transaction ID. Preserve existing IPv4 vectors. Test malformed input, selection, framing and cancellation.
- [x] Verify IPv6 SOCKS5 transport and live STUN allocation: Allocate Success confirmed after byte-order fix.
- [x] Verify answered call and bidirectional nonzero media: call `9bc17738-247d-48e9-914a-13e86bccf51f` connected for 26 seconds via IPv6 SOCKS5 and ended normally. Recipient confirmation of subjective audibility is pending.
- [x] Run fork tests/race/vet and gowapi tests; inspect the diff and document release limitations. No production proxy changes or gapi changes.

## References

- https://wacrg.org/spec/relay/relay-candidates/ — packed address lengths and endpoint merging.
- RFC 1928 — SOCKS5 IPv6 UDP addressing.
- RFC 8489 section 14.2 — XOR address encoding; confirm the WhatsApp attribute before extending its wire format.
