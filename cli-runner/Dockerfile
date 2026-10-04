# Fase X: Claude Code + Codex CLIs behind a tiny internal HTTP API (server.mjs). Versions are
# pinned to the ones validated with the runner's flags; bump them deliberately.
FROM node:24-slim
ARG CLAUDE_CODE_VERSION=2.1.289
ARG CODEX_VERSION=0.160.0
RUN npm install -g --omit=dev "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" "@openai/codex@${CODEX_VERSION}" \
 && npm cache clean --force \
 && useradd --create-home --uid 10001 runner
WORKDIR /app
COPY server.mjs ./
USER runner
ENV HOME=/home/runner NODE_ENV=production PORT=8099
EXPOSE 8099
HEALTHCHECK --interval=15s --timeout=5s --retries=5 CMD node -e "fetch('http://127.0.0.1:8099/health').then(r=>process.exit(r.ok?0:1)).catch(()=>process.exit(1))"
CMD ["node", "server.mjs"]
