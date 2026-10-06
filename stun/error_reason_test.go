package stun

import "testing"

func TestErrorReasonKindDoesNotReturnWireText(t *testing.T) {
	for _, tc := range []struct{ reason, kind string }{
		{"Invalid token SECRET_VALUE", "token"},
		{"Message integrity invalid SECRET_VALUE", "integrity"},
		{"Address family not supported", "family"},
		{"Endpoint mismatch", "endpoint"},
		{"SECRET_VALUE", "unknown"},
	} {
		value := append([]byte{0, 0, 4, 52}, []byte(tc.reason)...)
		pkt := EncodeStunRequest(MsgAllocateError, [12]byte{}, stunAttr(attrErrorCode, value), nil, false)
		if got := ErrorReasonKind(pkt); got != tc.kind {
			t.Fatalf("kind=%q want=%q", got, tc.kind)
		}
	}
}
