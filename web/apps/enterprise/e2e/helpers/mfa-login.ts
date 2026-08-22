import { createHmac } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { expect, type Page } from "@playwright/test";

type Audience = "enterprise" | "platform";

export function createMfaLogin(audience: Audience) {
  const prefix = `ARGUS_E2E_${audience.toUpperCase()}_TOTP`;
  const secret = process.env[`${prefix}_SECRET`] ?? "";
  let lastCode = process.env[`${prefix}_LAST_CODE`] ?? "";
  const artifactDir = process.env.ARGUS_E2E_ARTIFACTS ?? "";
  const stateFile = artifactDir
    ? join(artifactDir, `.argus-${audience}-totp-last-code`)
    : "";

  return async (page: Page, url: string, username: string, password: string) => {
    expect(username).not.toBe("");
    expect(password).not.toBe("");
    await page.goto(url);
    await page.locator('input[autocomplete="username"]').fill(username);
    await page.locator('input[autocomplete="current-password"]').fill(password);
    await page.locator('form button[type="submit"]').click();

    const mfaInput = page.locator('input[autocomplete="one-time-code"]');
    await expect
      .poll(async () => (await mfaInput.isVisible()) || !/\/login/.test(page.url()))
      .toBe(true);
    if (await mfaInput.isVisible()) {
      expect(secret, `${audience} TOTP secret is required`).not.toBe("");
      const code = await nextCode(secret, readLastCode(stateFile) || lastCode);
      lastCode = code;
      writeLastCode(stateFile, code);
      await mfaInput.fill(code);
      await page.locator('form button[type="submit"]').click();
    }
    await expect(page).not.toHaveURL(/\/login/);
  };
}

async function nextCode(secret: string, previous: string) {
  const deadline = Date.now() + 40_000;
  while (Date.now() < deadline) {
    const now = Date.now();
    const code = totp(secret, now);
    const secondsRemaining = 30 - (Math.floor(now / 1000) % 30);
    if (code !== previous && secondsRemaining >= 5) return code;
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error("TOTP counter did not advance before the login timeout");
}

function readLastCode(path: string) {
  if (!path) return "";
  try {
    return readFileSync(path, "utf8").trim();
  } catch {
    return "";
  }
}

function writeLastCode(path: string, code: string) {
  if (!path) return;
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, code, { encoding: "utf8", mode: 0o600 });
}

function totp(secret: string, now: number) {
  const key = decodeBase32(secret.trim().toUpperCase());
  const counter = BigInt(Math.floor(now / 1000 / 30));
  const message = Buffer.alloc(8);
  message.writeBigUInt64BE(counter);
  const digest = createHmac("sha1", key).update(message).digest();
  const offset = digest[digest.length - 1] & 0x0f;
  const binary =
    ((digest[offset] & 0x7f) << 24) |
    ((digest[offset + 1] & 0xff) << 16) |
    ((digest[offset + 2] & 0xff) << 8) |
    (digest[offset + 3] & 0xff);
  return String(binary % 1_000_000).padStart(6, "0");
}

function decodeBase32(value: string) {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let bits = "";
  for (const character of value) {
    const index = alphabet.indexOf(character);
    if (index < 0) throw new Error("invalid base32 TOTP secret");
    bits += index.toString(2).padStart(5, "0");
  }
  const bytes: number[] = [];
  for (let offset = 0; offset + 8 <= bits.length; offset += 8) {
    bytes.push(Number.parseInt(bits.slice(offset, offset + 8), 2));
  }
  return Buffer.from(bytes);
}
