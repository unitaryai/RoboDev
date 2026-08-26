# syntax=docker/dockerfile:1

# Claude Code execution environment for Osmia agent jobs.
FROM node:22-slim

# Install system dependencies.
RUN apt-get update && apt-get install -y --no-install-recommends \
        git \
        python3 \
        python3-httpx \
        curl \
        jq \
        ca-certificates \
        gnupg \
    && mkdir -p /etc/apt/keyrings \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
        | gpg --dearmor -o /etc/apt/keyrings/github-cli.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/github-cli.gpg] https://cli.github.com/packages stable main" \
        > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends gh \
    && rm -rf /var/lib/apt/lists/*

# Install glab (GitLab CLI) for merge request creation.
#
# The download is checksum-verified against the checksums.txt published with
# the release. Without that, a corrupted or substituted tarball is installed
# and executed with the agent's GitLab token.
ARG TARGETARCH
# renovate: datasource=gitlab-releases depName=gitlab-org/cli
ARG GLAB_VERSION="1.115.0"
# Docker's TARGETARCH and glab's asset names disagree in one place: Docker
# reports "arm" for linux/arm/v7, while glab publishes "armv6". Map every
# architecture it does publish, and fail loudly on anything else rather than
# guessing — an unmapped arch previously fell back to amd64, which builds a
# clean image whose glab dies with an exec format error the first time the
# agent opens a merge request.
RUN case "${TARGETARCH:-amd64}" in \
        amd64)   GLAB_ARCH="amd64" ;; \
        arm64)   GLAB_ARCH="arm64" ;; \
        arm)     GLAB_ARCH="armv6" ;; \
        386)     GLAB_ARCH="386" ;; \
        ppc64le) GLAB_ARCH="ppc64le" ;; \
        s390x)   GLAB_ARCH="s390x" ;; \
        *) echo "unsupported TARGETARCH '${TARGETARCH}': no glab release asset for it" >&2; exit 1 ;; \
    esac \
    && GLAB_BASE="https://gitlab.com/gitlab-org/cli/-/releases/v${GLAB_VERSION}/downloads" \
    && GLAB_TARBALL="glab_${GLAB_VERSION}_linux_${GLAB_ARCH}.tar.gz" \
    && curl -fsSL "${GLAB_BASE}/${GLAB_TARBALL}" -o "/tmp/${GLAB_TARBALL}" \
    && curl -fsSL "${GLAB_BASE}/checksums.txt" -o /tmp/glab-checksums.txt \
    && cd /tmp \
    # Match the filename field exactly. awk compares strings, so unlike grep
    # the dots in the name are not wildcards, and a longer name that merely
    # contains this one cannot match either. Exits nonzero if absent.
    && awk -v want="${GLAB_TARBALL}" '$2 == want { print; found = 1 } END { exit !found }' \
        /tmp/glab-checksums.txt > /tmp/glab-expected.txt \
    && sha256sum -c /tmp/glab-expected.txt \
    && tar -xzf "/tmp/${GLAB_TARBALL}" -C /tmp \
    && mv /tmp/bin/glab /usr/local/bin/glab \
    && chmod +x /usr/local/bin/glab \
    && glab --version \
    && rm -rf /tmp/glab* /tmp/bin

# Install the Claude Code CLI.
#
# --allow-scripts is required, not cosmetic. The package ships a thin JS
# launcher and downloads the platform-native binary in its postinstall. npm
# 12.0.0 made dependency install scripts opt-in, so without this the
# postinstall never runs, the native binary is absent, and `claude` fails at
# launch with "native binary not installed" — a broken image that builds
# cleanly. npm 11.18 warned about unapproved scripts but still ran them; 12.0.0
# is where the warning became a silent skip.
#
# node:22-slim currently pins npm 10, so this is insurance rather than a
# present fix: the image breaks the moment the base bumps past 12. The First
# Responder fork hit exactly this in production on 2026-07-31 after upgrading
# npm for unrelated reasons.
#
# The version check is the guard that matters. It fails the build if the
# native binary is missing for this or any other reason, so the failure can
# never reach a registry silently.
RUN npm install -g @anthropic-ai/claude-code --allow-scripts=@anthropic-ai/claude-code \
    && claude --version

# Create non-root user (GID/UID 10000 to avoid conflicts with node base image).
RUN groupadd --gid 10000 osmia \
    && useradd --uid 10000 --gid osmia --create-home osmia

# Copy entrypoint, hooks, MCP server, and setup wrapper.
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
COPY hooks/ /opt/osmia/hooks/
COPY slack_mcp.py /usr/local/bin/slack_mcp.py
COPY setup-claude.sh /usr/local/bin/setup-claude.sh

# Install MCP configuration and settings at a fixed path outside the home
# emptyDir.  setup-claude.sh copies these into ~/.claude/ and /workspace/
# at container startup so they are visible to Claude Code at runtime.
RUN mkdir -p /etc/claude-code
COPY mcp.json /etc/claude-code/mcp.json
COPY settings.json /etc/claude-code/settings.json

RUN chmod +x /usr/local/bin/entrypoint.sh \
    && chmod +x /opt/osmia/hooks/*.sh \
    && chmod +x /usr/local/bin/slack_mcp.py \
    && chmod +x /usr/local/bin/setup-claude.sh

WORKDIR /workspace
RUN chown osmia:osmia /workspace

USER osmia

ENTRYPOINT ["entrypoint.sh"]
