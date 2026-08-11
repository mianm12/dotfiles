package converge

// Request and Build expose the private planner boundary only to the external
// convergence planner tests. Production callers enter through Analyze.
type Request = planRequest

func Build(request Request) (Plan, error) {
	return buildPlan(request)
}

func (plan Plan) HasIssues() bool {
	return len(plan.Issues) != 0
}
