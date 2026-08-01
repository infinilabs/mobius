package service

import (
	"context"
	"log/slog"

	"mobius/internal/domain"
)

// TokenLedger is the monthly token usage source for the budget gate;
// *postgres.Client satisfies it via the heartbeat_runs ledger.
type TokenLedger interface {
	MonthTokens(ctx context.Context, agentID string) (int64, error)
}

// BudgetExceeded reports whether the agent's month-to-date token spend has
// reached its configured monthly budget (budget unit = kilotokens). Fails
// open: a ledger error allows execution rather than deadlocking the fleet on
// a read hiccup.
func BudgetExceeded(ctx context.Context, ledger TokenLedger, agent *domain.Employee) bool {
	if agent.MonthlyBudget == nil || *agent.MonthlyBudget <= 0 {
		return false
	}

	totalTokens, err := ledger.MonthTokens(ctx, agent.ID)
	if err != nil {
		slog.Warn("budget check failed, allowing execution", "agent_id", agent.ID, "error", err)
		return false
	}

	budgetTokens := int64(*agent.MonthlyBudget) * 1000
	if totalTokens >= budgetTokens {
		slog.Warn("agent budget exceeded",
			"agent_id", agent.ID, "agent_name", agent.Name,
			"used_tokens", totalTokens, "budget_tokens", budgetTokens)
		return true
	}
	return false
}
