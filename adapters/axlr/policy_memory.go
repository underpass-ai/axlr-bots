package axlr

// policyMemory is whom the policy shares the run's memory with: the team
// whose store the run opens (AXLR's docs/runtime-contract.md, "Policy per
// execution").
type policyMemory struct {
	Team string `json:"team"`
}
