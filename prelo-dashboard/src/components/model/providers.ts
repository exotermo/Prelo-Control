import type { ModelProvider } from "../../api/client";

export const PROVIDERS: Record<ModelProvider, { label: string; mono: string; description: string; keyHint: string }> = {
  anthropic: { label: "Anthropic", mono: "A", description: "Modelos Claude.", keyHint: "sk-ant-…" },
  openai: { label: "OpenAI", mono: "O", description: "Modelos GPT.", keyHint: "sk-…" },
  openai_compatible: {
    label: "Compatível com OpenAI", mono: "{ }",
    description: "Ollama, OpenRouter, Groq e afins — você informa o endereço.", keyHint: "chave do serviço",
  },
};
