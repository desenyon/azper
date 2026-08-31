import { mkdtempSync, rmSync, symlinkSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { describe, expect, it } from "vitest";
import { isMainModule, parseArgs } from "./cli.js";

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

  it("recognizes a symlinked global binary as the main module", () => {
    const directory = mkdtempSync(path.join(tmpdir(), "azper-cli-"));
    const target = fileURLToPath(import.meta.url);
    const link = path.join(directory, "azper");
    try {
      symlinkSync(target, link);
      expect(isMainModule(pathToFileURL(target).href, link)).toBe(true);
    } finally {
      rmSync(directory, { recursive: true, force: true });
    }
  });
});
