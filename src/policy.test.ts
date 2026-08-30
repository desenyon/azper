import { describe, expect, it } from "vitest";
import { evaluateToolCall } from "./policy.js";

const workspace = "/workspace/azper";

describe("evaluateToolCall", () => {
  it("keeps plan mode read-only", () => {
    expect(evaluateToolCall("plan", workspace, "edit", { path: "src/app.ts" }).action).toBe("block");
    expect(evaluateToolCall("plan", workspace, "read", { path: "src/app.ts" }).action).toBe("allow");
  });

  it("asks before mutations in review mode", () => {
    expect(evaluateToolCall("review", workspace, "edit", { path: "src/app.ts" }).action).toBe("approve");
    expect(evaluateToolCall("review", workspace, "bash", { command: "npm install left-pad" }).action).toBe("approve");
  });

  it("allows strict read-only shell commands in review mode", () => {
    expect(evaluateToolCall("review", workspace, "bash", { command: "git diff --stat" }).action).toBe("allow");
    expect(evaluateToolCall("review", workspace, "bash", { command: "git status; touch nope" }).action).toBe("approve");
  });

  it("still asks before high-impact commands in auto mode", () => {
    expect(evaluateToolCall("auto", workspace, "bash", { command: "git push origin main" })).toMatchObject({
      action: "approve",
      risk: "dangerous",
    });
  });

  it("blocks protected and out-of-workspace writes", () => {
    expect(evaluateToolCall("full", workspace, "write", { path: "../outside" }).action).toBe("block");
    expect(evaluateToolCall("auto", workspace, "edit", { file_path: ".git/config" }).action).toBe("block");
  });
});
