import { describe, expect, it } from "vitest";
import { parseArgs } from "./cli.js";

describe("parseArgs", () => {
  it("uses safe defaults", () => {
    expect(parseArgs([], "/workspace")).toMatchObject({ cwd: "/workspace", mode: "review", continueRecent: true });
  });

  it("accepts a clean session, cwd, and mode", () => {
    expect(parseArgs(["--new", "--cwd", "repo", "--mode", "plan"], "/workspace")).toMatchObject({
      cwd: "/workspace/repo",
      mode: "plan",
      continueRecent: false,
    });
  });

  it("rejects invalid modes", () => {
    expect(() => parseArgs(["--mode", "unsafe"])).toThrow("--mode must be");
  });
});
