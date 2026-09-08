# Browser Functional Fix Evidence

Date: 2026-09-07
Worktree: `D:\AI-Reasonix\.tmp\v2.1.3-worktree`
Branch: `codex/v3.0.0`
URL: `http://127.0.0.1:41874/?platform=windows`

## Command

Used the bundled runtime, with `NODE_PATH` set to its bundled `node_modules`:

```powershell
$node='C:\Users\16260\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin\node.exe'
$env:NODE_PATH='C:\Users\16260\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\node_modules'
& $node '.tmp/repair-2026-09-07/browser-functional-fix.cjs'
```

The parent server was left running. No real API or user image was used.

## Result

`functional-fix.json` reports 14/14 scenarios passed, with no crash overlay and no page errors:

- Settings read rejection shows a retry; the subsequent read restores the page.
- Install, model download, runtime stop, model delete, pause, resume, and cancel rejections stay in-page and re-enable controls.
- A successful delete followed by catalog refresh failure exposes a catalog-only retry. `DeleteLocalModel` was called once before and once after retry; `GetLocalAICatalog` advanced from 3 to 4.
- Paused shows resume/cancel, failed shows retry download, cancelled shows download again, and verifying shows cancel without resume.
- Settings Shift+Tab and Tab wrap within the dialog; closing returns focus to the opener.

Three screenshots were captured and inspected individually: `settings-error-fix.png`, `paused-download-fix.png`, and `failed-download-fix.png`.

## Related Changes

- LocalAI reload retry no longer repeats a successful mutation.
- `gpuDetectionFailed` is typed, normalized, represented in the browser mock, and localized separately from no discrete GPU.
- English and Chinese composer placeholders are concise; slash/@/shell shortcuts remain in existing menus.
- Added `status.details`: `Usage details` / `统计详情` for the parent statusbar work.
