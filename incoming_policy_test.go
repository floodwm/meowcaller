package meowcaller

import (
	"bytes"
	"context"
	"errors"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"testing"
)

func policyTestClient(policy func(IncomingCallInfo) (IncomingCallPolicy, error)) *Client {
	wa := whatsmeow.NewClient(&store.Device{}, waLog.Noop)
	c := NewClient(wa, WithIncomingPolicy(policy))
	c.eng.decryptOfferKey = func(context.Context, *events.CallOffer) ([]byte, error) { return make([]byte, 32), nil }
	return c
}
func policyOffer() *events.CallOffer {
	return &events.CallOffer{BasicCallMeta: types.BasicCallMeta{CallID: "IN", From: peerJID(), CallCreator: peerJID()}, Data: &waBinary.Node{Tag: "offer", Content: []waBinary.Node{{Tag: "video"}}}}
}
func TestIncomingPolicyBeforeSignaling(t *testing.T) {
	for _, tc := range []struct {
		name     string
		decision IncomingCallPolicy
		err      error
	}{{"disabled", IncomingCallPolicy{}, nil}, {"error", IncomingCallPolicy{Allow: true}, errors.New("policy")}, {"bad proxy", IncomingCallPolicy{Allow: true, MediaProxy: "http://bad"}, nil}} {
		t.Run(tc.name, func(t *testing.T) {
			called := 0
			c := policyTestClient(func(info IncomingCallInfo) (IncomingCallPolicy, error) {
				called++
				if info.ProviderID != "IN" || !info.Video || info.Group {
					t.Fatalf("info=%+v", info)
				}
				return tc.decision, tc.err
			})
			c.eng.decryptOfferKey = func(context.Context, *events.CallOffer) ([]byte, error) {
				t.Fatal("decrypted rejected offer")
				return nil, nil
			}
			c.eng.sendCallNode = func(context.Context, waBinary.Node) error { t.Fatal("signaled rejected offer"); return nil }
			c.eng.onOffer(policyOffer())
			if called != 1 || len(c.eng.calls) != 0 {
				t.Fatalf("called=%d calls=%d", called, len(c.eng.calls))
			}
		})
	}
}
func TestIncomingPolicyFreezesRouteAndDoesNotEnableLocalVideo(t *testing.T) {
	proxy := "socks5://127.0.0.1:1080"
	c := policyTestClient(func(IncomingCallInfo) (IncomingCallPolicy, error) {
		return IncomingCallPolicy{Allow: true, MediaProxy: proxy}, nil
	})
	sent := 0
	c.eng.sendCallNode = func(context.Context, waBinary.Node) error { sent++; return nil }
	c.eng.onOffer(policyOffer())
	m := c.eng.lookup("IN")
	if m == nil || m.localVideo || !m.remoteVideo || m.mediaProxy != proxy || sent != 1 {
		t.Fatalf("call=%+v sent=%d", m, sent)
	}
	proxy = "socks5://127.0.0.1:9999"
	c.eng.onOffer(policyOffer())
	if c.eng.lookup("IN") != m {
		t.Fatal("duplicate offer replaced call")
	}
	c.eng.onRelay("IN", &waBinary.Node{Tag: "transport"})
	if m.mediaProxy != "socks5://127.0.0.1:1080" {
		t.Fatal("route changed")
	}
}
func TestAbandonDoesNotSignalAndClosesLocally(t *testing.T) {
	eng, call := testEngineWithOutgoingCall()
	eng.calls[call.ID()].direction = CallDirectionIncoming
	cancelled := false
	eng.calls[call.ID()].cancel = func() { cancelled = true }
	sink := &lifecycleAudioSink{}
	call.Receive(sink)
	eng.sendCallNode = func(context.Context, waBinary.Node) error { t.Fatal("abandon signaled"); return nil }
	if err := call.Abandon(); err != nil {
		t.Fatal(err)
	}
	if call.State() != CallPhaseEnded || eng.lookup(call.ID()) != nil || !cancelled || sink.closeCount != 1 || call.EndReason() != "abandoned" {
		t.Fatal("incomplete local cleanup")
	}
	if err := call.Abandon(); err != nil {
		t.Fatal(err)
	}
}
func TestIncomingPolicyGroupPassivelyIgnored(t *testing.T) {
	called := false
	c := policyTestClient(func(info IncomingCallInfo) (IncomingCallPolicy, error) {
		called = true
		if !info.Group {
			t.Fatal("missing group info")
		}
		return IncomingCallPolicy{Allow: true}, nil
	})
	ev := policyOffer()
	ev.Data = &waBinary.Node{Tag: "offer", Attrs: waBinary.Attrs{"call-id": "IN", "call-creator": peerJID(), "joinable": "1"}, Content: []waBinary.Node{{Tag: "group_info", Attrs: waBinary.Attrs{"call-id": "IN", "call-creator": peerJID(), "transaction-id": "7", "media": "audio", "connected-limit": "8"}}}}
	c.eng.sendCallNode = func(context.Context, waBinary.Node) error { t.Fatal("group signaled"); return nil }
	c.eng.onOffer(ev)
	if !called || len(c.eng.calls) != 0 {
		t.Fatal("group registered or policy not evaluated")
	}
}

func TestPolicyIncomingOwnDeviceRejectIgnored(t *testing.T) {
	c := policyTestClient(func(IncomingCallInfo) (IncomingCallPolicy, error) { return IncomingCallPolicy{Allow: true}, nil })
	own := types.NewJID("self", types.DefaultUserServer)
	c.wa.Store.ID = &own
	eng, call := testEngineWithOutgoingCall()
	eng.c = c
	eng.calls[call.ID()].direction = CallDirectionIncoming
	linked := own
	linked.Device = 5
	eng.onReject(&events.CallReject{BasicCallMeta: types.BasicCallMeta{CallID: call.ID(), From: linked}, Data: &waBinary.Node{Tag: "reject", Attrs: waBinary.Attrs{"reason": "rejected"}}})
	if call.State() == CallPhaseEnded {
		t.Fatal("own device refusal ended incoming call")
	}
	eng.onReject(&events.CallReject{BasicCallMeta: types.BasicCallMeta{CallID: call.ID(), From: peerJID()}, Data: &waBinary.Node{Tag: "reject"}})
	if call.State() != CallPhaseEnded {
		t.Fatal("remote refusal ignored")
	}
}
func TestPolicyEndedOfferClearsExistingLocally(t *testing.T) {
	c := policyTestClient(func(IncomingCallInfo) (IncomingCallPolicy, error) { return IncomingCallPolicy{}, nil })
	call := &Call{eng: c.eng, id: "IN", phase: CallPhaseRinging}
	c.eng.calls["IN"] = &engineCall{call: call, direction: CallDirectionIncoming}
	c.eng.sendCallNode = func(context.Context, waBinary.Node) error { t.Fatal("ended offer signaled"); return nil }
	ev := policyOffer()
	ev.Data.Attrs = waBinary.Attrs{"is_call_ended": "1", "terminate_reason": "accepted_elsewhere"}
	c.eng.onOffer(ev)
	if call.State() != CallPhaseEnded || call.EndReason() != "accepted_elsewhere" {
		t.Fatal("ended offer leaked live call")
	}
}

func TestPolicyIncomingMediaFailureBeforeAnswerIsPassive(t *testing.T) {
	c := policyTestClient(func(IncomingCallInfo) (IncomingCallPolicy, error) { return IncomingCallPolicy{Allow: true}, nil })
	call := &Call{eng: c.eng, id: "IN", phase: CallPhaseRinging}
	c.eng.calls["IN"] = &engineCall{call: call, direction: CallDirectionIncoming}
	c.eng.sendCallNode = func(context.Context, waBinary.Node) error { t.Fatal("unanswered media failure signaled"); return nil }
	c.eng.finishMediaFailure("IN", "media_failed")
	if call.State() != CallPhaseEnded {
		t.Fatal("media failure leaked call")
	}
}

func TestPolicyAnsweredMediaFailureTerminates(t *testing.T) {
	c := policyTestClient(func(IncomingCallInfo) (IncomingCallPolicy, error) { return IncomingCallPolicy{Allow: true}, nil })
	call := &Call{eng: c.eng, id: "IN", phase: CallPhaseRinging}
	c.eng.calls["IN"] = &engineCall{call: call, direction: CallDirectionIncoming}
	if err := call.Answer(); err != nil {
		t.Fatal(err)
	}
	sent := 0
	c.eng.sendCallNode = func(context.Context, waBinary.Node) error { sent++; return nil }
	c.eng.finishMediaFailure("IN", "media_failed")
	if sent != 1 || call.State() != CallPhaseEnded {
		t.Fatalf("sent=%d state=%v", sent, call.State())
	}
}

func TestPolicyIncomingMediaFailureCallbackCannotDeclinePhone(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "hangup", true: "reject"}[reject], func(t *testing.T) {
			c := policyTestClient(func(IncomingCallInfo) (IncomingCallPolicy, error) {
				return IncomingCallPolicy{Allow: true}, nil
			})
			call := &Call{eng: c.eng, id: "IN", phase: CallPhaseRinging}
			c.eng.calls[call.ID()] = &engineCall{call: call, direction: CallDirectionIncoming}
			call.OnEnd(func(string) {
				var err error
				if reject {
					err = call.Reject()
				} else {
					err = call.Hangup()
				}
				if err != nil {
					t.Error(err)
				}
			})
			sent := 0
			c.eng.sendCallNode = func(context.Context, waBinary.Node) error { sent++; return nil }
			c.eng.finishMediaFailure(call.ID(), "media_failed")
			if sent != 0 || call.EndReason() != "media_failed" {
				t.Fatalf("passive failure declined phone: sent=%d reason=%s", sent, call.EndReason())
			}
		})
	}
}

func TestAbandonDuringMediaFailurePreventsPendingTerminate(t *testing.T) {
	eng, call := testEngineWithOutgoingCall()
	call.OnEnd(func(string) {
		if err := call.Abandon(); err != nil {
			t.Fatal(err)
		}
	})
	eng.sendCallNode = func(context.Context, waBinary.Node) error {
		t.Fatal("abandoned call signaled after callback")
		return nil
	}
	eng.finishMediaFailure(call.ID(), "media_failed")
}

func TestAcceptedElsewhereDuringMediaFailurePreventsTerminate(t *testing.T) {
	for _, offer := range []bool{false, true} {
		t.Run(map[bool]string{false: "terminate", true: "offer"}[offer], func(t *testing.T) {
			c := policyTestClient(func(IncomingCallInfo) (IncomingCallPolicy, error) { return IncomingCallPolicy{Allow: true}, nil })
			call := &Call{eng: c.eng, id: "IN", phase: CallPhaseConnecting}
			c.eng.calls["IN"] = &engineCall{call: call, direction: CallDirectionIncoming, incomingAnswered: true}
			call.OnEnd(func(string) {
				if offer {
					ev := policyOffer()
					ev.Data.Attrs = waBinary.Attrs{"is_call_ended": "1", "terminate_reason": "accepted_elsewhere"}
					c.eng.onOffer(ev)
				} else {
					c.eng.onTerminate("IN", "accepted_elsewhere")
				}
			})
			c.eng.sendCallNode = func(context.Context, waBinary.Node) error {
				t.Fatal("terminated phone after accepted_elsewhere")
				return nil
			}
			c.eng.finishMediaFailure("IN", "media_failed")
		})
	}
}
func TestPolicyIgnoresUnownedRawControl(t *testing.T) {
	c := policyTestClient(func(IncomingCallInfo) (IncomingCallPolicy, error) { return IncomingCallPolicy{}, nil })
	c.eng.sendCallNode = func(context.Context, waBinary.Node) error { t.Fatal("unowned control acknowledged"); return nil }
	for _, tag := range []string{"group_update", "enc_rekey", "video", "mute_v2"} {
		node := &waBinary.Node{Tag: "call", Attrs: waBinary.Attrs{"id": "N", "from": peerJID()}, Content: []waBinary.Node{{Tag: tag, Attrs: waBinary.Attrs{"call-id": "DENIED", "state": "1"}}}}
		if c.eng.onCallRaw(node) {
			t.Fatalf("consumed unowned %s", tag)
		}
	}
}

func TestPolicyUnknownCallEventLeavesUnownedRekeyUntouched(t *testing.T) {
	for _, mode := range []string{"unowned", "owned", "upstream"} {
		t.Run(mode, func(t *testing.T) {
			wa := whatsmeow.NewClient(&store.Device{}, waLog.Noop)
			var logs bytes.Buffer
			opts := []Option{WithLogger(zerolog.New(&logs).Level(zerolog.DebugLevel))}
			if mode != "upstream" {
				opts = append(opts, WithIncomingPolicy(func(IncomingCallInfo) (IncomingCallPolicy, error) {
					return IncomingCallPolicy{}, nil
				}))
			}
			c := NewClient(wa, opts...)
			if mode == "owned" {
				call := &Call{eng: c.eng, id: "IN", phase: CallPhaseRinging}
				c.eng.calls[call.ID()] = &engineCall{call: call, direction: CallDirectionIncoming}
			}
			// Routing is valid. The deliberately incomplete encrypted action must
			// be examined only for owned calls or the default upstream mode.
			node := &waBinary.Node{Tag: "call", Attrs: waBinary.Attrs{"from": peerJID()}, Content: []waBinary.Node{{Tag: "enc_rekey", Attrs: waBinary.Attrs{"call-id": "IN", "call-creator": peerJID()}}}}
			wa.DangerousInternals().DispatchEvent(&events.UnknownCallEvent{Node: node})
			processed := bytes.Contains(logs.Bytes(), []byte("group rekey received"))
			if processed != (mode != "unowned") {
				t.Fatalf("unexpected encrypted-control processing: mode=%s processed=%t", mode, processed)
			}
		})
	}
}

func TestAbandonPreventsLaterRejectAndHangup(t *testing.T) {
	eng, call := testEngineWithOutgoingCall()
	eng.sendCallNode = func(context.Context, waBinary.Node) error { t.Fatal("signaled after abandon"); return nil }
	if err := call.Abandon(); err != nil {
		t.Fatal(err)
	}
	if err := call.Reject(); err != nil {
		t.Fatal(err)
	}
	if err := call.Hangup(); err != nil {
		t.Fatal(err)
	}
}

func TestAbandonDuringRejectOrHangupPreventsPendingSignal(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(map[bool]string{false: "hangup", true: "reject"}[reject], func(t *testing.T) {
			eng, call := testEngineWithOutgoingCall()
			call.OnEnd(func(string) {
				if err := call.Abandon(); err != nil {
					t.Fatal(err)
				}
			})
			eng.sendCallNode = func(context.Context, waBinary.Node) error {
				t.Fatal("signal sent after abandon won during cleanup")
				return nil
			}
			var err error
			if reject {
				err = call.Reject()
			} else {
				err = call.Hangup()
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
