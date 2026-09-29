@echo off
echo Starting Sub2API backend service...
cd /d "%~dp0backend"
go run cmd/server/main.go
pause
