import { randomUUID } from "node:crypto";
import type { ApprovalRequest } from "./types.js";

type PendingApproval = {
  request: ApprovalRequest;
  resolve: (allowed: boolean) => void;
  timer: ReturnType<typeof setTimeout>;
};

export class ApprovalBroker {
  private readonly pending = new Map<string, PendingApproval>();
  private readonly alwaysAllowed = new Set<string>();

  constructor(private readonly publish: (request: ApprovalRequest) => void) {}

  async request(input: Omit<ApprovalRequest, "id">): Promise<boolean> {
    if (this.alwaysAllowed.has(input.toolName)) return true;

    const request = { ...input, id: randomUUID() };
    this.publish(request);

    return new Promise<boolean>((resolve) => {
      const timer = setTimeout(() => this.resolve(request.id, false), 120_000);
      this.pending.set(request.id, { request, resolve, timer });
    });
  }

  resolve(id: string, allowed: boolean, alwaysAllow = false): boolean {
    const pending = this.pending.get(id);
    if (!pending) return false;

    clearTimeout(pending.timer);
    this.pending.delete(id);
    if (allowed && alwaysAllow) this.alwaysAllowed.add(pending.request.toolName);
    pending.resolve(allowed);
    return true;
  }

  denyAll(): void {
    for (const id of [...this.pending.keys()]) this.resolve(id, false);
  }
}
