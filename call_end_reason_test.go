package meowcaller

import (
	"context"
	"testing"

	waBinary "go.mau.fi/whatsmeow/binary"
)

func TestMediaProxyFailureEndsOnceAndTerminatesCorrectCall(t *testing.T) {
	eng, call := testEngineWithOutgoingCall()
	canceled, ended := 0, 0
	eng.calls[call.ID()].cancel = func() { canceled++ }
	call.OnEnd(func(reason string) {
		ended++
		if reason != "media_proxy_failed" {
			t.Fatalf("end reason=%q", reason)
		}
	})
	var sent []waBinary.Node
	eng.sendCallNode = func(_ context.Context, node waBinary.Node) error {
		if call.State() != CallPhaseEnded || call.EndReason() != "media_proxy_failed" {
			t.Fatal("terminate sent before the terminal state was retained")
		}
		sent = append(sent, node)
		return nil
	}
	eng.finishMediaFailure(call.ID(), "media_proxy_failed")
	eng.finishMediaFailure(call.ID(), "media_proxy_failed")
	if canceled != 1 || ended != 1 || len(sent) != 1 || eng.lookup(call.ID()) != nil {
		t.Fatalf("cancel=%d end=%d terminate=%d", canceled, ended, len(sent))
	}
	node := sent[0]
	if node.Tag != "call" || node.Attrs["to"] != peerJID() || node.Attrs["id"] == "" {
		t.Fatalf("unexpected terminate envelope: %#v", node)
	}
	children := node.GetChildren()
	if len(children) != 1 || children[0].Tag != "terminate" || children[0].Attrs["call-id"] != call.ID() || children[0].Attrs["call-creator"] != creatorJID() {
		t.Fatalf("wrong call or creator: %#v", children)
	}
}

func TestCallEndReasonRetainsFirstTerminalReason(t *testing.T) {
	for _, reason := range []string{"busy", "rejected"} {
		t.Run(reason, func(t *testing.T) {
			eng, call := testEngineWithOutgoingCall()
			if got := call.EndReason(); got != "" {
				t.Fatalf("active reason=%q", got)
			}
			observed := ""
			call.OnStateChange(func(phase CallPhase) {
				if phase == CallPhaseEnded {
					observed = call.EndReason()
				}
			})
			eng.finishCall(call.ID(), reason)
			if got := call.EndReason(); got != reason || observed != reason {
				t.Fatalf("reason=%q state callback=%q", got, observed)
			}
			call.OnEnd(func(string) { t.Fatal("OnEnd must retain existing non-replaying behavior") })
			eng.finishCall(call.ID(), "remote_hangup")
			if got := call.EndReason(); got != reason {
				t.Fatalf("late reason=%q", got)
			}
		})
	}
}
