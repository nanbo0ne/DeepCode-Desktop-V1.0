import { readCachedUpdate, UPDATE_CHECK_INTERVAL_MS, writeCachedUpdate } from "../lib/updateCheck";
import type { UpdateInfo } from "../lib/types";

class MemoryStorage {
  value = new Map<string, string>();
  getItem(key: string) { return this.value.get(key) ?? null; }
  setItem(key: string, value: string) { this.value.set(key, value); }
}

const info: UpdateInfo = {
  available: true,
  current: "2.0.25",
  latest: "2.0.26",
  notes: "",
  canDownload: true,
  canSelfUpdate: false,
  downloadUrl: "https://github.com/nanbo0ne/O.R.C.A/releases/tag/desktop-v2.0.26",
  assetSize: 0,
};

let passed = 0;
let failed = 0;
function check(value: boolean, label: string) {
  if (value) { process.stdout.write(`  PASS  ${label}\n`); passed += 1; }
  else { process.stdout.write(`  FAIL  ${label}\n`); failed += 1; }
}

console.log("\nupdate check cache");
const storage = new MemoryStorage();
writeCachedUpdate("2.0.25", info, 1_000, storage);
check(readCachedUpdate("2.0.25", 1_000 + UPDATE_CHECK_INTERVAL_MS - 1, storage)?.latest === "2.0.26", "fresh cache is reused");
check(readCachedUpdate("2.0.25", 1_000 + UPDATE_CHECK_INTERVAL_MS, storage) === null, "24-hour cache expires");
check(readCachedUpdate("2.0.26", 2_000, storage) === null, "cache from another running version is ignored");

storage.setItem("orca.update-check.v1", JSON.stringify({ checkedAt: 1_000, currentVersion: "2.0.25", info: { ...info, canDownload: undefined } }));
check(readCachedUpdate("2.0.25", 2_000, storage)?.canDownload === false, "legacy cached updates without canDownload are treated as webpage-only");

const failedInfo = { ...info, err: "offline" };
const failedStorage = new MemoryStorage();
writeCachedUpdate("2.0.25", failedInfo, 1_000, failedStorage);
check(readCachedUpdate("2.0.25", 2_000, failedStorage) === null, "failed checks are not cached");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
