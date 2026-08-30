#!/usr/bin/env node
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import type { HarnessMode } from "./types.js";
import { TerminalApp } from "./ui/app.js";

type CliOptions = {
  cwd: string;
  mode: HarnessMode;
  continueRecent: boolean;
  help: boolean;
};

function usage(): string {
  return `
Azper — a terminal-native Pi coding harness

Usage: azper [options]

Options:
  --cwd <path>                  Workspace to open (default: current directory)
  --mode <review|plan|auto|full>
                                Initial tool-permission mode (default: review)
  --new                         Start a clean Pi session
  -h, --help                    Show this help

Inside Azper, use /help for commands. Press Ctrl+Q to exit.
`.trim();
}

export function parseArgs(args: string[], initialCwd = process.cwd()): CliOptions {
  const options: CliOptions = { cwd: initialCwd, mode: "review", continueRecent: true, help: false };
  for (let index = 0; index < args.length; index += 1) {
    const value = args[index];
    if (value === "-h" || value === "--help") options.help = true;
    else if (value === "--new") options.continueRecent = false;
    else if (value === "--cwd") {
      const cwd = args[index + 1];
      if (!cwd) throw new Error("--cwd requires a path");
      options.cwd = path.resolve(initialCwd, cwd);
      index += 1;
    } else if (value === "--mode") {
      const mode = args[index + 1];
      if (!mode || !["review", "plan", "auto", "full"].includes(mode)) {
        throw new Error("--mode must be review, plan, auto, or full");
      }
      options.mode = mode as HarnessMode;
      index += 1;
    } else {
      throw new Error(`Unknown option: ${value}`);
    }
  }
  return options;
}

export async function main(args = process.argv.slice(2)): Promise<void> {
  const options = parseArgs(args);
  if (options.help) {
    console.log(usage());
    return;
  }
  if (!process.stdin.isTTY || !process.stdout.isTTY) throw new Error("Azper requires an interactive terminal.");
  if (!fs.existsSync(options.cwd) || !fs.statSync(options.cwd).isDirectory()) throw new Error(`Workspace does not exist: ${options.cwd}`);

  const app = new TerminalApp(options);
  await app.run();
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error: unknown) => {
    console.error(`azper: ${error instanceof Error ? error.message : String(error)}`);
    process.exitCode = 1;
  });
}
