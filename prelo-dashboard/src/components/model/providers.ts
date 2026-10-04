import type { ModelProvider } from "../../api/client";

export const PROVIDERS: Record<ModelProvider, { label: string; mono: string; description: string; keyHint: string }> = {
  anthropic: { label: "Anthropic", mono: "A", description: "Modelos Claude, por chave de API.", keyHint: "sk-ant-…" },
  openai: { label: "OpenAI", mono: "O", description: "Modelos GPT, por chave de API.", keyHint: "sk-…" },
  openai_compatible: {
    label: "Compatível com OpenAI", mono: "{ }",
    description: "Ollama, OpenRouter, Groq e afins — você informa o endereço.", keyHint: "chave do serviço",
  },
  claude_cli: { label: "Claude Code", mono: "CC", description: "Sua assinatura Claude (Pro/Max), sem chave.", keyHint: "" },
  codex_cli: { label: "Codex", mono: "CX", description: "Sua assinatura ChatGPT (Pro), sem chave.", keyHint: "" },
};

export const API_PROVIDERS: ModelProvider[] = ["anthropic", "openai", "openai_compatible"];
export const CLI_PROVIDERS: ModelProvider[] = ["claude_cli", "codex_cli"];

/** Model aliases each CLI understands; the field stays free text. */
export const CLI_MODELS: Record<string, { id: string; note: string }[]> = {
  claude_cli: [
    { id: "sonnet", note: "equilíbrio — recomendado" },
    { id: "haiku", note: "mais rápido e barato, mas erra mais ao usar ferramentas" },
    { id: "opus", note: "mais capaz, gasta mais cota" },
  ],
  codex_cli: [{ id: "default", note: "o padrão da sua conta (o primeiro da lista abaixo)" }],
};
