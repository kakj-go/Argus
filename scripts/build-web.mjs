import { spawn } from "node:child_process";

const mode = process.argv[2];
if (mode !== "mock" && mode !== "real") {
  console.error("usage: node scripts/build-web.mjs mock|real");
  process.exit(2);
}

const env = { ...process.env, VITE_API_MODE: mode };
if (mode === "real") {
  env.VITE_API_BASE_URL ??= "https://api.argus.invalid";
  env.VITE_CARD_ORIGIN ??= "https://cards.argus.invalid";
  env.VITE_PLATFORM_URL ??= "https://platform.argus.invalid";
}

const executable = process.platform === "win32" ? "pnpm.cmd" : "pnpm";
const child = spawn(executable, ["-r", "--if-present", "build"], {
  cwd: process.cwd(),
  env,
  stdio: "inherit",
  windowsHide: true,
});

child.on("error", (error) => {
  console.error(error.message);
  process.exit(1);
});
child.on("exit", (code, signal) => {
  if (signal) {
    console.error(`web build terminated by ${signal}`);
    process.exit(1);
  }
  process.exit(code ?? 1);
});
