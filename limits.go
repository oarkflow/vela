package vela

// Limits bounds hostile or unexpectedly large payloads. Zero values select the
// defaults; callers may explicitly increase individual limits for trusted data.
// Limits are applied to both materializing and borrowed-view decoding.
type Limits struct {
	MaxBytes            int
	MaxDepth            int
	MaxNodes            int
	MaxContainerEntries int
	MaxStringBytes      int
	MaxKeyBytes         int
}

var DefaultLimits = Limits{
	MaxBytes:            64 << 20,
	MaxDepth:            64,
	MaxNodes:            1 << 20,
	MaxContainerEntries: 1 << 20,
	MaxStringBytes:      32 << 20,
	MaxKeyBytes:         1 << 20,
}

func (l Limits) normalized() Limits {
	if l.MaxBytes <= 0 {
		l.MaxBytes = DefaultLimits.MaxBytes
	}
	if l.MaxDepth <= 0 {
		l.MaxDepth = DefaultLimits.MaxDepth
	}
	if l.MaxDepth > MaxDepth {
		l.MaxDepth = MaxDepth
	}
	if l.MaxNodes <= 0 {
		l.MaxNodes = DefaultLimits.MaxNodes
	}
	if l.MaxContainerEntries <= 0 {
		l.MaxContainerEntries = DefaultLimits.MaxContainerEntries
	}
	if l.MaxStringBytes <= 0 {
		l.MaxStringBytes = DefaultLimits.MaxStringBytes
	}
	if l.MaxKeyBytes <= 0 {
		l.MaxKeyBytes = DefaultLimits.MaxKeyBytes
	}
	return l
}
