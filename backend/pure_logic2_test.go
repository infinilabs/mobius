package main

import (
	"testing"
)

// ---- EventConfig defaults ----

func TestEventConfigApplyDefaults(t *testing.T) {
	var ec EventConfig
	ec.ApplyDefaults()
	if ec.BufferSize == 0 || ec.BatchSize == 0 || ec.FlushIntervalS == 0 ||
		ec.RetentionDays == 0 || ec.ArchiveCron == "" {
		t.Errorf("applyDefaults left a zero field: %+v", ec)
	}

	custom := EventConfig{BufferSize: 1, BatchSize: 2, FlushIntervalS: 3, RetentionDays: 4, ArchiveCron: "* * * * *"}
	custom.ApplyDefaults()
	if custom.BufferSize != 1 || custom.ArchiveCron != "* * * * *" {
		t.Errorf("applyDefaults overrode explicit values: %+v", custom)
	}
}

// ---- skill markdown: build then parse must round-trip ----
