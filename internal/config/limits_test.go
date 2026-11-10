package config

import (
	"strings"
	"testing"
)

func TestLimitsValidateRanges(t *testing.T) {
	if err := DefaultLimits.Validate(); err != nil {
		t.Fatalf("defaults rejected: %v", err)
	}
	for _, tc := range []struct {
		name   string
		set    func(*Limits, int)
		lo, hi int
	}{
		{"max_entities", func(l *Limits, v int) { l.MaxEntities = v }, 1, 100000},
		{"max_targets", func(l *Limits, v int) { l.MaxTargets = v }, 1, 10240},
		{"max_connections", func(l *Limits, v int) { l.MaxConnections = v }, 128, 1000000},
		{"max_priority", func(l *Limits, v int) { l.MaxPriority = v }, 1, 10000000},
		{"max_sample_every", func(l *Limits, v int) { l.MaxSampleEvery = v }, 1, 10000000},
		{"max_powered_characters", func(l *Limits, v int) { l.MaxPoweredCharacters = v }, 1, 1280},
		{"max_identifier_length", func(l *Limits, v int) { l.MaxIdentifierLength = v }, 1, 640},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for v, valid := range map[int]bool{tc.lo - 1: false, tc.lo: true, tc.hi: true, tc.hi + 1: false} {
				l := DefaultLimits
				tc.set(&l, v)
				err := l.Validate()
				if (err == nil) != valid {
					t.Fatalf("%s=%d valid=%v err=%v", tc.name, v, valid, err)
				}
				if err != nil && !strings.Contains(err.Error(), "limits."+tc.name) {
					t.Fatalf("error does not name the field: %v", err)
				}
			}
		})
	}
}

func TestValidIDFollowsIdentifierLength(t *testing.T) {
	t.Cleanup(func() { SetLimits(DefaultLimits) })
	l := DefaultLimits
	l.MaxIdentifierLength = 3
	SetLimits(l)
	for id, valid := range map[string]bool{"abc": true, "abcd": false, "": false, "a.b": false, "a-_": true} {
		if ValidID(id) != valid {
			t.Fatalf("ValidID(%q) != %v", id, valid)
		}
	}
	if IdentifierPattern() != `^[A-Za-z0-9_-]{1,3}$` {
		t.Fatalf("pattern %s", IdentifierPattern())
	}
}
