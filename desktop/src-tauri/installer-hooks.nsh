; Tauri CLI 2.11.3 includes utils.nsh BEFORE installerHooks, then inserts
; CheckIfAppIsRunning before both installation and uninstallation file changes.
; Replace that macro rather than checking only in PREINSTALL: a process could
; appear between a preinstall check and the stock macro's basename-based kill.
; This hook never terminates a process, including another portable copy.
!ifmacrondef CheckIfAppIsRunning
  !error "LumaTape requires Tauri's CheckIfAppIsRunning macro before installerHooks; review the installer template."
!endif
!macroundef CheckIfAppIsRunning

!macro CheckIfAppIsRunning executableName productName
  !define LUMATAPE_GUARD_ID ${__COUNTER__}
  Push $R0
  Push $R1
  Push $R2

  ; The updater has already restored the game and is about to exit normally.
  ; Wait at most ten seconds for that exit. A manual install/uninstall instead
  ; tells the user to quit the running application before retrying.
  StrCpy $R2 0
  ${If} $UpdateMode = 1
    StrCpy $R2 100
  ${EndIf}

  lumatape_guard_poll_${LUMATAPE_GUARD_ID}:
    ; nsis-tauri-utils 0.5.3: 0 = found, 1 = not found.
    nsis_tauri_utils::FindProcessCurrentUser "${executableName}"
    Pop $R0
    ${If} $R0 == 1
      Goto lumatape_guard_done_${LUMATAPE_GUARD_ID}
    ${EndIf}
    ${If} $R0 == 0
    ${AndIf} $R2 > 0
      IntOp $R2 $R2 - 1
      Sleep 100
      Goto lumatape_guard_poll_${LUMATAPE_GUARD_ID}
    ${EndIf}

    ; Choose the user's Windows UI language, as the tray does. Do not depend on
    ; an installer language picker or expose a kill/retry action in passive mode.
    System::Call 'kernel32::GetUserDefaultUILanguage() i.R0'
    IntOp $R0 $R0 & 0x3ff
    ${If} $R0 == 0x19
      StrCpy $R1 "Не удалось подтвердить закрытие ${productName}. Выберите «Выход» в трее LumaTape, затем повторите установку или удаление. Запущенные приложения не будут завершены автоматически."
    ${Else}
      StrCpy $R1 "Could not confirm that ${productName} has closed. Choose Quit in the LumaTape tray, then retry installation or removal. Running applications will not be terminated automatically."
    ${EndIf}
    DetailPrint "$R1"
    ; A silent installer must also fail without waiting for invisible input.
    IfSilent lumatape_guard_abort_${LUMATAPE_GUARD_ID}
      MessageBox MB_OK|MB_ICONEXCLAMATION "$R1"
    lumatape_guard_abort_${LUMATAPE_GUARD_ID}:
      Pop $R2
      Pop $R1
      Pop $R0
      SetErrorLevel 2
      Abort

  lumatape_guard_done_${LUMATAPE_GUARD_ID}:
    Pop $R2
    Pop $R1
    Pop $R0
  !undef LUMATAPE_GUARD_ID
!macroend

; Tauri's stock WebView2 section deliberately skips /UPDATE. Versions through
; 0.3.4 were tray-only, so an existing installation may have no WebView2 runtime.
; Reuse Tauri's embedded bootstrapper before replacing any application files.
!macro LumaTapeReadWebViewVersion result
  !define LUMATAPE_WEBVIEW_READ_ID ${__COUNTER__}
  ReadRegStr ${result} HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\${WEBVIEW2APPGUID}" "pv"
  StrCmp ${result} "" lumatape_webview_user_${LUMATAPE_WEBVIEW_READ_ID}
  StrCmp ${result} "0.0.0.0" lumatape_webview_user_${LUMATAPE_WEBVIEW_READ_ID} lumatape_webview_read_done_${LUMATAPE_WEBVIEW_READ_ID}
  lumatape_webview_user_${LUMATAPE_WEBVIEW_READ_ID}:
    ReadRegStr ${result} HKCU "SOFTWARE\Microsoft\EdgeUpdate\Clients\${WEBVIEW2APPGUID}" "pv"
    StrCmp ${result} "0.0.0.0" 0 lumatape_webview_read_done_${LUMATAPE_WEBVIEW_READ_ID}
    StrCpy ${result} ""
  lumatape_webview_read_done_${LUMATAPE_WEBVIEW_READ_ID}:
  !undef LUMATAPE_WEBVIEW_READ_ID
!macroend

!macro NSIS_HOOK_PREINSTALL
  !if "${ARCH}" != "x64"
    !error "Review LumaTape WebView2 registry detection before changing the Windows target."
  !endif
  !if "${INSTALLWEBVIEW2MODE}" != "embedBootstrapper"
    !error "LumaTape updates require Tauri's embedded WebView2 bootstrapper."
  !endif
  Push $R0
  Push $R1
  StrCmp $UpdateMode 1 0 lumatape_webview_done
  !insertmacro LumaTapeReadWebViewVersion $R0
  StrCmp $R0 "" 0 lumatape_webview_done

  InitPluginsDir
  ClearErrors
  File "/oname=$PLUGINSDIR\lumatape-webview2-bootstrapper.exe" "${WEBVIEW2BOOTSTRAPPERPATH}"
  IfErrors lumatape_webview_failed
  DetailPrint "$(installingWebview2)"
  ClearErrors
  ExecWait '"$PLUGINSDIR\lumatape-webview2-bootstrapper.exe" ${WEBVIEW2INSTALLERARGS} /install' $R1
  IfErrors lumatape_webview_failed
  StrCmp $R1 0 0 lumatape_webview_failed
  ; A successful bootstrapper process is not proof that the runtime is present.
  !insertmacro LumaTapeReadWebViewVersion $R0
  StrCmp $R0 "" lumatape_webview_failed
  DetailPrint "$(webview2InstallSuccess)"
  Goto lumatape_webview_done

  lumatape_webview_failed:
    System::Call 'kernel32::GetUserDefaultUILanguage() i.R0'
    IntOp $R0 $R0 & 0x3ff
    ${If} $R0 == 0x19
      StrCpy $R1 "Не удалось установить Microsoft Edge WebView2 для окна настроек. Файлы LumaTape ещё не заменены. Проверьте подключение к интернету и повторите установку."
    ${Else}
      StrCpy $R1 "Could not install Microsoft Edge WebView2 for settings. LumaTape files have not been replaced. Check your internet connection and retry the installation."
    ${EndIf}
    DetailPrint "$R1"
    IfSilent lumatape_webview_abort
      MessageBox MB_OK|MB_ICONEXCLAMATION "$R1"
    lumatape_webview_abort:
      Pop $R1
      Pop $R0
      SetErrorLevel 3
      Abort

  lumatape_webview_done:
    Pop $R1
    Pop $R0
    ClearErrors
!macroend
