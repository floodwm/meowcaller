package meowcaller

import "testing"

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
