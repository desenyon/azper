import { execFileSync } from "node:child_process";
import type { ThinkingLevel } from "@earendil-works/pi-agent-core";
import {
  createAgentSession,
  DefaultResourceLoader,
  getAgentDir,
  ModelRuntime,
  SessionManager,
  SettingsManager,
  type AgentSession,
  type InlineExtension,
} from "@earendil-works/pi-coding-agent";
import { ApprovalBroker } from "./approval-broker.js";
import { evaluateToolCall } from "./policy.js";
import { AZPER_SYSTEM_GUIDANCE } from "./prompt.js";
import type { HarnessEvent, HarnessMode, SessionSnapshot, WireModel } from "./types.js";

const FULL_TOOLS = ["read", "bash", "edit", "write", "grep", "find", "ls"];
const PLAN_TOOLS = ["read", "grep", "find", "ls"];
const THINKING_LEVELS = ["off", "minimal", "low", "medium", "high", "xhigh", "max"] as const;

function asWireModel(model: {
  id: string;
  provider: string;
  name: string;
  reasoning: boolean;
  contextWindow: number;
}): WireModel {
  return {
    id: model.id,
    provider: model.provider,
    name: model.name,
    reasoning: model.reasoning,
    contextWindow: model.contextWindow,
  };
}

function messageFor(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function branchFor(cwd: string): string {
  try {
    return execFileSync("git", ["branch", "--show-current"], {
      cwd,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim() || "detached";
  } catch {
    return "workspace";
  }
}

export class PiHarness {
  private session?: AgentSession;
  private modelRuntime?: ModelRuntime;
  private models: WireModel[] = [];
  private mode: HarnessMode;
  private unsubscribeSession?: () => void;
  private initError?: string;
  private readonly listeners = new Set<(event: HarnessEvent) => void>();
  private readonly approvals: ApprovalBroker;

  constructor(
    private readonly cwd: string,
    mode: HarnessMode = "review",
  ) {
    this.mode = mode;
    this.approvals = new ApprovalBroker((request) => this.emit({ type: "approval", request }));
  }

  async initialize(continueRecent = true): Promise<void> {
    this.unsubscribeSession?.();
    this.session?.dispose();
    this.session = undefined;
    this.initError = undefined;

    try {
      const agentDir = getAgentDir();
      const settingsManager = SettingsManager.create(this.cwd, agentDir, { projectTrusted: true });
      settingsManager.applyOverrides({
        compaction: { enabled: true, reserveTokens: 16_384, keepRecentTokens: 20_000 },
        retry: { enabled: true, maxRetries: 3, baseDelayMs: 1_000 },
        steeringMode: "one-at-a-time",
        followUpMode: "one-at-a-time",
      });

      this.modelRuntime = await ModelRuntime.create({
        allowModelNetwork: false,
        modelRefreshTimeoutMs: 8_000,
      });
      this.models = (await this.modelRuntime.getAvailable()).map(asWireModel);

      const guardrails: InlineExtension = {
        name: "azper-guardrails",
        factory: (pi) => {
          pi.on("tool_call", async (event) => {
            const decision = evaluateToolCall(this.mode, this.cwd, event.toolName, event.input);
            if (decision.action === "allow") return undefined;
            if (decision.action === "block") {
              return { block: true, reason: decision.reason, terminate: this.mode === "plan" };
            }

            const allowed = await this.approvals.request({
              toolName: event.toolName,
              title: decision.title,
              command: decision.command,
              risk: decision.risk,
            });
            return allowed ? undefined : { block: true, reason: "Blocked by the user in Azper." };
          });
        },
      };

      const resourceLoader = new DefaultResourceLoader({
        cwd: this.cwd,
        agentDir,
        settingsManager,
        extensionFactories: [guardrails],
        appendSystemPromptOverride: (base) => [...base, AZPER_SYSTEM_GUIDANCE],
      });
      await resourceLoader.reload({ resolveProjectTrust: async () => true });

      const result = await createAgentSession({
        cwd: this.cwd,
        agentDir,
        modelRuntime: this.modelRuntime,
        resourceLoader,
        settingsManager,
        sessionManager: continueRecent ? SessionManager.continueRecent(this.cwd) : SessionManager.create(this.cwd),
        tools: FULL_TOOLS,
      });

      this.session = result.session;
      this.applyModeTools();
      this.unsubscribeSession = this.session.subscribe(() => this.publishSnapshot());
      if (result.modelFallbackMessage) this.notice(result.modelFallbackMessage);
    } catch (error) {
      this.initError = messageFor(error);
    }

    this.publishSnapshot();
  }

  subscribe(listener: (event: HarnessEvent) => void): () => void {
    this.listeners.add(listener);
    listener({ type: "snapshot", snapshot: this.snapshot() });
    return () => this.listeners.delete(listener);
  }

  async prompt(text: string): Promise<void> {
    if (!text.trim()) return;
    if (!this.session) throw new Error(this.initError ?? "Pi session is not available.");
    try {
      const behavior = this.session.isStreaming ? { streamingBehavior: "followUp" as const } : undefined;
      await this.session.prompt(text.trim(), behavior);
    } catch (error) {
      this.fail(error);
    }
  }

  async abort(): Promise<void> {
    await this.session?.abort();
  }

  async compact(): Promise<void> {
    if (!this.session) throw new Error("Pi session is not available.");
    try {
      await this.session.compact("Preserve decisions, modified files, verification evidence, and remaining work.");
      this.notice("Context compacted.", "success");
    } catch (error) {
      this.fail(error);
    }
  }

  async newSession(): Promise<void> {
    this.approvals.denyAll();
    await this.initialize(false);
    this.notice("Started a clean session.", "success");
  }

  setMode(mode: HarnessMode): void {
    this.mode = mode;
    this.applyModeTools();
    this.notice(`Mode: ${mode}.`);
    this.publishSnapshot();
  }

  cycleMode(): HarnessMode {
    const modes: HarnessMode[] = ["review", "plan", "auto", "full"];
    const current = modes.indexOf(this.mode);
    const next = modes[(current + 1) % modes.length] ?? "review";
    this.setMode(next);
    return next;
  }

  async selectModel(selector: string): Promise<void> {
    if (!this.session || !this.modelRuntime) throw new Error("Pi session is not available.");
    if (this.models.length === 0) throw new Error("No authenticated Pi models are available. Run `pi` once to configure a provider.");

    let selected: WireModel | undefined;
    if (selector === "next") {
      const current = this.models.findIndex((model) => model.id === this.session?.model?.id && model.provider === this.session.model.provider);
      selected = this.models[(current + 1) % this.models.length];
    } else {
      selected = this.models.find((model) => `${model.provider}/${model.id}` === selector || model.id === selector);
    }
    if (!selected) throw new Error(`Model not found: ${selector}`);

    const model = this.modelRuntime.getModel(selected.provider, selected.id);
    if (!model) throw new Error(`Model not found: ${selector}`);
    await this.session.setModel(model);
    this.notice(`Model: ${selected.name}.`, "success");
    this.publishSnapshot();
  }

  setThinking(level: string): void {
    if (!this.session || !THINKING_LEVELS.includes(level as (typeof THINKING_LEVELS)[number])) {
      throw new Error(`Invalid thinking level: ${level}`);
    }
    this.session.setThinkingLevel(level as ThinkingLevel);
    this.notice(`Thinking: ${level}.`);
    this.publishSnapshot();
  }

  cycleThinking(): string {
    const available = this.session?.getAvailableThinkingLevels() ?? ["off"];
    const current = available.indexOf(this.session?.thinkingLevel ?? "off");
    const next = available[(current + 1) % available.length] ?? "off";
    this.setThinking(next);
    return next;
  }

  resolveApproval(id: string, allow: boolean, alwaysAllow = false): void {
    this.approvals.resolve(id, allow, alwaysAllow);
  }

  async dispose(): Promise<void> {
    this.approvals.denyAll();
    if (this.session?.isStreaming) await this.session.abort();
    this.unsubscribeSession?.();
    this.session?.dispose();
  }

  getSnapshot(): SessionSnapshot {
    return this.snapshot();
  }

  private applyModeTools(): void {
    this.session?.setActiveToolsByName(this.mode === "plan" ? PLAN_TOOLS : FULL_TOOLS);
  }

  private snapshot(): SessionSnapshot {
    const stats = this.session?.getSessionStats();
    const context = this.session?.getContextUsage();
    return {
      sessionId: this.session?.sessionId,
      sessionFile: this.session?.sessionFile,
      connected: Boolean(this.session),
      branch: branchFor(this.cwd),
      cwd: this.cwd,
      model: this.session?.model ? asWireModel(this.session.model) : undefined,
      models: this.models,
      thinkingLevel: this.session?.thinkingLevel ?? "off",
      thinkingLevels: this.session?.getAvailableThinkingLevels() ?? [],
      mode: this.mode,
      streaming: this.session?.isStreaming ?? false,
      messages: this.session?.messages ?? [],
      stats: {
        input: stats?.tokens.input ?? 0,
        output: stats?.tokens.output ?? 0,
        cacheRead: stats?.tokens.cacheRead ?? 0,
        cacheWrite: stats?.tokens.cacheWrite ?? 0,
        cost: stats?.cost ?? 0,
        contextTokens: context?.tokens ?? 0,
        contextWindow: context?.contextWindow ?? this.session?.model?.contextWindow ?? 0,
        toolCalls: stats?.toolCalls ?? 0,
      },
      error: this.initError,
    };
  }

  private publishSnapshot(): void {
    this.emit({ type: "snapshot", snapshot: this.snapshot() });
  }

  private notice(message: string, level: "info" | "error" | "success" = "info"): void {
    this.emit({ type: "notice", level, message });
  }

  private fail(error: unknown): void {
    this.notice(messageFor(error), "error");
    this.publishSnapshot();
  }

  private emit(event: HarnessEvent): void {
    for (const listener of this.listeners) listener(event);
  }
}
