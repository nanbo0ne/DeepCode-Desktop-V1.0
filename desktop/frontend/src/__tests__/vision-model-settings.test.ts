import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const panel = readFileSync(`${root}/components/SettingsPanel.tsx`, "utf8");
const bridge = readFileSync(`${root}/lib/bridge.ts`, "utf8");
const types = readFileSync(`${root}/lib/types.ts`, "utf8");
const en = readFileSync(`${root}/locales/en.ts`, "utf8");
const zh = readFileSync(`${root}/locales/zh.ts`, "utf8");

let passed = 0;
let failed = 0;
function check(value: boolean, label: string) {
  if (value) { process.stdout.write(`  PASS  ${label}\n`); passed += 1; }
  else { process.stdout.write(`  FAIL  ${label}\n`); failed += 1; }
}

console.log("\nvision model settings contract");
check(types.includes("visionModel?: string") && types.includes("effectiveVisionModel?: string"), "settings view keeps raw and effective vision model fields optional");
check(bridge.includes("SetVisionModel(ref: string): Promise<void>") && bridge.includes("async SetVisionModel(ref: string)"), "bridge exposes a vision-only model setter and mock");
check(panel.includes("<ModelPicker") && panel.includes('value={s.visionModel ?? ""}') && panel.includes("app.SetVisionModel(ref)"), "settings uses the existing model picker for the vision role");
check(panel.includes('emptyOptionLabel={t("settings.visionModelAuto")}') && panel.includes("s.effectiveVisionModel"), "empty vision selection shows the automatic effective model");
check(panel.includes("app.SetSubagentModel(ref)") && panel.includes("app.SetSubagentEffort(e.target.value)"), "general subagent controls remain separate");
check(["settings.visionModel", "settings.visionModelAuto", "settings.visionModelHint"].every((key) => en.includes(`"${key}"`) && zh.includes(`"${key}"`)), "vision model labels are localized");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
