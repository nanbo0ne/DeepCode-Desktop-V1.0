# O.R.C.A. v3.0.2 Real-Usage Validation

Validation record for v3.0.2. This document records the tested source and local checks; the GitHub Release is authoritative for publication status and final asset checksums.

## Progress

- [x] Check the existing repair worktree and previous validation build.
- [x] Run real provider-backed conversations, images, attachment writes, and cancellation.
- [x] Reproduce and fix truncated risk-review responses; repeat the real classification cases.
- [x] Complete document generation and genuine-render validation fixes.
- [x] Complete context-panel and browser workflow regression checks.
- [x] Verify installer directory selection, cleanup scope, and pinned dependencies.
- [x] Run source-frozen frontend, Go, layout, and Windows package checks.
- [ ] Publish the verified version and confirm hosted downloads.

## Real Provider Scenarios

The scenarios use synthetic receipts, charts, and a temporary CSV project. No user screenshots, private project files, or conversation history were uploaded. Credentials were supplied to temporary test processes, not written into source or configuration files.

The same Go boot/controller and provider implementations used by the desktop were exercised. This is not a claim that every scenario was driven through the native window.

| Scenario | Result |
|---|---|
| Brief everyday advice in Assistant mode | Passed; one answer, no unrelated tools |
| Follow-up recall and reopening saved history | Passed |
| Cancel a real stream, then send another message | Passed |
| PNG receipt OCR: order, quantities, total | Passed |
| JPEG receipt and a follow-up question | Passed |
| Chart values, maximum/minimum, difference | Passed |
| Two-image question with distinct answers | Passed |
| Flash delegates an image to a vision-capable subagent | Passed |
| CSV reference, JSON write, readback, independent file parsing | Passed |
| Official model-list refresh | Passed |
| OpenAI-compatible and Anthropic-compatible image streaming | Passed, including stream completion |
| Model-generated DOCX, XLSX, PPTX and PDF | Passed, then independently parsed with format-specific libraries |
| Edit workbook cell B2 and preserve other numeric cells | Passed, including numeric cell type and formula storage |
| Isolated low/medium/high operational-risk classification | Initial truncation reproduced; fixed and repeated successfully |

The successful file task used `write_file` and `read_file`, returned its final result on the successful round, and produced the expected numeric total. Model outputs are variable; these are observed results, not accuracy guarantees for arbitrary images.

## New Findings

1. Risk review shared the conversation's thinking behavior, while its 96-token budget could cut off structured output. Short classifier requests now explicitly opt out of thinking where supported, use a bounded 256-token budget, require a complete stream, and observe cancellation. The normal conversation's reasoning settings are unchanged.
2. The context panel could accept a late response from a previous tab, derive API request counts from file activity, and mislabel an unfamiliar currency. The fix passed a real React A-to-B-to-late-A deferred-response test. Unknown costs remain hidden.
3. Document preview drew generic lines instead of rendering content. PDF pagination/wrapping and real first-page rendering were fixed. Three long-document pages were independently parsed, rendered and inspected. XLSX numeric storage, PPTX overflow rejection, and legacy-sidecar read compatibility passed focused tests. Office rendering and formula recalculation are not claimed.
4. Native WebView2 exposed the idle Send button without an accessible name. A localized label was added and covered by a regression test.
5. Installer path precedence was corrected using a real NSIS command-line probe. Uninstall no longer recursively deletes the selected installation folder. Process closure is limited to that folder, and runtime archives are fixed by version and SHA-256. Portable repackaging after signing retains dependencies. Real account installation was not performed.
6. Ctrl+K could fail to reopen the command palette because its handler performed a side effect inside a React state updater. After the fix, three open/close cycles and search interactions passed. The missing browser favicon was also resolved.
7. Windows release CI exposed empty Git change lists when the workspace used an 8.3 alias and Git returned an expanded root. Status paths now use Git's repository prefix. A real short-path repository test covers root/subdirectory views and preserves file-name whitespace. An unrelated MCP parsing test was isolated from launching real network-backed processes.

## Browser Workflows

Six interaction groups passed using the development bridge: new sessions and project navigation; right-panel modes; model, reasoning, and permission selection; attachments and file/history references; repeated command search; saved appearance and restart behavior. The final rerun recorded no page, request, or console errors. These UI fixtures complement, rather than replace, the real provider/controller scenarios above.

A fresh 160-case geometry matrix covered Modern/Classic, empty/running conversations, right panel open/closed, widths 1920/1366/1024/820/760, and browser DPR 1/1.25/1.5/2. No measured overlap, viewport clipping or page error was found. Modern welcome centering, scroll-area right edge and action alignment stayed within 2 pixels. Final Modern and Classic screenshots were inspected individually. This is browser DPR testing, not physical multi-monitor Windows DPI acceptance.

One side-agent visual attempt used a temporary source copy and did not finish its Classic matrix. Its screenshots were excluded from acceptance; the parent reran the matrix against the actual worktree server. Earlier harness failures from dialog readiness and one ambiguous spreadsheet-coordinate prompt were diagnosed and rerun, not counted as passing software tests.

## Native Window Check

An isolated Modern Windows instance was launched using temporary application-data directories. Maximize/restore and right-panel closure were exercised. The maximized empty state was centered within the conversation area; the Composer actions remained on the right. No existing user configuration was changed.

The external UI automation helper failed its editable-value operation with a UIA cache-property error. Native typing was not claimed as verified from that failed action. Provider-backed controller tests and separate browser input tests cover the application paths independently.

## Local Release Package

The final local Windows build reports product/file version `3.0.2.0` and product name `O.R.C.A. for Windows`. Installer extraction and the portable ZIP contain the same executable SHA-256. The ZIP integrity check passed and includes Node, its license, and CodeGraph.

The NSIS embedded CRC matched the independent offline calculation, and a one-byte mutation in memory was rejected. The calculation follows the [NSIS 3.12 loader](https://github.com/kichik/nsis/blob/v312/Source/exehead/fileform.c); the installer was not executed and `/NCRC` was not used.

The source-frozen frontend tests ran on Node 22.23.2. Production frontend/Wails packaging, root Go tests, all desktop Go packages, and diff whitespace checks passed. The final native process exposed the correct window title, but the external helper's last accessibility snapshot lacked child labels and its screenshot was occluded. That attempt is not counted as another full native interaction pass.

The first release CI run caught a missing Wails binding-generation step in the fresh-checkout frontend gate; publishing was blocked and the step was added. Development dependency advisories were addressed through compatible lockfile updates; `npm audit` then reported zero known vulnerabilities. Legacy PDF fixtures are explicitly binary so checkout cannot change their byte offsets.

Subsequent fresh-platform checks caught Windows packaging's absent parent directory and installer contract tests depending on an untracked Windows-generated include. Both were fixed without removing the native test gates. The public release is created only after all platform jobs succeed.

## Remaining Boundaries

No clean Windows VM, full local-model download, GPU inference, real financial action, or UAC bypass was performed. Native multi-monitor DPI and full Computer Use input acceptance remain hardware-dependent checks. Remote bot accounts other than the supplied model provider are not configured for live testing. Release notes must retain these distinctions and the unsigned-Windows warning.
