// Run: tsx src/__tests__/local-ai-contract.test.ts

import { normalizeLocalAICatalog } from "../lib/localAI";

let passed = 0;
let failed = 0;

function check(condition: boolean, label: string) {
  if (condition) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\nlocal AI bridge contract");

const empty = normalizeLocalAICatalog(null);
check(empty.models.length === 0 && empty.downloads.length === 0, "normalizes a missing catalog");
check(empty.hardware.gpus.length === 0, "normalizes missing hardware and GPUs");
check(empty.status.state === "unavailable", "provides a conservative runtime state");

const nullArrays = normalizeLocalAICatalog({
  supported: true,
  platform: "windows",
  models: null,
  runtimes: null,
  installedModels: null,
  downloads: null,
  hardware: { gpus: null, memoryTotalMiB: 32768 },
  status: null,
});
check(nullArrays.models.length === 0 && nullArrays.installedModels.length === 0, "turns null catalog collections into arrays");
check(nullArrays.hardware.gpus.length === 0, "turns a null GPU collection into an array");
check(nullArrays.hardware.memoryTotalMiB === 32768, "preserves valid hardware values");
check(nullArrays.hardware.gpuDetectionFailed === false, "defaults GPU detection failures to false");

const detectionFailed = normalizeLocalAICatalog({
  supported: true,
  platform: "windows",
  hardware: { gpuDetectionFailed: true, gpus: [] },
});
check(detectionFailed.hardware.gpuDetectionFailed === true, "preserves GPU detection failures");

const mixedGPUs = normalizeLocalAICatalog({
  supported: true,
  platform: "windows",
  hardware: {
    gpus: [
      { name: "Integrated", dedicatedMiB: 1024, backend: "vulkan" },
      { name: "Discrete", dedicatedMiB: 16384, availableMiB: 14000, backend: "cuda" },
    ],
  },
});
check(mixedGPUs.hardware.gpus.length === 2, "preserves every enumerated adapter");
check(mixedGPUs.hardware.gpus[1]?.name === "Discrete", "does not assume the dedicated GPU is GPU0");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
