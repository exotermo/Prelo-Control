import type { CoverColor } from "../api/client";

// Fase PA cover palette — all dark enough for the cover title on cream paper (≥ 4.5:1).
export const COVER_COLORS: Record<CoverColor, { label: string; hex: string }> = {
  ink: { label: "Nanquim", hex: "#1e1b16" },
  clay: { label: "Argila", hex: "#a5461b" },
  moss: { label: "Musgo", hex: "#3d6234" },
  ocean: { label: "Oceano", hex: "#1d5574" },
  plum: { label: "Ameixa", hex: "#6a3659" },
  mustard: { label: "Mostarda", hex: "#7f5a00" },
};
