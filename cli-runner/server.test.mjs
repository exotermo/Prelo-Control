import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  buildPrompt, codexModels, claudeArgs, codexArgs, codexPrompt, createServer, limiter, parseClaude, parseCodex,
  runOnce, RunnerError, systemOf, validateRun,
} from "./server.mjs";

const TOKEN = "t".repeat(40);

test("single user message is passed as-is; conversations become a transcript", () => {
  assert.equal(buildPrompt([{ role: "system", content: "s" }, { role: "user", content: "oi" }]), "oi");
  const p = buildPrompt([{ role: "user", content: "a" }, { role: "assistant", content: "b" }, { role: "user", content: "c" }]);
  assert.match(p, /Usuário:\na\n\nAssistente:\nb\n\nUsuário:\nc/);
  assert.equal(systemOf([{ role: "system", content: "x" }, { role: "user", content: "y" }, { role: "system", content: "z" }]), "x\n\nz");
});

test("validation rejects unknown engines, odd model names and bad messages", () => {
  assert.throws(() => validateRun({ engine: "bash", messages: [{ role: "user", content: "x" }] }), RunnerError);
  assert.throws(() => validateRun({ engine: "claude", model: "a b; rm -rf", messages: [{ role: "user", content: "x" }] }), RunnerError);
  assert.throws(() => validateRun({ engine: "codex", messages: [] }), RunnerError);
  assert.throws(() => validateRun({ engine: "codex", messages: [{ role: "root", content: "x" }] }), RunnerError);
  assert.throws(() => validateRun({ engine: "codex", messages: [{ role: "system", content: "x" }, { role: "user", content: "  " }] }), RunnerError);
  assert.equal(validateRun({ engine: "codex", model: "default", messages: [{ role: "user", content: "x" }] }).model, null);
  assert.equal(validateRun({ engine: "claude", model: "claude-sonnet-4-6[1m]", messages: [{ role: "user", content: "x" }] }).model, "claude-sonnet-4-6[1m]");
});

test("claude runs with every tool, MCP server and setting source disabled", () => {
  const args = claudeArgs({ model: "sonnet" }, "seja breve");
  const at = (flag) => args[args.indexOf(flag) + 1];
  assert.equal(at("--tools"), "");
  assert.equal(at("--setting-sources"), "");
  assert.ok(args.includes("--strict-mcp-config"));
  assert.ok(args.includes("--no-session-persistence"));
  assert.equal(at("--system-prompt"), "seja breve");
  assert.equal(at("--model"), "sonnet");
});

test("codex runs read-only, ephemeral, without shell/browser/apps and without user config", () => {
  const args = codexArgs({ model: null }, "/tmp/w", "/tmp/w/.last");
  assert.equal(args[args.indexOf("--sandbox") + 1], "read-only");
  for (const f of ["shell_tool", "unified_exec", "apps", "browser_use", "computer_use", "multi_agent", "plugins"]) {
    assert.ok(args.includes(`features.${f}=false`), f);
  }
  assert.ok(args.includes("--ignore-user-config") && args.includes("--ephemeral"));
  assert.ok(!args.includes("-m"), "default model must not pass -m");
  assert.equal(args.at(-1), "-");
  assert.match(codexPrompt("regras", "pergunta"), /^<instrucoes>\nregras\n<\/instrucoes>\n\npergunta$/);
});

test("claude output: success, plan limit and logged-out are told apart", () => {
  const ok = parseClaude(JSON.stringify({ type: "result", subtype: "success", is_error: false, result: "olá",
    usage: { input_tokens: 10, cache_read_input_tokens: 5, output_tokens: 3 }, modelUsage: { "claude-sonnet-4-6": {} } }), "", 0, "sonnet");
  assert.deepEqual(ok, { text: "olá", model: "claude-sonnet-4-6", inputTokens: 15, outputTokens: 3 });
  assert.throws(() => parseClaude(JSON.stringify({ subtype: "success", is_error: true, result: "Claude AI usage limit reached|1700000000" }), "", 1),
    (e) => e.code === "rate_limited" && e.status === 429);
  assert.throws(() => parseClaude(JSON.stringify({ subtype: "success", is_error: true, result: "Invalid API key · Please run /login" }), "", 1),
    (e) => e.code === "auth_required");
  assert.throws(() => parseClaude("not json", "boom", 2), (e) => e.code === "failed");
  assert.throws(() => parseClaude(JSON.stringify({ subtype: "success", is_error: true, result: "Credit balance is too low" }), "", 0),
    (e) => e.code === "no_credit" && /assinatura/.test(e.message));
});

test("codex output: last message, token usage and failures", () => {
  const stdout = [
    JSON.stringify({ type: "thread.started", thread_id: "x" }),
    JSON.stringify({ type: "item.completed", item: { type: "agent_message", text: "resposta" } }),
    JSON.stringify({ type: "turn.completed", usage: { input_tokens: 20, output_tokens: 7 } }),
  ].join("\n");
  assert.deepEqual(parseCodex(stdout, "", 0, "resposta final\n", null), { text: "resposta final", model: "codex-default", inputTokens: 20, outputTokens: 7 });
  assert.equal(parseCodex(stdout, "", 0, null, "gpt-x").text, "resposta");
  const failed = JSON.stringify({ type: "turn.failed", error: { message: "You've hit your usage limit" } });
  assert.throws(() => parseCodex(failed, "", 1, null, null), (e) => e.code === "rate_limited");
  assert.throws(() => parseCodex("", "Not logged in", 1, null, null), (e) => e.code === "auth_required");
});

test("runOnce spawns the right binary in a throwaway directory and never forwards the runner token", async () => {
  process.env.CLI_RUNNER_TOKEN = TOKEN;
  const calls = [];
  const fakeExec = async (bin, args, opts) => {
    calls.push({ bin, args, opts });
    return { stdout: JSON.stringify({ subtype: "success", is_error: false, result: "ok", usage: {} }), stderr: "", code: 0 };
  };
  const cfg = { claudeBin: "claude", codexBin: "codex", home: "/home/runner", timeoutMs: 1000 };
  const out = await runOnce(cfg, { engine: "claude", model: null, messages: [{ role: "user", content: "oi" }] }, fakeExec);
  assert.equal(out.text, "ok");
  assert.equal(calls[0].bin, "claude");
  assert.equal(calls[0].opts.input, "oi");
  assert.match(calls[0].opts.cwd, /prelo-run-/);
  assert.equal(calls[0].opts.env.CLI_RUNNER_TOKEN, undefined);
  delete process.env.CLI_RUNNER_TOKEN;
});

test("limiter never runs more than N tasks at once", async () => {
  const limit = limiter(2);
  let running = 0;
  let peak = 0;
  const task = () => limit(async () => {
    running += 1; peak = Math.max(peak, running);
    await new Promise((r) => setTimeout(r, 20));
    running -= 1;
  });
  await Promise.all([task(), task(), task(), task(), task()]);
  assert.equal(peak, 2);
});

test("HTTP: token required, errors mapped, runs dispatched", async () => {
  const server = createServer({ token: TOKEN, concurrency: 1 }, {
    runOnce: async (r) => {
      if (r.messages[0].content === "limite") throw new RunnerError("rate_limited", 429, "limite");
      return { text: `eco: ${r.messages[0].content}`, engine: r.engine };
    },
    status: async () => ({ claude: { loggedIn: true }, codex: { loggedIn: false } }),
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const base = `http://127.0.0.1:${server.address().port}`;
  const auth = { authorization: `Bearer ${TOKEN}`, "content-type": "application/json" };
  try {
    assert.equal((await fetch(`${base}/health`)).status, 200);
    assert.equal((await fetch(`${base}/v1/status`)).status, 401);
    assert.equal((await fetch(`${base}/v1/status`, { headers: { authorization: "Bearer errado" } })).status, 401);
    assert.equal((await (await fetch(`${base}/v1/status`, { headers: auth })).json()).claude.loggedIn, true);
    const ok = await fetch(`${base}/v1/run`, { method: "POST", headers: auth, body: JSON.stringify({ engine: "codex", messages: [{ role: "user", content: "oi" }] }) });
    assert.equal((await ok.json()).text, "eco: oi");
    const limited = await fetch(`${base}/v1/run`, { method: "POST", headers: auth, body: JSON.stringify({ engine: "codex", messages: [{ role: "user", content: "limite" }] }) });
    assert.equal(limited.status, 429);
    assert.equal((await limited.json()).code, "rate_limited");
    const bad = await fetch(`${base}/v1/run`, { method: "POST", headers: auth, body: "{" });
    assert.equal(bad.status, 400);
  } finally {
    server.close();
  }
});

test("codex models come from the CLI cache: listed only, by priority", async () => {
  const home = await mkdtemp(join(tmpdir(), "home-"));
  await mkdir(join(home, ".codex"));
  await writeFile(join(home, ".codex", "models_cache.json"), JSON.stringify({ models: [
    { slug: "b", display_name: "B", visibility: "list", priority: 2 },
    { slug: "hidden", visibility: "hide", priority: 0 },
    { slug: "a", display_name: "A", visibility: "list", priority: 1, description: "top" },
  ] }));
  assert.deepEqual((await codexModels(home)).map((m) => m.id), ["a", "b"]);
  assert.deepEqual(await codexModels("/nao/existe"), []);
});
