package middleware

import "testing"

func TestHeaderValuesRejectControls(t *testing.T) {
	for _, v := range []string{"hello\r\nInjected: true", "bad\x00", "bad\x01", "bad\x7f"} {
		if ValidHeaderValue(v) {
			t.Fatal("control character accepted")
		}
	}
	if !ValidHeaderValue("value\twith tab") {
		t.Fatal("valid header rejected")
	}
}
