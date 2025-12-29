@echo off
REM Endstate CLI shim for local development - forwards all arguments to PowerShell
REM This ensures PowerShell redirection (1> and 2>) works correctly by running pwsh as a child process
pwsh -NoProfile -ExecutionPolicy Bypass -File "%~dp0endstate.ps1" %*
