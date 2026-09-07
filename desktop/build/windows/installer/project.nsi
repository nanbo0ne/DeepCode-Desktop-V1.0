Unicode true

####
## O.R.C.A for Windows per-user NSIS installer.
##
## This file is COMMITTED and customized (Wails leaves an existing project.nsi
## untouched and only regenerates wails_tools.nsh). The customizations vs.
## Wails' default template:
##
##   1. REQUEST_EXECUTION_LEVEL "user" + InstallDir under $LOCALAPPDATA - install
##      without administrator rights. This is what lets the auto-updater re-run a
##      freshly downloaded installer silently (`/S`) with no UAC prompt.
##   2. Uninstall registry under HKCU (not HKLM). Wails' wails.writeUninstaller /
##      wails.deleteUninstaller macros hard-code HKLM, which a non-admin install
##      cannot write - so we inline HKCU versions below instead.
##   3. InstallDir is remembered across updates via InstallDirRegKey +
##      InstallLocation (HKCU\...\Uninstall\InstallLocation). When upgrading from
##      a build that did not write InstallLocation yet, .onInit falls back to the
##      old DisplayIcon path before using the default. Without this, every release
##      forces the user back to %LOCALAPPDATA%\Programs\DeepSeek-Orca even if they had
##      moved the install to a different drive (e.g. D:\Tools\DeepSeek-Orca); the silent
##      auto-updater would re-run with /S into the wrong dir, leaving the old
##      install orphaned.
##
## Everything else mirrors Wails' generated default. Defines below override the
## ProjectInfo values that wails_tools.nsh would otherwise populate.
####

## Install per-user (no admin). Must be defined BEFORE including wails_tools.nsh,
## which only sets the "admin" default when REQUEST_EXECUTION_LEVEL is undefined.
!define REQUEST_EXECUTION_LEVEL "user"
!define UNINST_KEY_NAME "O.R.C.A for Windows"
!define PRODUCT_EXECUTABLE "Orca.exe"
!define LEGACY_UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\DeepSeek-Orca"

####
## Include the wails tools (auto-generated; provides INFO_* defines and the
## wails.* macros used below).
####
!include "wails_tools.nsh"
!include "FileFunc.nsh"
!include "LogicLib.nsh"

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"
!include "nsDialogs.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp" #Include this to add a bitmap on the left side of the Welcome Page. Must be a size of 164x314
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
!define MUI_FINISHPAGE_RUN_TEXT "运行 ${INFO_PRODUCTNAME}"
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.
!define MUI_LICENSEPAGE_CHECKBOX

Var DeleteSavedDataCheckbox
Var DeleteSavedData
Var DesktopShortcutCheckbox
Var CreateDesktopShortcut

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
!insertmacro MUI_PAGE_LICENSE "resources\eula.txt" # Adds a EULA page to the installer
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
Page custom InstallOptionsPage InstallOptionsPageLeave
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_CONFIRM # Confirm uninstall page.
UninstPage custom un.DeleteDataPage un.DeleteDataPageLeave
!insertmacro MUI_UNPAGE_INSTFILES # Uninstalling page

!insertmacro MUI_LANGUAGE "SimpChinese" # Set the Language of the installer

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\O.R.C.A-for-Windows-windows-${ARCH}-installer.exe" # Name of the installer's file.
!define ORCA_DEFAULT_INSTALLDIR "$LOCALAPPDATA\Programs\O.R.C.A for Windows"
!define ORCA_INSTALLDIR_SENTINEL "$LOCALAPPDATA\Programs\O.R.C.A for Windows.__nsis_default__"
InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation" # Reuse the previous install path on update; .onInit falls back to the default on first install.
InstallDir "${ORCA_INSTALLDIR_SENTINEL}" # .onInit replaces this sentinel when no /D or registry path exists.
ShowInstDetails show # This will always show the installation details.

####
## Per-user uninstaller registry (HKCU). Replaces wails.writeUninstaller /
## wails.deleteUninstaller, which write HKLM and would fail without admin rights.
####
!macro orca.writeUninstaller
    WriteUninstaller "$INSTDIR\uninstall.exe"

    WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
    WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"
    # Persist the resolved install path so a subsequent update picks it up
    # via InstallDirRegKey above. Without this, every release would force the
    # user back to %LOCALAPPDATA%\Programs\DeepSeek-Orca even if they had moved
    # the install to a different drive (e.g. D:\Tools\DeepSeek-Orca). The auto-
    # updater re-runs this installer with /S and trusts the persisted path,
    # so it has to be present before the silent re-install.
    WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"

    ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
    IntFmt $0 "0x%08X" $0
    WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" "$0"
!macroend

!macro orca.deleteUninstaller
    Delete "$INSTDIR\uninstall.exe"
    Delete "$INSTDIR\uninstall.bat"
    DeleteRegKey HKCU "${UNINST_KEY}"
!macroend

Function .onInit
   !insertmacro wails.checkArchitecture
   SetShellVarContext current

   ; NSIS consumes /D before $CMDLINE is exposed, so use a compile-time
   ; sentinel to distinguish "no /D" from an explicit path. /D therefore keeps
   ; its documented highest priority even when it equals the normal default.
   StrCmp $INSTDIR "${ORCA_INSTALLDIR_SENTINEL}" use_compat_install_dir install_dir_done

use_compat_install_dir:
   StrCpy $INSTDIR "${ORCA_DEFAULT_INSTALLDIR}"
   ClearErrors
   ReadRegStr $0 HKCU "${UNINST_KEY}" "InstallLocation"
   IfErrors current_display_icon
   StrCmp $0 "" current_display_icon
   StrCpy $INSTDIR $0
   Goto install_dir_done

current_display_icon:
   ClearErrors
   ReadRegStr $0 HKCU "${UNINST_KEY}" "DisplayIcon"
   IfErrors legacy_install_dir
   StrCmp $0 "" legacy_install_dir
   ${GetParent} "$0" $INSTDIR
   StrCmp $INSTDIR "" legacy_install_dir install_dir_done

legacy_install_dir:
   ; V2 used a separate uninstall key. Reuse its path for an in-place upgrade.
   ClearErrors
   ReadRegStr $0 HKCU "${LEGACY_UNINST_KEY}" "InstallLocation"
   IfErrors legacy_display_icon
   StrCmp $0 "" legacy_display_icon
   StrCpy $INSTDIR $0
   Goto install_dir_done

legacy_display_icon:
   ClearErrors
   ReadRegStr $0 HKCU "${LEGACY_UNINST_KEY}" "DisplayIcon"
   IfErrors install_dir_default
   StrCmp $0 "" install_dir_default
   ${GetParent} "$0" $INSTDIR
   StrCmp $INSTDIR "" install_dir_default install_dir_done

install_dir_default:
   StrCpy $INSTDIR "${ORCA_DEFAULT_INSTALLDIR}"

install_dir_done:

   ; First installs create a desktop shortcut by default. Upgrades preserve the
   ; user's existing choice, including a shortcut they deliberately removed.
   StrCpy $CreateDesktopShortcut ${BST_CHECKED}
   ClearErrors
   ReadRegStr $1 HKCU "${UNINST_KEY}" "DisplayName"
   IfErrors shortcut_choice_done
   StrCmp $1 "" shortcut_choice_done
   IfFileExists "$DESKTOP\${INFO_PRODUCTNAME}.lnk" shortcut_choice_done 0
   IfFileExists "$DESKTOP\O.R.C.A for Windows.lnk" shortcut_choice_done 0
   StrCpy $CreateDesktopShortcut ${BST_UNCHECKED}

shortcut_choice_done:
FunctionEnd

Function orca.closeTargetProcesses
    InitPluginsDir
    FileOpen $0 "$PLUGINSDIR\orca-close-processes.ps1" w
    FileWrite $0 "$$ErrorActionPreference = 'SilentlyContinue'$\r$\n"
    FileWrite $0 "$$targetDir = [IO.Path]::GetFullPath($$args[0])$\r$\n"
    FileWrite $0 "$$targetPaths = @([IO.Path]::Combine($$targetDir, 'Orca.exe'), [IO.Path]::Combine($$targetDir, 'deepseek-orca-desktop.exe'), [IO.Path]::Combine($$targetDir, 'node.exe'))$\r$\n"
    FileWrite $0 "$$names = @('Orca', 'deepseek-orca-desktop', 'node')$\r$\n"
    FileWrite $0 "function Get-TargetProcesses { @(Get-Process -Name $$names -ErrorAction SilentlyContinue | Where-Object { try { $$path = $$_.Path; $$path -and ($$targetPaths -contains [IO.Path]::GetFullPath($$path)) } catch { $$false } }) }$\r$\n"
    FileWrite $0 "foreach ($$process in @(Get-TargetProcesses)) { if ($$process.MainWindowHandle -ne 0) { [void]$$process.CloseMainWindow() } }$\r$\n"
    FileWrite $0 "$$deadline = [DateTime]::UtcNow.AddSeconds(5)$\r$\n"
    FileWrite $0 "do { $$alive = @(Get-TargetProcesses); if ($$alive.Count -eq 0) { exit 0 }; Start-Sleep -Milliseconds 250 } while ([DateTime]::UtcNow -lt $$deadline)$\r$\n"
    FileWrite $0 "exit 2$\r$\n"
    FileClose $0

close_target_processes_retry:
    nsExec::ExecToStack /TIMEOUT=8000 '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\orca-close-processes.ps1" "$INSTDIR"'
    Pop $1
    Pop $0
    StrCmp $1 "0" close_target_processes_done
    IfSilent close_target_processes_silent_failed close_target_processes_prompt

close_target_processes_prompt:
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "${INFO_PRODUCTNAME} is still running from the selected install folder. Close it and click Retry, or cancel the operation." IDRETRY close_target_processes_retry
    Goto close_target_processes_failed

close_target_processes_silent_failed:
    SetErrorLevel 66

close_target_processes_failed:
    Abort

close_target_processes_done:
FunctionEnd

Function un.orca.closeTargetProcesses
    InitPluginsDir
    FileOpen $0 "$PLUGINSDIR\orca-close-processes.ps1" w
    FileWrite $0 "$$ErrorActionPreference = 'SilentlyContinue'$\r$\n"
    FileWrite $0 "$$targetDir = [IO.Path]::GetFullPath($$args[0])$\r$\n"
    FileWrite $0 "$$targetPaths = @([IO.Path]::Combine($$targetDir, 'Orca.exe'), [IO.Path]::Combine($$targetDir, 'deepseek-orca-desktop.exe'), [IO.Path]::Combine($$targetDir, 'node.exe'))$\r$\n"
    FileWrite $0 "$$names = @('Orca', 'deepseek-orca-desktop', 'node')$\r$\n"
    FileWrite $0 "function Get-TargetProcesses { @(Get-Process -Name $$names -ErrorAction SilentlyContinue | Where-Object { try { $$path = $$_.Path; $$path -and ($$targetPaths -contains [IO.Path]::GetFullPath($$path)) } catch { $$false } }) }$\r$\n"
    FileWrite $0 "foreach ($$process in @(Get-TargetProcesses)) { if ($$process.MainWindowHandle -ne 0) { [void]$$process.CloseMainWindow() } }$\r$\n"
    FileWrite $0 "$$deadline = [DateTime]::UtcNow.AddSeconds(5)$\r$\n"
    FileWrite $0 "do { $$alive = @(Get-TargetProcesses); if ($$alive.Count -eq 0) { exit 0 }; Start-Sleep -Milliseconds 250 } while ([DateTime]::UtcNow -lt $$deadline)$\r$\n"
    FileWrite $0 "exit 2$\r$\n"
    FileClose $0

un_close_target_processes_retry:
    nsExec::ExecToStack /TIMEOUT=8000 '"$SYSDIR\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$PLUGINSDIR\orca-close-processes.ps1" "$INSTDIR"'
    Pop $1
    Pop $0
    StrCmp $1 "0" un_close_target_processes_done
    IfSilent un_close_target_processes_silent_failed un_close_target_processes_prompt

un_close_target_processes_prompt:
    MessageBox MB_RETRYCANCEL|MB_ICONEXCLAMATION "${INFO_PRODUCTNAME} is still running from the selected install folder. Close it and click Retry, or cancel the operation." IDRETRY un_close_target_processes_retry
    Goto un_close_target_processes_failed

un_close_target_processes_silent_failed:
    SetErrorLevel 66

un_close_target_processes_failed:
    Abort

un_close_target_processes_done:
FunctionEnd

Function InstallOptionsPage
    nsDialogs::Create 1018
    Pop $0
    ${If} $0 == error
        Abort
    ${EndIf}

    ${NSD_CreateLabel} 0 0 100% 24u "选择安装选项"
    Pop $0
    ${NSD_CreateCheckbox} 0 32u 100% 24u "创建桌面快捷方式"
    Pop $DesktopShortcutCheckbox
    ${If} $CreateDesktopShortcut == ${BST_CHECKED}
        ${NSD_Check} $DesktopShortcutCheckbox
    ${Else}
        ${NSD_Uncheck} $DesktopShortcutCheckbox
    ${EndIf}

    nsDialogs::Show
FunctionEnd

Function InstallOptionsPageLeave
    ${NSD_GetState} $DesktopShortcutCheckbox $CreateDesktopShortcut
FunctionEnd

Section
    !insertmacro wails.setShellContext

    DetailPrint "Closing running ${INFO_PRODUCTNAME} from the selected install folder..."
    Call orca.closeTargetProcesses

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR
    Delete "$INSTDIR\uninstall.bat"

    !insertmacro wails.files
    File /oname=node.exe "..\installer-go\payload\node.exe"
    File /oname=LICENSE.node.txt "..\installer-go\payload\LICENSE.node.txt"
    SetOutPath "$INSTDIR\codegraph"
    File /r "..\installer-go\payload\codegraph\*.*"
    SetOutPath "$INSTDIR"

    Delete "$SMPROGRAMS\O.R.C.A for Windows.lnk"
    Delete "$SMPROGRAMS\Uninstall O.R.C.A for Windows.lnk"
    Delete "$DESKTOP\O.R.C.A for Windows.lnk"
    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    ${If} $CreateDesktopShortcut == ${BST_CHECKED}
        CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    ${Else}
        Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"
    ${EndIf}
    CreateShortcut "$SMPROGRAMS\Uninstall ${INFO_PRODUCTNAME}.lnk" "$INSTDIR\uninstall.exe"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    Delete "$SMPROGRAMS\DeepSeek-Orca.lnk"
    Delete "$DESKTOP\DeepSeek-Orca.lnk"
    !insertmacro orca.writeUninstaller

    ; Retire V2 only when this install actually replaced its executable folder.
    ReadRegStr $0 HKCU "${LEGACY_UNINST_KEY}" "InstallLocation"
    StrCmp $0 "" 0 legacy_cleanup_check
    ReadRegStr $0 HKCU "${LEGACY_UNINST_KEY}" "DisplayIcon"
    StrCmp $0 "" legacy_cleanup_done
    ${GetParent} "$0" $0
legacy_cleanup_check:
    GetFullPathName $0 "$0"
    GetFullPathName $1 "$INSTDIR"
    StrCmp $0 $1 0 legacy_cleanup_done
    Delete "$INSTDIR\deepseek-orca-desktop.exe"
    Delete "$SMPROGRAMS\Uninstall DeepSeek-Orca.lnk"
    DeleteRegKey HKCU "${LEGACY_UNINST_KEY}"
legacy_cleanup_done:
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    Call un.orca.closeTargetProcesses

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$SMPROGRAMS\Uninstall ${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"
    Delete "$SMPROGRAMS\O.R.C.A for Windows.lnk"
    Delete "$SMPROGRAMS\Uninstall O.R.C.A for Windows.lnk"
    Delete "$DESKTOP\O.R.C.A for Windows.lnk"
    Delete "$SMPROGRAMS\DeepSeek-Orca.lnk"
    Delete "$DESKTOP\DeepSeek-Orca.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    ${If} $DeleteSavedData == ${BST_CHECKED}
        RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath with saved data
        RMDir /r "$AppData\deepseek-orca"
        RMDir /r "$AppData\orca"
        RMDir /r "$LocalAppData\deepseek-orca"
        RMDir /r "$Profile\.deepseek-orca"
        RMDir /r "$AppData\O.R.C.A"
        RMDir /r "$LocalAppData\O.R.C.A"
        RMDir /r "$INSTDIR\data"
        RMDir /r "$INSTDIR\.deepseek-orca"
    ${EndIf}

    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$INSTDIR\node.exe"
    Delete "$INSTDIR\LICENSE.node.txt"
    RMDir /r "$INSTDIR\codegraph"
    !insertmacro orca.deleteUninstaller

    ; Never recursively remove a user-selected install directory: it may contain
    ; unrelated files. Known app-owned entries above are removed explicitly.
    RMDir "$INSTDIR"
SectionEnd

Function un.DeleteDataPage
    nsDialogs::Create 1018
    Pop $0
    ${If} $0 == error
        Abort
    ${EndIf}

    ${NSD_CreateLabel} 0 0 100% 24u "Remove saved O.R.C.A. data?"
    Pop $0
    ${NSD_CreateCheckbox} 0 32u 100% 24u "Delete configuration, conversations, memory, cache, and other saved data. This cannot be undone."
    Pop $DeleteSavedDataCheckbox
    ${NSD_Uncheck} $DeleteSavedDataCheckbox

    nsDialogs::Show
FunctionEnd

Function un.DeleteDataPageLeave
    ${NSD_GetState} $DeleteSavedDataCheckbox $DeleteSavedData
FunctionEnd
