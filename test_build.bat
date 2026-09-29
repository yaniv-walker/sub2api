@echo off
cd backend
echo Testing build...
go build -o temp_sub2api.exe ./cmd/server 2>&1
if errorlevel 1 (
    echo Build FAILED
    exit /b 1
) else (
    echo Build SUCCESS
    del temp_sub2api.exe 2>nul
    exit /b 0
)
