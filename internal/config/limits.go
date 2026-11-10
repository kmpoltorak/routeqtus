package config

import (
	"fmt"
	"regexp"
)

const (
	HTTPMethodPattern = `^[A-Z][A-Z0-9_-]{0,31}$`
	// IdentifierLengthCeiling is the largest accepted limits.max_identifier_length.
	IdentifierLengthCeiling = 640
)

// Limits bounds operational configuration. The bootstrap `limits` section sets them.
type Limits struct {
	MaxHeaderValueBytes   int   `yaml:"max_header_value_bytes"`
	MaxConcurrentRequests int   `yaml:"max_concurrent_requests"`
	MaxRequestsPerSecond  int   `yaml:"max_requests_per_second"`
	MaxBurst              int   `yaml:"max_burst"`
	MaxTokenReferences    int   `yaml:"max_token_references"`
	MaxTokenBytes         int   `yaml:"max_token_bytes"`
	MaxRouteBodyBytes     int64 `yaml:"max_route_body_bytes"`
	MaxEntities           int   `yaml:"max_entities"`
	MaxTargets            int   `yaml:"max_targets"`
	MaxConnections        int   `yaml:"max_connections"`
	MaxPriority           int   `yaml:"max_priority"`
	MaxSampleEvery        int   `yaml:"max_sample_every"`
	MaxIdentifierLength   int   `yaml:"max_identifier_length"`
}

var DefaultLimits = Limits{MaxHeaderValueBytes: 8192, MaxConcurrentRequests: 1000000, MaxRequestsPerSecond: 1000000, MaxBurst: 1000000, MaxTokenReferences: 64, MaxTokenBytes: 4096, MaxRouteBodyBytes: 1 << 30, MaxEntities: 10000, MaxTargets: 1024, MaxConnections: 100000, MaxPriority: 1000000, MaxSampleEvery: 1000000, MaxIdentifierLength: 64}

// Validate bounds bootstrap ceilings. max_connections starts at 128, the
// default upstream max_conns and max_idle_conns, so omitted upstream fields stay valid.
func (l Limits) Validate() error {
	for _, f := range []struct {
		name          string
		value, lo, hi int
	}{
		{"max_concurrent_requests", l.MaxConcurrentRequests, 1, 10000000},
		{"max_requests_per_second", l.MaxRequestsPerSecond, 1, 10000000},
		{"max_burst", l.MaxBurst, 1, 10000000},
		{"max_token_references", l.MaxTokenReferences, 1, 640},
		{"max_token_bytes", l.MaxTokenBytes, 32, 40960},
		{"max_header_value_bytes", l.MaxHeaderValueBytes, 1, 81920},
		{"max_entities", l.MaxEntities, 1, 100000},
		{"max_targets", l.MaxTargets, 1, 10240},
		{"max_connections", l.MaxConnections, 128, 1000000},
		{"max_priority", l.MaxPriority, 1, 10000000},
		{"max_sample_every", l.MaxSampleEvery, 1, 10000000},
		{"max_identifier_length", l.MaxIdentifierLength, 1, IdentifierLengthCeiling},
	} {
		if f.value < f.lo || f.value > f.hi {
			return fmt.Errorf("limits.%s must be between %d and %d", f.name, f.lo, f.hi)
		}
	}
	if l.MaxRouteBodyBytes < 1 || l.MaxRouteBodyBytes > 1<<30 {
		return fmt.Errorf("limits.max_route_body_bytes must be between 1 and 1073741824")
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
