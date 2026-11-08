package config

import (
	"strings"
	"testing"
)

func TestFieldNamesAreCaseSensitive(t *testing.T) {
	s := strings.Replace(minimal, `"routes":[]`, `"routes":[],"METRICS":true`, 1)
	if _, err := Decode([]byte(s)); err == nil {
		t.Fatal("case-insensitive alias accepted")
	}
}
