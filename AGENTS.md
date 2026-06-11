# Agent Entry

AI agents working in this repository should start with [.agent/AGENT.md](.agent/AGENT.md).

That playbook points to the CLI, Skill, repo, and security specs that define the expected contract for this project.

Before release, Functional Contract Coverage must remain 100%: every public README / Skill / reference / help / context / doctor / changelog / update behavior needs command-level tests.

Release readiness must be explicit: `reference.release_readiness` and `doctor` declare `stable`, `beta`, or `unpublishable`; `stable` requires recorded live smoke/E2E evidence.
