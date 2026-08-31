import { chmodSync, rmSync } from "node:fs";
import { execFileSync } from "node:child_process";

const outputDirectory = new URL("../dist/", import.meta.url);
const cli = new URL("./cli.js", outputDirectory);

rmSync(outputDirectory, { recursive: true, force: true });
execFileSync(process.execPath, [new URL("../node_modules/typescript/bin/tsc", import.meta.url).pathname, "-p", "tsconfig.json"], {
  cwd: new URL("..", import.meta.url),
  stdio: "inherit",
});
chmodSync(cli, 0o755);
