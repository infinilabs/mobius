package service

import (
	"context"

	"mobius/internal/domain"
)

// MaxDelegationDepth bounds delegation chains (plan 1.1). A delegated task
// carries its parent's depth + 1, so a runaway delegate spiral — including an
// A→B→A ping-pong, which depth alone cannot distinguish from a legitimate
// chain — terminates instead of recursing forever.
const MaxDelegationDepth = 5

// ExceedsDelegationDepth reports whether delegating from a task at parentDepth
// would push the chain past MaxDelegationDepth.
func ExceedsDelegationDepth(parentDepth int) bool {
	return parentDepth+1 > MaxDelegationDepth
}

// CanDelegate is the delegation policy (plan 2.4):
//   - nobody may delegate to themselves — a task loop that never converges;
//     refused for everyone, CEO included
//   - the CEO may delegate to anyone
//   - a manager may delegate only within their own subtree: direct reports and
//     transitive descendants (walked via the ManagerID chain)
//   - everything else is refused, in particular lateral manager→manager
//     delegation across teams and delegating upward to the CEO
func CanDelegate(ctx context.Context, g employeeGetter, creator, assignee *domain.Employee) bool {
	if creator.ID == assignee.ID {
		return false
	}
	if creator.Role == "CEO" {
		return true
	}
	if !domain.HasTag(creator.Tags, "manager") {
		return false
	}
	if assignee.ManagerID != nil && *assignee.ManagerID == creator.ID {
		return true
	}
	return isInManagementChain(ctx, g, creator.ID, assignee)
}
