package config

const (
	IdentifierPattern    = `^[A-Za-z0-9_-]{1,64}$`
	HTTPMethodPattern    = `^[A-Z][A-Z0-9_-]{0,31}$`
	PoweredValuePattern  = `^[^\x00-\x08\x0a-\x1f\x7f]+$`
	MaxIdentifierLength  = 64
	MaxPriority          = 1000000
	MaxConnections       = 100000
	MaxSampleEvery       = 1000000
	MaxPoweredCharacters = 128
	MaxTargets           = 1024
	MaxEntities          = 10000
)
