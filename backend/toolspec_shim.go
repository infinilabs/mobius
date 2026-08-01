package main

// Transitional aliases (plan 6.4f prep): tool specs live in internal/tools.

import (
	"mobius/internal/domain"
	"mobius/internal/tools"
)

var (
	buildAgentTools    = tools.BuildAgentTools
	checkHireDuplicate = tools.CheckHireDuplicate
	managerDirectives  = tools.ManagerDirectives
)

func hasTag(tags []string, target string) bool { return domain.HasTag(tags, target) }
