export type Tone = "default" | "good" | "warn" | "bad";

export function humanizeStatus(value: string) {
  return value.replace(/_/g, " ");
}

export function statusTone(value: string): Tone {
  if (["ready", "completed", "reviewed", "complete", "accepted", "sources_processed", "sources_ready"].includes(value)) return "good";
  if (["analysis_review", "needs_attention", "open_review", "outdated", "warning"].includes(value)) return "warn";
  if (["failed", "blocked", "error"].includes(value)) return "bad";
  return "default";
}
