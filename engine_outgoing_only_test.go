package meowcaller

import (
	"context"
	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"reflect"
	"testing"
)

func TestOutgoingOnlyLeavesUnsolicitedCallsToLegacyHandler(t *testing.T) {
	wa := whatsmeow.NewClient(&store.Device{}, waLog.Noop)
	legacy := 0
	wa.AddEventHandler(func(any) { legacy++ })
	c := NewClient(wa, WithOutgoingOnly())
	sent := 0
	c.eng.sendCallNode = func(context.Context, waBinary.Node) error { sent++; return nil }
	// A deliberately incomplete offer must never enter decryption/preaccept.
	wa.DangerousInternals().DispatchEvent(&events.CallOffer{BasicCallMeta: types.BasicCallMeta{CallID: "INCOMING"}})
	raw := &waBinary.Node{Tag: "call", Attrs: waBinary.Attrs{"id": "N", "from": peerJID()}, Content: []waBinary.Node{{Tag: "group_update", Attrs: waBinary.Attrs{"call-id": "INCOMING"}}}}
	if c.eng.onCallRaw(raw) {
		t.Fatal("outgoing-only wrapper consumed unrelated raw call")
	}
	wa.DangerousInternals().DispatchEvent(&events.UnknownCallEvent{Node: raw})
	if legacy != 2 || sent != 0 || len(c.eng.calls) != 0 {
		t.Fatalf("legacy=%d sent=%d calls=%d", legacy, sent, len(c.eng.calls))
	}
}

func TestReinstallEventHandlerAfterLegacyClear(t *testing.T) {
	wa := whatsmeow.NewClient(&store.Device{}, waLog.Noop)
	c := NewClient(wa, WithOutgoingOnly())
	wa.RemoveEventHandlers()
	legacy := 0
	wa.AddEventHandler(func(any) { legacy++ })
	c.ReinstallEventHandler()
	c.ReinstallEventHandler()
	if n := reflect.ValueOf(wa).Elem().FieldByName("eventHandlers").Len(); n != 2 {
		t.Fatalf("handlers=%d want 2", n)
	}
	call := &Call{eng: c.eng, id: "CID", phase: CallPhaseCalling}
	c.eng.calls[call.ID()] = &engineCall{call: call, direction: CallDirectionOutgoing}
	wa.DangerousInternals().DispatchEvent(&events.CallTerminate{BasicCallMeta: types.BasicCallMeta{CallID: call.ID()}, Reason: "ended"})
	if call.State() != CallPhaseEnded || legacy != 1 {
		t.Fatalf("state=%v legacy=%d", call.State(), legacy)
	}
}
