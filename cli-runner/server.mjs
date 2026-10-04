// Fase X: runs the owner's own Claude Code / Codex CLIs (logged in with their Pro plans) as a
// plain "text in, text out" model for llm-gateway. Only llm-gateway talks to this service (shared
// token, internal network). The CLIs are started without a shell (argument arrays), inside an
// empty temporary directory, with every built-in tool disabled — they answer, they never act.
import http from "node:http";
import { spawn } from "node:child_process";
import { mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir, homedir } from "node:os";
import { join } from "node:path";
import { timingSafeEqual } from "node:crypto";

export const MAX_BODY_BYTES = 512 * 1024;
const MODEL_PATTERN = /^[A-Za-z0-9._:\-[\]]{1,80}$/;

export function config(env = process.env) {
  return {
    token: env.CLI_RUNNER_TOKEN ?? "",
    port: Number(env.PORT ?? 8099),
    concurrency: Math.max(1, Number(env.CLI_RUNNER_CONCURRENCY ?? 2)),
    timeoutMs: Number(env.CLI_RUNNER_TIMEOUT_MS ?? 180000),
    claudeBin: env.CLAUDE_BIN ?? "claude",
    codexBin: env.CODEX_BIN ?? "codex",
    home: env.HOME ?? homedir(),
  };
}

export class RunnerError extends Error {
  constructor(code, status, message) {
    super(message);
    this.code = code;
    this.status = status;
  }
}

function turnText(m) {
  if (m.role === "assistant" && m.toolName) return `Assistente pediu a ferramenta ${m.toolName} com argumentos ${m.toolArgsJson || "{}"}`;
  if (m.role === "tool") return `Resultado da ferramenta ${m.toolName ?? ""}:\n${m.content}`;
  const label = { user: "Usuário", assistant: "Assistente" };
  return `${label[m.role] ?? m.role}:\n${m.content}`;
}

/** Flattens the gateway's conversation into one prompt the CLIs can take on stdin. */
export function buildPrompt(messages, tools = []) {
  const turns = messages.filter((m) => m.role !== "system");
  if (tools.length === 0 && turns.length === 1 && turns[0].role === "user") return turns[0].content;
  const transcript = turns.map(turnText).join("\n\n");
  if (tools.length === 0) return `${transcript}\n\nResponda como Assistente à última mensagem acima.`;
  const catalog = tools.map((t) => `- ${t.name}: ${t.description}\n  argumentos (JSON Schema): ${JSON.stringify(t.inputSchema ?? { type: "object" })}`).join("\n");
  return `Você pode usar estas ferramentas (uma por vez):\n${catalog}\n\n`
    + "Responda SEMPRE no formato estruturado pedido:\n"
    + '- para usar uma ferramenta: action="tool", tool=<nome exato>, arguments=<objeto JSON dos argumentos, como texto>, text=""\n'
    + '- para responder ao usuário: action="final", text=<resposta>, tool="", arguments=""\n'
    + "Use uma ferramenta só quando precisar de informação ou ação que ela entrega; depois do resultado, continue.\n\n"
    + `Conversa até agora:\n\n${transcript}`;
}

// Structured reply when tools are offered (OpenAI-strict friendly: every field required, arguments as text).
export const TOOL_SCHEMA = {
  type: "object",
  properties: {
    action: { type: "string", enum: ["final", "tool"] },
    text: { type: "string" },
    tool: { type: "string" },
    arguments: { type: "string" },
  },
  required: ["action", "text", "tool", "arguments"],
  additionalProperties: false,
};

/** Turns the structured reply into the gateway's shape: a final answer or one tool request. */
export function decideReply(raw, tools) {
  let parsed = raw;
  if (typeof raw === "string") {
    try { parsed = JSON.parse(raw); } catch { return { kind: "final", text: raw }; }
  }
  if (parsed?.action === "tool" && tools.some((t) => t.name === parsed.tool)) {
    let args = parsed.arguments || "{}";
    try { args = JSON.stringify(JSON.parse(args)); } catch { args = "{}"; }
    return { kind: "tool", toolName: parsed.tool, toolArgsJson: args, text: "" };
  }
  return { kind: "final", text: parsed?.text ?? "" };
}

export function systemOf(messages) {
  return messages.filter((m) => m.role === "system").map((m) => m.content).join("\n\n");
}

export function validateRun(body) {
  if (!body || typeof body !== "object") throw new RunnerError("bad_request", 400, "invalid body");
  if (body.engine !== "claude" && body.engine !== "codex") throw new RunnerError("bad_request", 400, "engine must be claude or codex");
  if (body.model != null && body.model !== "" && body.model !== "default" && !MODEL_PATTERN.test(body.model)) {
    throw new RunnerError("bad_request", 400, "invalid model name");
  }
  if (!Array.isArray(body.messages) || body.messages.length === 0) throw new RunnerError("bad_request", 400, "messages are required");
  for (const m of body.messages) {
    if (!m || typeof m.content !== "string" || !["system", "user", "assistant", "tool"].includes(m.role)) {
      throw new RunnerError("bad_request", 400, "invalid message");
    }
  }
  const tools = Array.isArray(body.tools) ? body.tools : [];
  if (tools.length > 32 || tools.some((t) => !t || typeof t.name !== "string" || !/^[a-z0-9_]{1,64}$/.test(t.name) || typeof t.description !== "string")) {
    throw new RunnerError("bad_request", 400, "invalid tools");
  }
  if (!body.messages.some((m) => m.role !== "system" && (m.content.trim() !== "" || m.toolName))) {
    throw new RunnerError("bad_request", 400, "the conversation has no message to answer");
  }
  return {
    engine: body.engine,
    model: body.model && body.model !== "default" ? body.model : null,
    messages: body.messages,
    tools,
    timeoutMs: Number.isFinite(body.timeoutMs) ? Math.min(Math.max(body.timeoutMs, 5000), 600000) : null,
  };
}

// Built-ins that would let Codex act (shell, browser, apps, images, sub-agents…) — all off.
const CODEX_DISABLED_FEATURES = [
  "shell_tool", "unified_exec", "apps", "browser_use", "browser_use_external", "computer_use",
  "image_generation", "in_app_browser", "multi_agent", "plugins", "hooks", "tool_suggest",
];

export function claudeArgs(run, system) {
  const args = ["-p", "--output-format", "json", "--tools", "", "--strict-mcp-config", "--setting-sources", "",
    "--no-session-persistence"];
  if (run.tools?.length) args.push("--json-schema", JSON.stringify(TOOL_SCHEMA));
  if (system) args.push("--system-prompt", system);
  if (run.model) args.push("--model", run.model);
  return args;
}

export function codexArgs(run, workdir, lastMessageFile, schemaFile = null) {
  const args = ["exec", "--json", "--ephemeral", "--skip-git-repo-check", "--ignore-user-config",
    "--sandbox", "read-only", "-C", workdir, "-o", lastMessageFile, "-c", 'web_search="disabled"'];
  for (const feature of CODEX_DISABLED_FEATURES) args.push("-c", `features.${feature}=false`);
  if (run.model) args.push("-m", run.model);
  if (schemaFile) args.push("--output-schema", schemaFile);
  args.push("-");
  return args;
}

/** Codex has no system-prompt flag: the instructions go first in the prompt. */
export function codexPrompt(system, prompt) {
  return system ? `<instrucoes>\n${system}\n</instrucoes>\n\n${prompt}` : prompt;
}

function classify(text) {
  const t = (text ?? "").toLowerCase();
  if (/credit balance|insufficient.{0,20}(credit|balance)|billing/.test(t)) {
    // Claude Code logged in with an API (Console) account instead of the Pro/Max subscription.
    return new RunnerError("no_credit", 402, "o CLI está logado numa conta com cobrança por API, sem crédito — refaça o login escolhendo a sua assinatura (Pro/Max)");
  }
  if (/usage limit|rate limit|quota|too many requests|limit reached|429/.test(t)) {
    return new RunnerError("rate_limited", 429, "o limite do plano foi atingido — tente mais tarde");
  }
  if (/log ?in|logged out|not logged|unauthori|authenticat|oauth|401|invalid api key|credential|token expired/.test(t)) {
    return new RunnerError("auth_required", 401, "o CLI não está logado (ou o login expirou)");
  }
  return null;
}

export function parseClaude(stdout, stderr, exitCode, requestedModel) {
  let out;
  try {
    out = JSON.parse(stdout.trim().split("\n").filter(Boolean).pop() ?? "");
  } catch {
    throw classify(stderr + stdout) ?? new RunnerError("failed", 502, `claude terminou sem resposta (código ${exitCode})`);
  }
  if (out.is_error || out.subtype !== "success") {
    throw classify(`${out.result ?? ""} ${out.subtype ?? ""} ${stderr}`) ?? new RunnerError("failed", 502, "claude não conseguiu responder");
  }
  const models = Object.keys(out.modelUsage ?? {});
  return {
    text: out.result ?? "",
    structured: out.structured_output ?? null,
    model: models[0] ?? requestedModel ?? "claude-default",
    inputTokens: (out.usage?.input_tokens ?? 0) + (out.usage?.cache_read_input_tokens ?? 0) + (out.usage?.cache_creation_input_tokens ?? 0),
    outputTokens: out.usage?.output_tokens ?? 0,
  };
}

export function parseCodex(stdout, stderr, exitCode, lastMessage, requestedModel) {
  let inputTokens = 0;
  let outputTokens = 0;
  let failure = null;
  let text = lastMessage ?? "";
  for (const line of stdout.split("\n")) {
    if (!line.trim().startsWith("{")) continue;
    let event;
    try { event = JSON.parse(line); } catch { continue; }
    if (event.type === "turn.completed" && event.usage) {
      inputTokens += event.usage.input_tokens ?? 0;
      outputTokens += event.usage.output_tokens ?? 0;
    } else if (event.type === "turn.failed" || event.type === "error") {
      failure = event.error?.message ?? event.message ?? "erro";
    } else if (event.type === "item.completed" && event.item?.type === "agent_message" && !text) {
      text = event.item.text ?? "";
    }
  }
  if (failure || (exitCode !== 0 && !text)) {
    throw classify(`${failure ?? ""} ${stderr}`) ?? new RunnerError("failed", 502, "codex não conseguiu responder");
  }
  return { text: text.trim(), model: requestedModel ?? "codex-default", inputTokens, outputTokens };
}

function execute(bin, args, { cwd, input, env, timeoutMs }) {
  return new Promise((resolve, reject) => {
    const child = spawn(bin, args, { cwd, env, stdio: ["pipe", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    const cap = (s, chunk) => (s.length < 4 * 1024 * 1024 ? s + chunk : s);
    child.stdout.on("data", (c) => { stdout = cap(stdout, c.toString()); });
    child.stderr.on("data", (c) => { stderr = cap(stderr, c.toString()); });
    const timer = setTimeout(() => {
      child.kill("SIGKILL");
      reject(new RunnerError("timeout", 504, "o CLI não respondeu a tempo"));
    }, timeoutMs);
    child.on("error", (err) => {
      clearTimeout(timer);
      reject(new RunnerError("unavailable", 503, err.code === "ENOENT" ? `${bin} não está instalado` : "falha ao iniciar o CLI"));
    });
    child.on("close", (code) => {
      clearTimeout(timer);
      resolve({ stdout, stderr, code });
    });
    child.stdin.end(input);
  });
}

/** Environment for the CLIs: only what they need — never this service's own token. */
function childEnv(cfg, extra = {}) {
  const env = { PATH: process.env.PATH, HOME: cfg.home, LANG: "C.UTF-8", NO_COLOR: "1", ...extra };
  if (process.env.CLAUDE_CODE_OAUTH_TOKEN) env.CLAUDE_CODE_OAUTH_TOKEN = process.env.CLAUDE_CODE_OAUTH_TOKEN;
  if (process.env.CODEX_HOME) env.CODEX_HOME = process.env.CODEX_HOME;
  return env;
}

export async function runOnce(cfg, run, exec = execute) {
  const workdir = await mkdtemp(join(tmpdir(), "prelo-run-"));
  const started = Date.now();
  const timeoutMs = run.timeoutMs ?? cfg.timeoutMs;
  try {
    const system = systemOf(run.messages);
    const tools = run.tools ?? [];
    const prompt = buildPrompt(run.messages, tools);
    let result;
    if (run.engine === "claude") {
      const { stdout, stderr, code } = await exec(cfg.claudeBin, claudeArgs(run, system),
        { cwd: workdir, input: prompt, env: childEnv(cfg, { CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC: "1" }), timeoutMs });
      result = parseClaude(stdout, stderr, code, run.model);
    } else {
      const lastFile = join(workdir, ".last-message");
      let schemaFile = null;
      if (tools.length) {
        schemaFile = join(workdir, ".schema.json");
        await writeFile(schemaFile, JSON.stringify(TOOL_SCHEMA));
      }
      const { stdout, stderr, code } = await exec(cfg.codexBin, codexArgs(run, workdir, lastFile, schemaFile),
        { cwd: workdir, input: codexPrompt(system, prompt), env: childEnv(cfg), timeoutMs });
      let last = null;
      try { last = await readFile(lastFile, "utf8"); } catch { /* no final message */ }
      // With no -m, Codex runs its default — the account's top-priority model; name it.
      const resolved = run.model ?? (await codexModels(cfg.home))[0]?.id ?? null;
      result = parseCodex(stdout, stderr, code, last, resolved);
    }
    if (tools.length) {
      const reply = decideReply(result.structured ?? result.text, tools);
      result = { ...result, ...reply };
    } else {
      result = { ...result, kind: "final" };
    }
    delete result.structured;
    return { ...result, engine: run.engine, durationMs: Date.now() - started };
  } finally {
    await rm(workdir, { recursive: true, force: true });
  }
}

/** Small FIFO limiter: at most `concurrency` CLIs at once (they are heavy and share one quota). */
export function limiter(concurrency) {
  let active = 0;
  const queue = [];
  const next = () => {
    if (active >= concurrency || queue.length === 0) return;
    active += 1;
    const { task, resolve, reject } = queue.shift();
    task().then(resolve, reject).finally(() => { active -= 1; next(); });
  };
  return (task) => new Promise((resolve, reject) => { queue.push({ task, resolve, reject }); next(); });
}

/**
 * Models the logged-in Codex account can use, from the CLI's own cache (~/.codex/models_cache.json),
 * listed ones only, by priority — the first is what Codex runs when no model is given.
 */
export async function codexModels(home) {
  try {
    const cache = JSON.parse(await readFile(join(home, ".codex", "models_cache.json"), "utf8"));
    return (cache.models ?? [])
      .filter((m) => m.visibility === "list" && typeof m.slug === "string")
      .sort((a, b) => (a.priority ?? 999) - (b.priority ?? 999))
      .map((m) => ({ id: m.slug, name: m.display_name ?? m.slug, description: (m.description ?? "").slice(0, 160) }));
  } catch {
    return [];
  }
}

async function exists(path) {
  try { await stat(path); return true; } catch { return false; }
}

export async function status(cfg, exec = execute) {
  const claudeLogged = !!process.env.CLAUDE_CODE_OAUTH_TOKEN || await exists(join(cfg.home, ".claude", ".credentials.json"));
  let codexLogged = false;
  try {
    const { stdout, stderr, code } = await exec(cfg.codexBin, ["login", "status"], { cwd: tmpdir(), input: "", env: childEnv(cfg), timeoutMs: 15000 });
    codexLogged = code === 0 && /logged in/i.test(stdout + stderr);
  } catch { codexLogged = false; }
  return {
    claude: { loggedIn: claudeLogged, login: "docker compose exec -it cli-runner claude  (e dentro dele: /login)" },
    codex: { loggedIn: codexLogged, login: "docker compose exec -it cli-runner codex login --device-auth", models: codexLogged ? await codexModels(cfg.home) : [] },
  };
}

function authorized(cfg, header) {
  const expected = Buffer.from(`Bearer ${cfg.token}`);
  const given = Buffer.from(header ?? "");
  return cfg.token.length >= 32 && given.length === expected.length && timingSafeEqual(given, expected);
}

function send(res, status, body) {
  res.writeHead(status, { "content-type": "application/json" });
  res.end(JSON.stringify(body));
}

export function createServer(cfg, deps = {}) {
  const limit = limiter(cfg.concurrency);
  const run = deps.runOnce ?? ((r) => runOnce(cfg, r));
  const getStatus = deps.status ?? (() => status(cfg));
  return http.createServer(async (req, res) => {
    if (req.method === "GET" && req.url === "/health") return send(res, 200, { status: "UP" });
    if (!authorized(cfg, req.headers.authorization)) return send(res, 401, { code: "unauthorized", message: "unauthorized" });
    try {
      if (req.method === "GET" && req.url === "/v1/status") return send(res, 200, await getStatus());
      if (req.method === "POST" && req.url === "/v1/run") {
        let size = 0;
        const chunks = [];
        for await (const chunk of req) {
          size += chunk.length;
          if (size > MAX_BODY_BYTES) throw new RunnerError("too_large", 413, "request too large");
          chunks.push(chunk);
        }
        let body;
        try { body = JSON.parse(Buffer.concat(chunks).toString("utf8")); } catch { throw new RunnerError("bad_request", 400, "invalid JSON"); }
        const parsed = validateRun(body);
        return send(res, 200, await limit(() => run(parsed)));
      }
      return send(res, 404, { code: "not_found", message: "not found" });
    } catch (err) {
      if (err instanceof RunnerError) return send(res, err.status, { code: err.code, message: err.message });
      console.error("cli-runner: unexpected error", err?.message);
      return send(res, 500, { code: "internal", message: "erro interno" });
    }
  });
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const cfg = config();
  if (cfg.token.length < 32) {
    console.error("CLI_RUNNER_TOKEN must be set (at least 32 characters)");
    process.exit(1);
  }
  createServer(cfg).listen(cfg.port, "0.0.0.0", () => console.log(`cli-runner listening on :${cfg.port}`));
}
