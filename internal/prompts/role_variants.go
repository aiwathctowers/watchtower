package prompts

// RoleInstructions provides role-specific guidance to be prepended to prompts.
// These refine how the AI should interpret and prioritize information for each role level.
var RoleInstructions = map[string]string{
	"top_management": `[ROLE CONTEXT: User is Top Management/Executive]
You are summarizing information for senior leadership. Focus on:
- Strategic signals and organizational impact
- Cross-functional dependencies and bottlenecks
- Decision quality and traceability
- Resource constraints and escalations
- Avoid: operational details, task-level granularity
Emphasize: what needs executive attention, what affects the org graph`,

	"direction_owner": `[ROLE CONTEXT: User owns direction/strategy in their domain]
You are supporting a strategic leader who also executes. Focus on:
- Domain-wide patterns and systemic issues
- Technology direction and architectural decisions
- Team coordination within their area
- Strategic initiatives and their status
- Balance strategy insight with tactical context
Emphasize: how this connects to their domain vision, resource tradeoffs`,

	"middle_management": `[ROLE CONTEXT: User is Middle Management/Team Lead]
You are supporting a coordinator and people manager. Focus on:
- Team execution and coordination needs
- Ownership clarity and assignment routing
- Priority conflicts and dependency resolution
- Team capability and capability gaps
- Individual performance signals
Emphasize: who owns what, what's blocked, coordination tax, team health`,

	"senior_ic": `[ROLE CONTEXT: User is Senior IC/Expert without direct management]
You are supporting a technical authority. Focus on:
- Technical depth and pattern analysis
- System design decisions and tradeoffs
- Knowledge and expertise leverage opportunities
- Cross-team technical coordination
- Mentoring and knowledge transfer
Emphasize: technical insights, architectural patterns, expertise application`,

	"ic": `[ROLE CONTEXT: User is Individual Contributor]
You are supporting a task-focused contributor. Focus on:
- Clear, actionable task definitions
- Required context and decision clarity
- Unblocking and dependency visibility
- Quality signals and review feedback
Emphasize: clarity of what's needed, context for action, blockers`,
}

// GetRoleInstruction returns the role-specific instruction block for a given role.
// Returns empty string if role is unknown.
func GetRoleInstruction(role string) string {
	if instr, ok := RoleInstructions[role]; ok {
		return instr
	}
	return ""
}
