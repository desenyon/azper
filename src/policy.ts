import path from "node:path";
import type { HarnessMode } from "./types.js";

const HIGH_IMPACT_COMMANDS = [
  /\brm\s+(?:-[^\s]*r|--recursive)\b/i,
  /\bsudo\b/i,
  /\b(?:chmod|chown)\b/i,
  /\bgit\s+(?:push|reset\s+--hard|clean\s+-[a-z]*f)/i,
  /\b(?:npm|pnpm|yarn|bun)\s+publish\b/i,
  /\b(?:deploy|terraform\s+apply|kubectl\s+(?:apply|delete))\b/i,
  /\b(?:curl|wget)\b[^\n|;]*(?:\||>)\s*(?:sh|bash)\b/i,
];

const SAFE_REVIEW_COMMANDS = [
  /^\s*pwd\s*$/i,
  /^\s*ls(?:\s+[^;&|><`]*)?$/i,
  /^\s*rg(?:\s+[^;&|><`]*)?$/i,
  /^\s*grep(?:\s+[^;&|><`]*)?$/i,
  /^\s*git\s+(?:status|diff|log|show|branch)(?:\s+[^;&|><`]*)?$/i,
  /^\s*(?:npm\s+(?:test|run\s+(?:test|lint|build|check))|npx\s+(?:tsc|vitest))(?:\s+[^;&|><`]*)?$/i,
  /^\s*sed\s+-n\s+[^;&|><`]*$/i,
];

const PROTECTED_SEGMENTS = [".git", "node_modules", ".env", ".ssh", ".aws", ".pi/agent/auth.json"];

export type PolicyDecision =
  | { action: "allow" }
  | { action: "block"; reason: string }
  | { action: "approve"; title: string; command: string; risk: "mutation" | "dangerous" };

function stringField(input: unknown, ...keys: string[]): string {
  if (!input || typeof input !== "object") return "";
  const record = input as Record<string, unknown>;
  for (const key of keys) {
    if (typeof record[key] === "string") return record[key];
  }
  return "";
}

function pathIsProtected(workspace: string, candidate: string): boolean {
  const resolved = path.resolve(workspace, candidate);
  const relative = path.relative(workspace, resolved);
  if (relative.startsWith("..") || path.isAbsolute(relative)) return true;
  return PROTECTED_SEGMENTS.some((segment) => relative === segment || relative.startsWith(`${segment}${path.sep}`));
}

export function evaluateToolCall(
  mode: HarnessMode,
  workspace: string,
  toolName: string,
  input: unknown,
): PolicyDecision {
  if (["read", "grep", "find", "ls"].includes(toolName)) return { action: "allow" };

  if (mode === "plan") {
    return { action: "block", reason: "Plan mode is read-only. Switch modes to make changes." };
  }

  if (toolName === "write" || toolName === "edit") {
    const target = stringField(input, "path", "file_path");
    if (!target || pathIsProtected(workspace, target)) {
      return { action: "block", reason: "Azper blocked a write outside the workspace or to a protected path." };
    }
    if (mode === "auto" || mode === "full") return { action: "allow" };
    return {
      action: "approve",
      title: `${toolName === "edit" ? "Edit" : "Write"} ${path.relative(workspace, path.resolve(workspace, target))}`,
      command: target,
      risk: "mutation",
    };
  }

  if (toolName === "bash") {
    const command = stringField(input, "command");
    const dangerous = HIGH_IMPACT_COMMANDS.some((pattern) => pattern.test(command));
    if (mode === "full") return { action: "allow" };
    if (dangerous) {
      return { action: "approve", title: "Run a high-impact command", command, risk: "dangerous" };
    }
    if (mode === "auto" || SAFE_REVIEW_COMMANDS.some((pattern) => pattern.test(command))) {
      return { action: "allow" };
    }
    return { action: "approve", title: "Run a shell command", command, risk: "mutation" };
  }

  return mode === "review"
    ? { action: "approve", title: `Run ${toolName}`, command: JSON.stringify(input), risk: "mutation" }
    : { action: "allow" };
}
