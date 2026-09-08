import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { Check, KeyRound, SkipForward } from "lucide-react";
import logoSymbol from "../assets/logo-symbol.png";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { ONBOARDING_PROVIDER_ID } from "../lib/onboarding";

export function OnboardingOverlay({ onComplete }: { onComplete: () => void }) {
  const t = useT();
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const dialogRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    void app.GetOnboardingState().catch((e) => setError(t("onboarding.error.unknown", { msg: String(e) })));
  }, [t]);
  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      dialogRef.current?.querySelector<HTMLElement>("input, button:not([disabled])")?.focus();
    });
    return () => cancelAnimationFrame(frame);
  }, []);

  const keepFocusInDialog = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== "Tab") return;
    const focusable = Array.from(dialogRef.current?.querySelectorAll<HTMLElement>(
      'button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])',
    ) || []).filter((element) => element.offsetParent !== null);
    if (focusable.length === 0) { event.preventDefault(); dialogRef.current?.focus(); return; }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
  };
  const complete = async () => {
    setBusy(true); setError(null);
    try { await app.CompleteOnboarding(); onComplete(); }
    catch (e) { setError(String(e)); }
    finally { setBusy(false); }
  };
  const connect = async () => {
    if (!key.trim()) { setError(t("onboarding.error.emptyProvider")); return; }
    setBusy(true); setError(null);
    try {
      await app.ConnectProviderPreset(ONBOARDING_PROVIDER_ID, key.trim());
      await complete();
    } catch (e) {
      setError(String(e));
      setBusy(false);
    }
  };

  return <div className="onboarding">
    <div ref={dialogRef} className="onboarding__card" role="dialog" aria-modal="true" aria-labelledby="onboarding-title" aria-describedby="onboarding-description" tabIndex={-1} onKeyDown={keepFocusInDialog}>
      <img src={logoSymbol} className="onboarding__logo" alt="O.R.C.A." draggable={false} />
      <div id="onboarding-title" className="onboarding__title">{t("onboarding.title")}</div>
      <div id="onboarding-description" className="onboarding__tag">{t("onboarding.tagline")}</div>
      <div className="onboarding__privacy"><span>{t("onboarding.deepseekPrivacy")}</span><span>{t("onboarding.deepseekPrivacyLocal")}</span></div>
      <div className="onboarding__provider-heading"><KeyRound size={18} /><span><strong>{t("onboarding.officialApi")}</strong><small>https://api.deepseek.com</small></span></div>
      <label className="onboarding__label" htmlFor="onboarding-deepseek-key">DEEPSEEK_API_KEY</label>
      <input id="onboarding-deepseek-key" className="onboarding__input" type="password" autoComplete="off" value={key} onChange={(e) => setKey(e.target.value)} placeholder="sk-..." />
      {error && <div className="onboarding__error" role="alert">{error}</div>}
      <button className="onboarding__submit" disabled={busy || !key.trim()} onClick={() => void connect()}>{busy ? t("onboarding.validating") : t("onboarding.submit")}</button>
      <button className="onboarding__skip" disabled={busy} onClick={() => void complete()}><SkipForward size={14} />{t("onboarding.skipToApp")}</button>
      <div className="onboarding__links"><span>{t("onboarding.upgradeNote")}</span><span><Check size={13} />{t("onboarding.changeSettings")}</span></div>
    </div>
  </div>;
}
