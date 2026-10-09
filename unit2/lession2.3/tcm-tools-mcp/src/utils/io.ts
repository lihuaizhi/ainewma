import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

export async function loadJson<T>(
  fileName: string,
  dataDir?: string,
  fallback: T = [] as unknown as T
): Promise<T> {
  const candidates: string[] = [];

  if (dataDir) {
    candidates.push(resolve(dataDir, fileName));
    candidates.push(resolve(process.cwd(), dataDir, fileName));
  }

  candidates.push(resolve(process.cwd(), fileName));
  candidates.push(resolve(process.cwd(), "data", fileName));
  candidates.push(resolve(__dirname, "..", "..", "data", fileName));
  candidates.push(resolve(__dirname, "..", "data", fileName));

  const uniqueCandidates = Array.from(new Set(candidates));

  for (const p of uniqueCandidates) {
    try {
      if (existsSync(p)) {
        const raw = readFileSync(p, "utf-8");
        const parsed = JSON.parse(raw) as T;
        return parsed;
      }
    } catch (err) {
      console.error(`[loadJson] Failed to load ${p}:`, err);
    }
  }

  console.warn(
    `[loadJson] Could not find ${fileName} in candidates, returning fallback. Tried:`,
    uniqueCandidates
  );
  return fallback;
}
