@echo off
setlocal
"%~dp0rmic.exe" --setup
if errorlevel 1 pause
