package config

import (
	"fmt"
	"regexp"
)

const (
	HTTPMethodPattern   = `^[A-Z][A-Z0-9_-]{0,31}$`
	PoweredValuePattern = `^[^\x00-\x08\x0a-\x1f\x7f]+$`
	// IdentifierLengthCeiling is the largest accepted limits.max_identifier_length.
	IdentifierLengthCeiling = 640
)

// Limits bounds operational configuration. The bootstrap `limits` section sets them.
type Limits struct {
	MaxEntities          int `yaml:"max_entities"`
	MaxTargets           int `yaml:"max_targets"`
	MaxConnections       int `yaml:"max_connections"`
	MaxPriority          int `yaml:"max_priority"`
	MaxSampleEvery       int `yaml:"max_sample_every"`
	MaxPoweredCharacters int `yaml:"max_powered_characters"`
	MaxIdentifierLength  int `yaml:"max_identifier_length"`
}

var DefaultLimits = Limits{MaxEntities: 10000, MaxTargets: 1024, MaxConnections: 100000, MaxPriority: 1000000, MaxSampleEvery: 1000000, MaxPoweredCharacters: 128, MaxIdentifierLength: 64}

// Validate accepts 1 through 10x each default. max_connections starts at 128, the
// default upstream max_conns and max_idle_conns, so omitted upstream fields stay valid.
func (l Limits) Validate() error {
	for _, f := range []struct {
		name          string
		value, lo, hi int
	}{
		{"max_entities", l.MaxEntities, 1, 100000},
		{"max_targets", l.MaxTargets, 1, 10240},
		{"max_connections", l.MaxConnections, 128, 1000000},
		{"max_priority", l.MaxPriority, 1, 10000000},
		{"max_sample_every", l.MaxSampleEvery, 1, 10000000},
		{"max_powered_characters", l.MaxPoweredCharacters, 1, 1280},
		{"max_identifier_length", l.MaxIdentifierLength, 1, IdentifierLengthCeiling},
	} {
		if f.value < f.lo || f.value > f.hi {
			return fmt.Errorf("limits.%s must be between %d and %d", f.name, f.lo, f.hi)
		}
	}
	return nil
}

// ponytail: one process-wide set, written once at startup by app.Compiler before any
// compile or schema; pass Limits explicitly if one process ever needs two sets.
var current = DefaultLimits

func SetLimits(l Limits)    { current = l }
func CurrentLimits() Limits { return current }

var identifierCharacters = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// IdentifierPattern is the schema pattern for the current max_identifier_length.
func IdentifierPattern() string {
	return fmt.Sprintf(`^[A-Za-z0-9_-]{1,%d}$`, current.MaxIdentifierLength)
}

// ValidID reports whether s is 1 to max_identifier_length ASCII letters, digits, '_' or '-'.
func ValidID(s string) bool {
	return len(s) <= current.MaxIdentifierLength && identifierCharacters.MatchString(s)
}
