import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";
import { createServer } from "vite";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const [app, portValue, apiMode = "mock"] = process.argv.slice(2);
const apps = {
  enterprise: "web/apps/enterprise",
  platform: "web/apps/platform",
  card: "web/apps/card-runtime",
};

if (!apps[app] || !/^\d+$/.test(portValue ?? "")) {
  console.error("usage: node scripts/run-vite.mjs enterprise|platform|card PORT [mock|real]");
  process.exit(2);
}
if (app !== "card") {
  process.env.VITE_API_MODE = apiMode;
}

const server = await createServer({
  root: path.join(root, apps[app]),
  server: { host: "0.0.0.0", port: Number(portValue), strictPort: true },
});
await server.listen();

const stop = async () => {
  await server.close();
  process.exit(0);
};
process.once("SIGINT", stop);
process.once("SIGTERM", stop);
