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
RUN GLAB_ARCH="${TARGETARCH:-amd64}" \
    && GLAB_BASE="https://gitlab.com/gitlab-org/cli/-/releases/v${GLAB_VERSION}/downloads" \
    && GLAB_TARBALL="glab_${GLAB_VERSION}_linux_${GLAB_ARCH}.tar.gz" \
    && curl -fsSL "${GLAB_BASE}/${GLAB_TARBALL}" -o "/tmp/${GLAB_TARBALL}" \
    && curl -fsSL "${GLAB_BASE}/checksums.txt" -o /tmp/glab-checksums.txt \
    && cd /tmp \
    # grep -F so no filename character is read as a pattern, and the trailing
    # anchor so a name that merely contains this one cannot match.
    && grep -F " ${GLAB_TARBALL}" /tmp/glab-checksums.txt | grep -- "${GLAB_TARBALL}$" > /tmp/glab-expected.txt \
    && test -s /tmp/glab-expected.txt \
    && sha256sum -c /tmp/glab-expected.txt \
    && tar -xzf "/tmp/${GLAB_TARBALL}" -C /tmp \
    && mv /tmp/bin/glab /usr/local/bin/glab \
    && chmod +x /usr/local/bin/glab \
    && glab --version \
    && rm -rf /tmp/glab* /tmp/bin

# Install the Claude Code CLI.
#
# --allow-scripts is required, not cosmetic. The package ships a thin JS
# launcher and downloads the platform-native binary in its postinstall. npm 11
# and later block install scripts by default, so without this the postinstall
# never runs, the native binary is absent, and `claude` fails at launch with
# "native binary not installed" — a broken image that builds cleanly.
#
# node:22-slim currently pins npm 10, where scripts still run, so this is
# insurance rather than a present fix: the image breaks the moment the base
# bumps to npm 11. The First Responder fork hit exactly this in production on
# 2026-07-31 after upgrading npm.
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
