package main

// Transitional aliases (plan 6.4f prep): memory extraction lives in
// internal/memory.

import (
	"mobius/internal/memory"
)

const maxExtractionInputLen = memory.MaxExtractionInputLen

var (
	truncateForExtraction    = memory.TruncateForExtraction
	resolveModelID           = memory.ResolveModelID
	absorbMemoryFromExchange = memory.AbsorbFromExchange
)
