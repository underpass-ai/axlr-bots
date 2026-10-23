package domain

// RoutingPolicy says how the model router picks a model for the bot.
type RoutingPolicy string

const (
	// RoutingCostOptimized prefers the cheapest model that meets the task.
	RoutingCostOptimized RoutingPolicy = "cost-optimized"
	// RoutingQualityOptimized prefers the most capable model allowed.
	RoutingQualityOptimized RoutingPolicy = "quality-optimized"
	// RoutingFixed always uses the model the policy names.
	RoutingFixed RoutingPolicy = "fixed"
)

// ParseRoutingPolicy validates a routing policy; empty means cost-optimized.
func ParseRoutingPolicy(raw string) (RoutingPolicy, error) {
	switch RoutingPolicy(raw) {
	case "":
		return RoutingCostOptimized, nil
	case RoutingCostOptimized, RoutingQualityOptimized, RoutingFixed:
		return RoutingPolicy(raw), nil
	}
	return "", invalid("routing policy", "must be cost-optimized, quality-optimized or fixed")
}

func (p RoutingPolicy) String() string { return string(p) }
