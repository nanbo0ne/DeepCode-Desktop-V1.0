// Run: tsx src/__tests__/onboarding-route.test.ts

import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { DEFAULT_ONBOARDING_ROUTE } from "../lib/onboarding";

let failed = 0;
function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}\n`);
    failed += 1;
  }
}

console.log("\nonboarding route");
eq(DEFAULT_ONBOARDING_ROUTE, "deepseek", "first launch opens the DeepSeek key route");
const root = fileURLToPath(new URL("..", import.meta.url));
const overlay = readFileSync(join(root, "components", "OnboardingOverlay.tsx"), "utf8");
const app = readFileSync(join(root, "App.tsx"), "utf8");
eq(overlay.includes('setError(t("onboarding.error.unknown", { msg: String(e) }))'), true, "state load failures surface inside onboarding");
eq(overlay.includes('role="dialog" aria-modal="true"'), true, "onboarding exposes modal dialog semantics");
eq(overlay.includes('const changeRoute = (next: OnboardingRoute) => { setError(null); setRoute(next); };'), true, "route changes clear stale errors");
eq(overlay.includes('onKeyDown={keepFocusInDialog}'), true, "keyboard focus remains inside onboarding");
eq(app.includes('needsOnboarding && !startupSplashVisible && <OnboardingOverlay'), true, "startup splash and onboarding never mount together");
if (failed > 0) process.exit(1);
