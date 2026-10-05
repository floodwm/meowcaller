package meowcaller

import (
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"testing"
)

func TestOutgoingRejectDeviceScope(t *testing.T) {
	for _, tc := range []struct {
		name     string
		device   uint16
		reason   string
		accepted uint16
		end      bool
	}{
		{"secondary unavailable", 5, "", 0, false},
		{"secondary capability error", 5, "enc", 0, false},
		{"primary declined", 0, "", 0, true},
		{"secondary explicit refusal", 5, "rejected", 0, true},
		{"secondary busy", 5, "busy", 0, true},
		{"primary busy", 0, "busy", 0, true},
		{"chosen device busy", 2, "busy", 2, true},
		{"other device after acceptance", 5, "busy", 2, false},
		{"chosen device declined", 2, "", 2, true},
		{"primary after another device acceptance", 0, "", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, call := testEngineWithOutgoingCall()
			if tc.accepted != 0 {
				from := peerJID()
				from.Device = tc.accepted
				eng.onAccept(&events.CallAccept{BasicCallMeta: types.BasicCallMeta{CallID: call.ID(), From: from}})
			}
			from := peerJID()
			from.Device = tc.device
			reason := ""
			call.OnEnd(func(r string) { reason = r })
			eng.onReject(&events.CallReject{BasicCallMeta: types.BasicCallMeta{CallID: call.ID(), From: from}, Data: &waBinary.Node{Tag: "reject", Attrs: waBinary.Attrs{"reason": tc.reason}}})
			if ended := call.State() == CallPhaseEnded; ended != tc.end {
				t.Fatalf("ended=%v, want %v", ended, tc.end)
			}
			wantReason := "rejected"
			if tc.reason == "busy" {
				wantReason = "busy"
			}
			if tc.end && reason != wantReason {
				t.Fatalf("end reason=%q", reason)
			}
		})
	}
}
