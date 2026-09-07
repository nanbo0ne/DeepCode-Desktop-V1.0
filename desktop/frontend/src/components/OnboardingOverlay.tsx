import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { ArrowRight, Check, Download, KeyRound, Monitor, SkipForward } from "lucide-react";
import logoSymbol from "../assets/logo-symbol.png";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { normalizeLocalAICatalog } from "../lib/localAI";
import { DEFAULT_ONBOARDING_ROUTE, type OnboardingRoute } from "../lib/onboarding";
import type { LocalAICatalogView, OnboardingState } from "../lib/types";

export function OnboardingOverlay({ onComplete }: { onComplete: () => void }) {
  const t = useT();
  const [state, setState] = useState<OnboardingState | null>(null);
  const [route, setRoute] = useState<OnboardingRoute>(DEFAULT_ONBOARDING_ROUTE);
  const [providerID, setProviderID] = useState("deepseek");
  const [key, setKey] = useState("");
  const [modelID, setModelID] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [localCatalog, setLocalCatalog] = useState<LocalAICatalogView | null>(null);
  const dialogRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    void app.GetOnboardingState()
      .then((next) => {
        setState(next as OnboardingState);
        const deepseek = next.providers.find((p) => p.name === "deepseek");
        setProviderID(deepseek?.name || next.providers.find((p) => p.added)?.name || next.providers[0]?.name || "deepseek");
      })
      .catch((e) => setError(t("onboarding.error.unknown", { msg: String(e) })));
  }, []);
  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      dialogRef.current?.querySelector<HTMLElement>("input, select, button:not([disabled])")?.focus();
    });
    return () => cancelAnimationFrame(frame);
  }, [route]);
  useEffect(() => {
    if (route !== "local") return;
    setError(null);
    setLocalCatalog(null);
    void app.GetLocalAICatalog()
      .then((next) => setLocalCatalog(normalizeLocalAICatalog(next)))
      .catch((e) => setError(t("onboarding.error.unknown", { msg: String(e) })));
  }, [route]);

  const selected = useMemo(() => state?.providers.find((p) => p.name === providerID), [providerID, state]);
  const changeRoute = (next: OnboardingRoute) => { setError(null); setRoute(next); };
  const keepFocusInDialog = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== "Tab") return;
    const focusable = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>(
      'button:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])',
    ) || []).filter((element) => element.offsetParent !== null);
    if (focusable.length === 0) { event.preventDefault(); dialogRef.current?.focus(); return; }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
  };
  const complete = async () => { setBusy(true); setError(null); try { await app.CompleteOnboarding(); onComplete(); } catch (e) { setError(String(e)); } finally { setBusy(false); } };
  const connect = async () => {
    if (!providerID || !key.trim()) { setError(t("onboarding.error.emptyProvider")); return; }
    setBusy(true); setError(null);
    try { await app.ConnectProviderPreset(providerID, key.trim()); await complete(); } catch (e) { setError(String(e)); setBusy(false); }
  };
  const installLocal = async () => {
    if (!localCatalog?.supported) { setError(t("onboarding.error.localUnsupported")); return; }
    setBusy(true); setError(null);
    try { await app.StartLocalRuntimeInstall(localCatalog.hardware.recommendedRuntime); const model = modelID || localCatalog.hardware.recommendedModel || localCatalog.models[0]?.id; if (model) await app.StartLocalModelDownload(model); await complete(); } catch (e) { setError(String(e)); setBusy(false); }
  };

  return <div className="onboarding">
    <div ref={dialogRef} className="onboarding__card" role="dialog" aria-modal="true" aria-labelledby="onboarding-title" aria-describedby="onboarding-description" tabIndex={-1} onKeyDown={keepFocusInDialog}>
      <img src={logoSymbol} className="onboarding__logo" alt="O.R.C.A." draggable={false} />
      <div id="onboarding-title" className="onboarding__title">{t("onboarding.title")}</div>
      <div id="onboarding-description" className="onboarding__tag">{t("onboarding.tagline")}</div>
      {route === "deepseek" && <>
        <div className="onboarding__privacy"><span>{t("onboarding.deepseekPrivacy")}</span><span>{t("onboarding.deepseekPrivacyLocal")}</span></div>
        <div className="onboarding__provider-heading"><KeyRound size={18} /><span><strong>{t("onboarding.officialApi")}</strong><small>https://api.deepseek.com</small></span></div>
        <label className="onboarding__label" htmlFor="onboarding-deepseek-key">DEEPSEEK_API_KEY</label>
        <input id="onboarding-deepseek-key" className="onboarding__input" type="password" autoComplete="off" value={key} onChange={(e) => setKey(e.target.value)} placeholder="sk-..." />
        {error && <div className="onboarding__error" role="alert">{error}</div>}
        <button className="onboarding__submit" disabled={busy || !key.trim()} onClick={() => { setProviderID("deepseek"); void connect(); }}>{busy ? t("onboarding.validating") : t("onboarding.submit")}</button>
        <div className="onboarding__choices onboarding__choices--secondary">
          <button className="onboarding__choice" onClick={() => changeRoute("providers")}><KeyRound size={18} /><span><strong>{t("onboarding.otherProviders")}</strong><small>{t("onboarding.otherProvidersHint")}</small></span><ArrowRight size={16} /></button>
          <button className="onboarding__choice" onClick={() => changeRoute("local")}><Monitor size={18} /><span><strong>{t("onboarding.localAI")}</strong><small>{t("onboarding.localAIHint")}</small></span><ArrowRight size={16} /></button>
        </div>
        <button className="onboarding__skip" disabled={busy} onClick={() => void complete()}><SkipForward size={14} />{t("onboarding.skipToApp")}</button>
      </>}
      {route === "providers" && <>
        <label className="onboarding__label" htmlFor="onboarding-provider">{t("onboarding.provider")}</label>
        <select id="onboarding-provider" className="onboarding__input" value={providerID} onChange={(e) => setProviderID(e.target.value)}>{(state?.providers || []).map((p) => <option key={p.name} value={p.name}>{p.name} · {p.baseUrl}</option>)}</select>
        <label className="onboarding__label" htmlFor="onboarding-key">{selected?.apiKeyEnv || t("onboarding.apiKey")}</label>
        <input id="onboarding-key" className="onboarding__input" type="password" autoComplete="off" value={key} onChange={(e) => setKey(e.target.value)} placeholder={t("onboarding.apiKeyLocalPlaceholder")} />
        {error && <div className="onboarding__error" role="alert">{error}</div>}
        <button className="onboarding__submit" disabled={busy} onClick={() => void connect()}>{busy ? t("onboarding.validating") : t("onboarding.submit")}</button>
        <button className="onboarding__skip" disabled={busy} onClick={() => { setProviderID("deepseek"); changeRoute("deepseek"); }}>{t("onboarding.backToDeepSeek")}</button>
      </>}
      {route === "local" && <>
        <div className="onboarding__privacy">{t("onboarding.localHint")}</div>
        {!localCatalog && !error && <div className="onboarding__privacy">{t("onboarding.localLoading")}</div>}
        {localCatalog && <select className="onboarding__input" value={modelID || localCatalog.hardware.recommendedModel || ""} onChange={(e) => setModelID(e.target.value)}>{localCatalog.models.map((model) => <option key={model.id} value={model.id}>{model.name}</option>)}</select>}
        {error && <div className="onboarding__error" role="alert">{error}</div>}
        <button className="onboarding__submit" disabled={busy || !localCatalog?.supported} onClick={() => void installLocal()}>{busy ? t("onboarding.localLoading") : <><Download size={14} />{t("onboarding.installContinue")}</>}</button>
        <button className="onboarding__skip" disabled={busy} onClick={() => changeRoute("deepseek")}>{t("onboarding.backToDeepSeek")}</button>
      </>}
      <div className="onboarding__links"><span>{t("onboarding.upgradeNote")}</span><span><Check size={13} />{t("onboarding.changeSettings")}</span></div>
    </div>
  </div>;
}
