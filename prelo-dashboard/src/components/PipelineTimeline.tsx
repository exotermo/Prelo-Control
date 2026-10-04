import type { Turn } from "../api/client";

const KIND_LABEL: Record<Turn["kind"], string> = {
  LLM_CALL: "Pensamento",
  TOOL_CALL: "Ferramenta",
  SUBTASK: "Delegação",
};

const KIND_ICON: Record<Turn["kind"], string> = {
  LLM_CALL: "◆",
  TOOL_CALL: "⚙",
  SUBTASK: "⑂",
};

function nodeState(turn: Turn): "done" | "error" | "running" {
  if (turn.error) return "error";
  if (!turn.completedAt) return "running";
  return "done";
}

export function PipelineTimeline({ turns }: { turns: Turn[] }) {
  return (
    <div className="pipeline">
      {turns.map((turn, index) => {
        const state = nodeState(turn);
        return (
          <div className="pipeline-step" key={turn.turnNumber}>
            <div className="pipeline-rail">
              <span className={`pipeline-node pipeline-node-${state}`} aria-hidden="true">
                {state === "error" ? "✕" : KIND_ICON[turn.kind]}
              </span>
              {index < turns.length - 1 && <span className={`pipeline-connector pipeline-connector-${state}`} />}
            </div>
            <div className="pipeline-card">
              <div className="pipeline-card-header">
                <span className="pipeline-kind">
                  #{turn.turnNumber} · {KIND_LABEL[turn.kind]}
                </span>
                <span className="muted" style={{ fontSize: 12 }}>
                  {turn.completedAt ? new Date(turn.completedAt).toLocaleTimeString() : "em andamento…"}
                </span>
              </div>
              {turn.output && <pre>{turn.output}</pre>}
              {turn.error && <pre className="error">{turn.error}</pre>}
            </div>
          </div>
        );
      })}
    </div>
  );
}
