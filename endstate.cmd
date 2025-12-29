@echo off
REM Endstate CLI shim for local development - forwards all arguments to PowerShell
REM This ensures PowerShell redirection (1> and 2>) works correctly by running pwsh as a child process
REM Set ENDSTATE_ENTRYPOINT so the ps1 can verify it was invoked via the approved shim
set ENDSTATE_ENTRYPOINT=cmd
pwsh -NoProfile -ExecutionPolicy Bypass -File "%~dp0endstate.ps1" %*
