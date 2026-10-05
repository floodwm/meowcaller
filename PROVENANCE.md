# Wappi meowcaller fork

Upstream: https://github.com/purpshell/meowcaller
Fork: https://github.com/floodwm/meowcaller
Migration starts at fork commit `c48c3e2a243c672c942cbf0c941d14161720d540`; its Git history is retained.
Source commit: `27a3c6b18657614c9ec2ed16dfc497eff11de6ec` (before hypermeow migration).
License: MIT; original license retained in LICENSE.

This library wraps the existing official Whatsmeow client. It does not create a
second session or Whatsmeow client. Whatsmeow is pinned to
`v0.0.0-20260929112325-8b41cfe6d9c4`, matching gowapi.

The previous copy in gowapi/third_party contained the library and standalone
regression tests, without examples, datasheets or capture-dependent MLow tests.
This repository retains all upstream files and codec test fixtures. Only local
patches and tests are transferred. Codec implementations and reference pins are
unchanged. Examples use the same official Whatsmeow client as the library.
The optional diag recorder remains disabled unless explicitly supplied through
WithDiagnostics. Gowapi does not enable it.

Local fixes: `WithOutgoingOnly` preserves legacy handling of unsolicited calls;
`ReinstallEventHandler` restores only this wrapper event subscription after
legacy code clears handlers. Failed outgoing invitation sends release registered
call state and media resources with reason `offer_failed`.

Outgoing direct calls distinguish unavailable secondary devices from
primary device refusal. Explicit busy/decline remains terminal before an answer.
After CallAccept, only the answering device can reject that direct call; selected
device identity is recorded even without optional capability metadata.

Protocol limitation: a secondary device reject with no reason is ambiguous.
The current policy keeps ringing in that case (also for non-user error reasons
such as `enc`). A genuine manual decline from a secondary device without an
explicit busy/decline reason therefore cannot be distinguished offline and may
not terminate immediately. This policy has only unit validation, not live
multi-device WhatsApp validation. Primary rejects remain authoritative before
acceptance, regardless of reason.

Validation:
```
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test ./... -count=1
```

## Integration

Construct `meowcaller.NewClient(existingWhatsmeowClient, meowcaller.WithOutgoingOnly())`
once before connecting
that existing client. Reuse this wrapper for subsequent calls. If legacy code
calls `RemoveEventHandlers`, call `ReinstallEventHandler` before adding legacy
handlers that mutate event metadata. Reinstallation does not wrap raw hooks again. The adapter hooks
Whatsmeow call/ack handlers through DangerousInternals plus reflection/unsafe,
so changes to the Whatsmeow version require compatibility verification.

`client.Call(ctx, target)` sends the invitation and returns a `*Call`.
`Call.Play(source)` attaches an `AudioSource`; `Call.Receive(sink)` attaches an
`AudioSink`. Both exchange 960 float32 samples of mono 16 kHz PCM per 60 ms frame.
Source EOF stops playback; it does not hang up the call. `PCMStream(io.ReadCloser)`
accepts s16le 16 kHz mono input; `SinkFunc` adapts a PCM callback.

`Call.OnPeerAccept` signals peer answer. `Call.OnReady` signals media readiness.
`Call.OnEnd` receives the termination reason; `Call.OnStateChange` tracks phases.
`Call.Hangup` terminates signaling/media and closes the current source and sink.
Register a finish handler on the returned Player to hang up after playback if
that behavior is required. Incoming calls use `Client.OnIncomingCall` and
`Call.Answer` / `Call.Reject`.

No live WhatsApp calls were made for this migration.

See [WAPPI.md](WAPPI.md) for publishing, version pinning and upstream updates.
