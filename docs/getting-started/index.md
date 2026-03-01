# Getting Started

Welcome to RoboDev! Choose the path that fits your experience level.

<div class="grid cards" markdown>

-   :material-docker:{ .lg .middle } **I want to try it locally**

    ---

    No Kubernetes cluster needed. Get RoboDev running with Docker Compose in 5 minutes.

    **Best for:** first-time users, quick evaluation, learning how RoboDev works.

    [:octicons-arrow-right-24: Docker Compose quick start](docker-compose.md)

-   :material-kubernetes:{ .lg .middle } **I have a Kubernetes cluster**

    ---

    Deploy RoboDev with Helm, connect it to GitHub Issues and Claude Code, and run your first automated task.

    **Best for:** K8s engineers, production deployments, team evaluations.

    [:octicons-arrow-right-24: Kubernetes quick start](kubernetes.md)

</div>

## What You'll Need

Regardless of which path you choose, you will need:

- A **GitHub repository** for the agent to work on
- A **GitHub personal access token** with `repo` and `issues` scopes
- An **Anthropic API key** (for Claude Code) or an **OpenAI API key** (for Codex)

## After Setup

Once you have RoboDev running, explore these next:

- [Configuration Reference](configuration.md) — full details on every config option
- [Troubleshooting](troubleshooting.md) — common issues and how to fix them
- [What is RoboDev?](../concepts/what-is-robodev.md) — understand the concepts behind the tool
- [Guard Rails Overview](../concepts/guardrails-overview.md) — learn about the safety layers
