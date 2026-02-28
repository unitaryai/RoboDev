# syntax=docker/dockerfile:1

# OpenCode execution environment for RoboDev agent jobs.
FROM node:22-slim

# Install system dependencies.
RUN apt-get update && apt-get install -y --no-install-recommends \
        git \
        python3 \
        curl \
        jq \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Install OpenCode CLI.
RUN npm install -g opencode

# Create non-root user.
RUN groupadd --gid 1000 robodev \
    && useradd --uid 1000 --gid robodev --create-home robodev

# Copy entrypoint.
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

WORKDIR /workspace
RUN chown robodev:robodev /workspace

USER robodev

ENTRYPOINT ["entrypoint.sh"]
