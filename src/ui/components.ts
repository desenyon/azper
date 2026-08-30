import chalk from "chalk";
import { truncateToWidth, wrapTextWithAnsi, type Component } from "@earendil-works/pi-tui";
import type { ApprovalRequest, SessionSnapshot } from "../types.js";

const accent = chalk.hex("#8aa9ff");
const cyan = chalk.hex("#73daca");
const violet = chalk.hex("#bb9af7");
const muted = chalk.hex("#666b7a");
const soft = chalk.hex("#a9adba");
const success = chalk.hex("#9ece6a");
const danger = chalk.hex("#f7768e");
const warning = chalk.hex("#e0af68");

function compactNumber(value: number): string {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}m`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(value >= 10_000 ? 0 : 1)}k`;
  return String(value);
}

function shortPath(value: string): string {
  return value.replace(/^\/Users\/[^/]+/, "~");
}

function textFromContent(content: unknown): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  return content
    .map((part) => {
      if (!part || typeof part !== "object") return "";
      const record = part as Record<string, unknown>;
      if (typeof record.text === "string") return record.text;
      if (typeof record.thinking === "string") return record.thinking;
      return "";
    })
    .filter(Boolean)
    .join("\n");
}

function wrapped(text: string, width: number, prefix = ""): string[] {
  const available = Math.max(8, width - prefix.length);
  const lines = wrapTextWithAnsi(text || " ", available);
  return lines.map((line, index) => `${index === 0 ? prefix : " ".repeat(prefix.length)}${line}`);
}

function recordOf(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" ? (value as Record<string, unknown>) : {};
}

function limitedLines(value: string, maximum = 16): { lines: string[]; omitted: number } {
  const all = value.split("\n");
  return { lines: all.slice(0, maximum), omitted: Math.max(0, all.length - maximum) };
}

export class HeaderView implements Component {
  constructor(private snapshot: SessionSnapshot) {}

  setSnapshot(snapshot: SessionSnapshot): void {
    this.snapshot = snapshot;
  }

  invalidate(): void {}

  render(width: number): string[] {
    const model = this.snapshot.model?.name ?? "no authenticated model";
    const context = `${compactNumber(this.snapshot.stats.contextTokens)}/${compactNumber(this.snapshot.stats.contextWindow)}`;
    const title = `${accent.bold("◈ AZPER")} ${muted("on Pi")}  ${soft("Build deliberately. Verify relentlessly.")}`;
    const status = [
      cyan(` ${this.snapshot.branch}`),
      muted(shortPath(this.snapshot.cwd)),
      violet(this.snapshot.mode),
      soft(`${model} · ${this.snapshot.thinkingLevel}`),
      muted(`ctx ${context}`),
    ].join(muted("  •  "));
    return [truncateToWidth(title, width), truncateToWidth(status, width), ""];
  }
}

export class TranscriptView implements Component {
  constructor(private snapshot: SessionSnapshot) {}

  setSnapshot(snapshot: SessionSnapshot): void {
    this.snapshot = snapshot;
  }

  invalidate(): void {}

  render(width: number): string[] {
    if (this.snapshot.messages.length === 0) return this.renderWelcome(width);

    const output: string[] = [];
    for (const raw of this.snapshot.messages) {
      const message = recordOf(raw);
      const role = String(message.role ?? "");
      if (role === "user") {
        output.push(...wrapped(textFromContent(message.content), width, cyan.bold("you  ")));
        output.push("");
        continue;
      }
      if (role === "assistant") {
        output.push(...this.renderAssistant(message, width));
        continue;
      }
      if (role === "toolResult") {
        output.push(...this.renderToolResult(message, width));
      }
    }

    if (this.snapshot.streaming) output.push(violet("  ◌ Pi is working…"), "");
    return output;
  }

  private renderWelcome(width: number): string[] {
    const lines = [
      accent.bold("  A focused coding-agent terminal"),
      soft("  Persistent Pi sessions, tuned compaction, native skills, and guarded tools."),
      "",
      `${violet("  review")} ${muted("asks before mutations")}   ${cyan("shift+tab")} ${muted("cycles modes")}`,
      `${cyan("  /help")} ${muted("shows commands")}             ${cyan("ctrl+q")} ${muted("exits safely")}`,
      "",
      muted("  Try: Review the current repository and propose the highest-leverage improvement."),
    ];
    if (this.snapshot.error) lines.push("", danger(`  Pi could not start: ${this.snapshot.error}`));
    if (this.snapshot.connected && this.snapshot.models.length === 0) {
      lines.push("", warning("  No authenticated model found. Run `pi` once to configure a provider."));
    }
    return lines.flatMap((line) => wrapped(line, width));
  }

  private renderAssistant(message: Record<string, unknown>, width: number): string[] {
    const output: string[] = [];
    const blocks = Array.isArray(message.content) ? message.content : [];
    for (const rawBlock of blocks) {
      const block = recordOf(rawBlock);
      if (block.type === "thinking" && typeof block.thinking === "string") {
        output.push(...wrapped(chalk.italic(muted(block.thinking)), width, violet("think  ")));
        output.push("");
      } else if (block.type === "text" && typeof block.text === "string") {
        output.push(...wrapped(block.text, width, accent.bold("pi   ")));
        output.push("");
      } else if (block.type === "toolCall") {
        output.push(...this.renderToolCall(block, width), "");
      }
    }
    return output;
  }

  private renderToolCall(block: Record<string, unknown>, width: number): string[] {
    const name = String(block.name ?? "tool");
    const args = recordOf(block.arguments);
    if (name === "bash") {
      return wrapped(String(args.command ?? "shell"), width, warning("$    "));
    }
    if (name === "edit") {
      const path = String(args.path ?? args.file_path ?? "file");
      const oldText = String(args.oldText ?? args.old_string ?? "");
      const newText = String(args.newText ?? args.new_string ?? "");
      const output = [truncateToWidth(`${violet("edit ")}${soft(path)}`, width)];
      for (const line of limitedLines(oldText, 8).lines) output.push(truncateToWidth(danger(`  - ${line}`), width));
      for (const line of limitedLines(newText, 8).lines) output.push(truncateToWidth(success(`  + ${line}`), width));
      return output;
    }
    const path = String(args.path ?? args.file_path ?? "");
    return [truncateToWidth(`${violet("  ●")} ${soft(name)}${path ? ` ${muted(path)}` : ""}`, width)];
  }

  private renderToolResult(message: Record<string, unknown>, width: number): string[] {
    const body = textFromContent(message.content);
    const { lines, omitted } = limitedLines(body, 12);
    const failed = Boolean(message.isError);
    const output = [
      truncateToWidth(`${failed ? danger("  ✕") : success("  ✓")} ${soft(String(message.toolName ?? "tool"))}`, width),
    ];
    for (const line of lines) output.push(truncateToWidth(muted(`    ${line}`), width));
    if (omitted) output.push(muted(`    … ${omitted} more lines`));
    output.push("");
    return output;
  }
}

export class ApprovalView implements Component {
  constructor(private request?: ApprovalRequest) {}

  setRequest(request?: ApprovalRequest): void {
    this.request = request;
  }

  invalidate(): void {}

  render(width: number): string[] {
    if (!this.request) return [];
    const risk = this.request.risk === "dangerous" ? danger.bold("HIGH IMPACT") : warning.bold("APPROVAL");
    return [
      truncateToWidth(`${risk}  ${soft(this.request.title)}`, width),
      ...wrapped(muted(this.request.command), width, "  "),
      truncateToWidth(`${cyan.bold("y")} allow once   ${cyan.bold("a")} always allow ${this.request.toolName}   ${danger.bold("n")} reject`, width),
      "",
    ];
  }
}

export class FooterView implements Component {
  private notice?: { level: "info" | "error" | "success"; message: string };

  constructor(private snapshot: SessionSnapshot) {}

  setSnapshot(snapshot: SessionSnapshot): void {
    this.snapshot = snapshot;
  }

  setNotice(notice?: { level: "info" | "error" | "success"; message: string }): void {
    this.notice = notice;
  }

  invalidate(): void {}

  render(width: number): string[] {
    const modeDescription = {
      review: "approve mutations",
      plan: "read only",
      auto: "approve high impact",
      full: "unprompted workspace tools",
    }[this.snapshot.mode];
    const shortcuts = `${violet(this.snapshot.mode)} ${muted(modeDescription)}  ${muted("│")}  ${soft("↵ send  shift+↵ newline  shift+tab mode  ctrl+t thinking  ctrl+k compact  ctrl+n new  ctrl+q quit")}`;
    const output = [truncateToWidth(shortcuts, width)];
    if (this.notice) {
      const paint = this.notice.level === "error" ? danger : this.notice.level === "success" ? success : accent;
      output.push(truncateToWidth(paint(this.notice.message), width));
    }
    return output;
  }
}
