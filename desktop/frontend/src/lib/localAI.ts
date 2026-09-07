import type {
  HardwareGPU,
  HardwareProfile,
  LocalAICatalogView,
  LocalDownloadTask,
  LocalModelInstallation,
  LocalModelSpec,
  LocalRuntimeSpec,
  LocalRuntimeStatus,
} from "./types";

type UnknownRecord = Record<string, unknown>;

function record(value: unknown): UnknownRecord {
  return value !== null && typeof value === "object" ? value as UnknownRecord : {};
}

function array<T>(value: unknown): T[] {
  return Array.isArray(value) ? value as T[] : [];
}

function text(value: unknown, fallback = ""): string {
  return typeof value === "string" ? value : fallback;
}

function number(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function bool(value: unknown): boolean {
  return value === true;
}

function normalizeHardware(value: unknown, platform: string, supported: boolean): HardwareProfile {
  const source = record(value);
  return {
    platform: text(source.platform, platform),
    supported: source.supported === undefined ? supported : bool(source.supported),
    gpus: array<unknown>(source.gpus).map((item) => {
      const gpu = record(item);
      return {
        name: text(gpu.name, "未知显卡"),
        vendor: text(gpu.vendor, "Unknown"),
        dedicatedMiB: number(gpu.dedicatedMiB),
        availableMiB: number(gpu.availableMiB),
        backend: text(gpu.backend, "unknown"),
      } satisfies HardwareGPU;
    }),
    gpuDetectionFailed: bool(source.gpuDetectionFailed),
    memoryTotalMiB: number(source.memoryTotalMiB),
    memoryFreeMiB: number(source.memoryFreeMiB),
    cpuLogicalCores: number(source.cpuLogicalCores),
    diskFreeBytes: number(source.diskFreeBytes),
    recommendedRuntime: text(source.recommendedRuntime),
    recommendedModel: text(source.recommendedModel),
    localAIRecommended: bool(source.localAIRecommended),
  };
}

export function normalizeLocalAICatalog(value: unknown): LocalAICatalogView {
  const source = record(value);
  const platform = text(source.platform, "unknown");
  const supported = bool(source.supported);
  return {
    supported,
    platform,
    models: array<LocalModelSpec>(source.models),
    runtimes: array<LocalRuntimeSpec>(source.runtimes),
    installedModels: array<LocalModelInstallation>(source.installedModels),
    runtime: source.runtime && typeof source.runtime === "object" ? source.runtime as LocalAICatalogView["runtime"] : undefined,
    downloads: array<LocalDownloadTask>(source.downloads),
    status: {
      state: text(record(source.status).state, "unavailable"),
      modelId: text(record(source.status).modelId) || undefined,
      baseUrl: text(record(source.status).baseUrl) || undefined,
      profile: record(source.status).profile && typeof record(source.status).profile === "object" ? record(source.status).profile as LocalRuntimeStatus["profile"] : undefined,
      lastError: text(record(source.status).lastError) || undefined,
    },
    hardware: normalizeHardware(source.hardware, platform, supported),
    modelsDirectory: text(source.modelsDirectory),
  };
}
