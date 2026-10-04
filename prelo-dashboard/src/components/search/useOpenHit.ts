import { touchRecent, type SearchHit } from "../../api/client";
import { useQuickView } from "../../context/QuickViewContext";
import { clientPath, navigate, projectPath } from "../../router";

/** Where a hit leads: clients and projects open their cover, a task opens the reading overlay. */
export function useOpenHit(token: string) {
  const { openTask } = useQuickView();
  return (hit: SearchHit) => {
    switch (hit.kind) {
      case "CLIENT":
        touchRecent(token, "CLIENT", hit.id);
        navigate(clientPath(hit.id));
        break;
      case "PROJECT":
        touchRecent(token, "PROJECT", hit.id);
        navigate(projectPath(hit.id, "visao-geral"));
        break;
      case "TASK":
        openTask(hit.id);
        break;
      case "FILE":
        if (hit.projectId) navigate(projectPath(hit.projectId, "arquivos"));
        break;
    }
  };
}
