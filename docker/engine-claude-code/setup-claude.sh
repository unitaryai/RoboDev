#!/bin/sh
# setup-claude.sh — initialises Claude Code user config before running the agent.
#
# The home directory (/home/osmia) is an emptyDir volume that shadows any
# files baked into the image.  This script replicates the config files at
# container startup, matching the approach used in the PoC:
#   1. ~/.claude/settings.json  — grants permission for MCP tool use
#   2. /workspace/.mcp.json     — registers the osmia-slack MCP server
#      (project-scope file that Claude Code auto-loads from the cwd)
#   3. ~/.claude/skills/*.md    — custom skills injected via env vars (optional)
#
# The main claude invocation also passes --mcp-config /workspace/.mcp.json
# as an explicit belt-and-suspenders load path.
#
# Session persistence support:
#   CLAUDE_CONFIG_DIR    — overrides ~/.claude; set by the session store to
#                          point at a PVC-backed directory so that session
#                          JSONL files survive pod restarts.
#   OSMIA_WORKSPACE_DIR  — when set and already populated (a .git directory
#                          is present), the git-clone step is skipped so the
#                          agent continues in the same workspace.
#
# Skill environment variables (set by the Osmia controller):
#   CLAUDE_SKILL_INLINE_<NAME>  — base64-encoded Markdown content for an inline skill
#   CLAUDE_SKILL_PATH_<NAME>    — path to a skill file on the container image
#
# NAME is the skill name with non-alphanumeric characters replaced by
# underscores and converted to uppercase (e.g. CREATE_CHANGELOG).
# The filename written is the lowercase, hyphenated form (e.g. create-changelog.md).

set -eu

# Use CLAUDE_CONFIG_DIR when set (session persistence), otherwise ~/.claude.
CLAUDE_DIR="${CLAUDE_CONFIG_DIR:-${HOME}/.claude}"

# Always create the directory — it will either be the PVC-backed path or
# the ephemeral emptyDir home.
mkdir -p "${CLAUDE_DIR}"

# Overwrite settings.json and the MCP config unconditionally — these are
# controlled by the operator and must reflect the current policy even on
# resumed sessions.  Session JSONL files are left untouched.
cp /etc/claude-code/settings.json "${CLAUDE_DIR}/settings.json"

# Copy MCP server config to the project root so it is auto-loaded and also
# available via the explicit --mcp-config flag in the claude invocation.
cp /etc/claude-code/mcp.json /workspace/.mcp.json

# Export CLAUDE_CONFIG_DIR so claude itself picks it up when the non-default
# path is in use.  A no-op when CLAUDE_CONFIG_DIR was already set in the env.
export CLAUDE_CONFIG_DIR="${CLAUDE_DIR}"

# Write custom skill files if any skill env vars are present. Skills go
# to ${HOME}/.claude/skills/<name>/SKILL.md — directory per skill, file
# named exactly SKILL.md — because that is Claude Code's documented
# discovery layout (the same shape used for plugin-provided skills under
# ~/.claude/plugins/.../skills/<name>/SKILL.md). A flat
# ${SKILLS_DIR}/<name>.md file is not discovered by the slash-command
# loader; invocations fail with "Unknown skill: <name>" even when the
# file exists.
#
# Skills are deterministically regenerated from the CLAUDE_SKILL_* env
# vars at every pod start, so they live on the pod's ephemeral
# ${HOME}/.claude/ rather than under CLAUDE_CONFIG_DIR. The relevant
# discovery paths are documented at
# https://code.claude.com/docs/en/skills.md and are not relocatable
# via env var today (see anthropics/claude-code#22902).
#
# Inline skills: CLAUDE_SKILL_INLINE_<NAME>=<base64-encoded Markdown>
# Path skills:   CLAUDE_SKILL_PATH_<NAME>=<path on image>
if env | grep -q '^CLAUDE_SKILL_'; then
    SKILLS_DIR="${HOME}/.claude/skills"
    mkdir -p "${SKILLS_DIR}"

    # Write inline skills (base64-decoded content).
    for var in $(env | grep '^CLAUDE_SKILL_INLINE_' | sed 's/=.*//'); do
        name=$(printf '%s' "$var" | sed 's/^CLAUDE_SKILL_INLINE_//' | tr '[:upper:]' '[:lower:]' | tr '_' '-')
        mkdir -p "${SKILLS_DIR}/${name}"
        printenv "$var" | base64 -d > "${SKILLS_DIR}/${name}/SKILL.md"
    done

    # Copy path-based skills from the image.
    for var in $(env | grep '^CLAUDE_SKILL_PATH_' | sed 's/=.*//'); do
        name=$(printf '%s' "$var" | sed 's/^CLAUDE_SKILL_PATH_//' | tr '[:upper:]' '[:lower:]' | tr '_' '-')
        path=$(printenv "$var")
        if [ -f "$path" ]; then
            mkdir -p "${SKILLS_DIR}/${name}"
            cp "$path" "${SKILLS_DIR}/${name}/SKILL.md"
        fi
    done
fi

# Write ConfigMap-backed sub-agent files to ${HOME}/.claude/agents/.
# Same HOME-relative reasoning as skills above: Claude Code's sub-agent
# discovery uses the HOME-relative path regardless of CLAUDE_CONFIG_DIR,
# so writing under ${CLAUDE_DIR} when session persistence is on would
# make the sub-agent invisible to the agent.
#
# Sub-agent env vars: CLAUDE_SUBAGENT_PATH_<NAME>=<path on volume>
if env | grep -q '^CLAUDE_SUBAGENT_PATH_'; then
    AGENTS_DIR="${HOME}/.claude/agents"
    mkdir -p "${AGENTS_DIR}"
    for var in $(env | grep '^CLAUDE_SUBAGENT_PATH_' | sed 's/=.*//'); do
        name=$(printf '%s' "$var" | sed 's/^CLAUDE_SUBAGENT_PATH_//' | tr '[:upper:]' '[:lower:]' | tr '_' '-')
        path=$(printenv "$var")
        [ -f "$path" ] && cp "$path" "${AGENTS_DIR}/${name}.md"
    done
fi

exec claude "$@"
