package meowcaller

import (
	"context"
	"errors"
	waBinary "go.mau.fi/whatsmeow/binary"
	"testing"
)

func TestFailedOutgoingOfferRemovesRegisteredCall(t *testing.T) {
	eng, call := testEngineWithOutgoingCall()
	failure := errors.New("send failed")
	eng.sendCallNode = func(context.Context, waBinary.Node) error { return failure }
	sink := &lifecycleAudioSink{}
	call.Receive(sink)
	reason := ""
	call.OnEnd(func(r string) { reason = r })
	err := eng.sendOutgoingOffer(context.Background(), call, waBinary.Node{})
	if !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
	if eng.lookup(call.ID()) != nil || call.State() != CallPhaseEnded || sink.closeCount != 1 || reason != "offer_failed" {
		t.Fatalf("cleanup: state=%v closes=%d reason=%q", call.State(), sink.closeCount, reason)
	}
}
