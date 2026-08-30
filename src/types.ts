export type HarnessMode = "review" | "plan" | "auto" | "full";

export type WireModel = {
  id: string;
  provider: string;
  name: string;
  reasoning: boolean;
  contextWindow: number;
};

export type ApprovalRequest = {
  id: string;
  toolName: string;
  title: string;
  command: string;
  risk: "mutation" | "dangerous";
};

export type HarnessStats = {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  cost: number;
  contextTokens: number;
  contextWindow: number;
  toolCalls: number;
};

export type SessionSnapshot = {
  sessionId?: string;
  sessionFile?: string;
  connected: boolean;
  branch: string;
  cwd: string;
  model?: WireModel;
  models: WireModel[];
  thinkingLevel: string;
  thinkingLevels: string[];
  mode: HarnessMode;
  streaming: boolean;
  messages: unknown[];
  stats: HarnessStats;
  error?: string;
};

export type HarnessEvent =
  | { type: "snapshot"; snapshot: SessionSnapshot }
  | { type: "approval"; request: ApprovalRequest }
  | { type: "notice"; level: "info" | "error" | "success"; message: string };
