import { rmSync } from "node:fs";
import { execFileSync } from "node:child_process";

rmSync(new URL("../dist", import.meta.url), { recursive: true, force: true });
execFileSync(process.execPath, [new URL("../node_modules/typescript/bin/tsc", import.meta.url).pathname, "-p", "tsconfig.json"], {
  cwd: new URL("..", import.meta.url),
  stdio: "inherit",
});
