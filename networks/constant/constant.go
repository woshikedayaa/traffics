package constant

import "time"

const (
	KeepAliveInitial    = 10 * time.Minute
	KeepAliveInterval   = 75 * time.Second
	KeepAliveProbeCount = 16

	DialerDefaultTimeout       = 5 * time.Second
	ResolverDefaultReadTimeout = 5 * time.Second
)

const (
	FamilyIPv4 = "4"
	FamilyIPv6 = "6"
)
