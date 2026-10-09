export function matchPattern(pattern: string, rawURL: string): boolean {
  if (!pattern || /[\r\n]/.test(pattern)) return false;
  const parts = pattern.split("*").map((part) => part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));
  try {
    return new RegExp(`^${parts.join(".*")}$`).test(rawURL);
  } catch {
    return false;
  }
}

export function matchesAny(patterns: string[], rawURL: string): boolean {
  return patterns.some((pattern) => matchPattern(pattern, rawURL));
}

export interface CaptureRule {
  pattern: string;
  enabled: boolean;
  mode?: "include" | "exclude";
}

export function captureAllowed(rules: CaptureRule[], rawURL: string): boolean {
  const enabled = rules.filter((rule) => rule.enabled && rule.pattern);
  const includes = enabled.filter((rule) => rule.mode !== "exclude").map((rule) => rule.pattern);
  const excludes = enabled.filter((rule) => rule.mode === "exclude").map((rule) => rule.pattern);
  if (!matchesAny(includes, rawURL)) return false;
  return !matchesAny(excludes, rawURL);
}

export interface PatternConflict {
  exclude: string;
  include: string;
}

export function patternConflicts(rules: CaptureRule[]): PatternConflict[] {
  const enabled = rules.filter((rule) => rule.enabled && rule.pattern.trim());
  const includes = enabled.filter((rule) => rule.mode !== "exclude");
  const excludes = enabled.filter((rule) => rule.mode === "exclude");
  const conflicts: PatternConflict[] = [];
  for (const exclude of excludes) {
    for (const include of includes) {
      if (covers(exclude.pattern.trim(), include.pattern.trim())) {
        conflicts.push({ exclude: exclude.pattern.trim(), include: include.pattern.trim() });
      }
    }
  }
  return conflicts;
}

export function patternConflictWarnings(rules: CaptureRule[]): string[] {
  return patternConflicts(rules).map(
    (conflict) => `Exclude "${conflict.exclude}" covers include "${conflict.include}", so that include will not capture anything.`,
  );
}

function covers(exclude: string, include: string): boolean {
  const samples = ["", "a", "/item?x=1"].map((value) => include.split("*").join(value));
  return samples.every((sample) => matchPattern(exclude, sample));
}
