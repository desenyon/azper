import { describe, expect, it, vi } from "vitest";
import { ApprovalBroker } from "./approval-broker.js";

describe("ApprovalBroker", () => {
  it("resolves a pending approval", async () => {
    const publish = vi.fn();
    const broker = new ApprovalBroker(publish);
    const pending = broker.request({ toolName: "edit", title: "Edit file", command: "src/app.ts", risk: "mutation" });
    const request = publish.mock.calls[0]?.[0];

    expect(broker.resolve(request.id, true)).toBe(true);
    await expect(pending).resolves.toBe(true);
  });

  it("remembers an always-allowed tool for the session", async () => {
    const publish = vi.fn();
    const broker = new ApprovalBroker(publish);
    const first = broker.request({ toolName: "bash", title: "Run", command: "npm test", risk: "mutation" });
    broker.resolve(publish.mock.calls[0]?.[0].id, true, true);
    await first;

    await expect(broker.request({ toolName: "bash", title: "Run", command: "npm run lint", risk: "mutation" })).resolves.toBe(true);
    expect(publish).toHaveBeenCalledTimes(1);
  });
});
