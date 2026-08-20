import { createHmac } from "node:crypto";

const secret = (await readStdin()).trim().replace(/\s+/g, "").toUpperCase();
if (!/^[A-Z2-7]{16,128}$/.test(secret)) {
  throw new Error("stdin must contain one RFC 4648 base32 TOTP secret");
}

const key = decodeBase32(secret);
const unixTime = process.env.ARGUS_TOTP_UNIX_TIME
  ? Number.parseInt(process.env.ARGUS_TOTP_UNIX_TIME, 10)
  : Math.floor(Date.now() / 1000);
if (!Number.isSafeInteger(unixTime) || unixTime < 0) {
  throw new Error("ARGUS_TOTP_UNIX_TIME must be a non-negative integer");
}
const counter = Math.floor(unixTime / 30);
const message = Buffer.alloc(8);
message.writeBigUInt64BE(BigInt(counter));
const digest = createHmac("sha1", key).update(message).digest();
const offset = digest[digest.length - 1] & 0x0f;
const binary =
  ((digest[offset] & 0x7f) << 24) |
  ((digest[offset + 1] & 0xff) << 16) |
  ((digest[offset + 2] & 0xff) << 8) |
  (digest[offset + 3] & 0xff);
process.stdout.write(String(binary % 1_000_000).padStart(6, "0"));

function decodeBase32(value) {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let bits = "";
  for (const character of value) {
    const index = alphabet.indexOf(character);
    if (index < 0) throw new Error("invalid base32 character");
    bits += index.toString(2).padStart(5, "0");
  }
  const bytes = [];
  for (let offset = 0; offset + 8 <= bits.length; offset += 8) {
    bytes.push(Number.parseInt(bits.slice(offset, offset + 8), 2));
  }
  return Buffer.from(bytes);
}

async function readStdin() {
  const chunks = [];
  for await (const chunk of process.stdin) chunks.push(chunk);
  return Buffer.concat(chunks).toString("utf8");
}
