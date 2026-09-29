@echo off
chcp 65001 >nul
echo ========================================
echo Sub2API 本地开发启动脚本
echo ========================================
echo.

cd /d "%~dp0"

where docker >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo [错误] 未检测到 Docker，请先安装 Docker Desktop
    echo 下载地址: https://www.docker.com/products/docker-desktop
    pause
    exit /b 1
)

where go >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    echo [错误] 未检测到 Go，请先安装 Go 1.27+
    echo 下载地址: https://golang.org/dl/
    pause
    exit /b 1
)

echo [1/5] 检查配置文件...
if not exist "deploy\.env" (
    echo [信息] 创建 .env 文件...
    copy deploy\.env.example deploy\.env
    echo [警告] 请编辑 deploy\.env 文件，设置数据库密码等配置
    echo.
    pause
)

echo [2/5] 启动 PostgreSQL 和 Redis...
cd deploy
docker compose -f docker-compose.local.yml up -d postgres redis
if %ERRORLEVEL% NEQ 0 (
    echo [错误] Docker 服务启动失败
    pause
    exit /b 1
)

echo [3/5] 等待数据库就绪（15秒）...
timeout /t 15 /nobreak >nul

echo [4/5] 检查 Wire 代码生成...
cd ..\backend\cmd\server
if not exist "wire_gen.go" (
    echo [信息] 首次运行，生成 Wire 依赖注入代码...
    go install github.com/google/wire/cmd/wire@latest
    wire
    if %ERRORLEVEL% NEQ 0 (
        echo [错误] Wire 代码生成失败
        cd ..\..\..
        pause
        exit /b 1
    )
)

echo [5/5] 启动后端服务...
cd ..\..
go run cmd/server/main.go

pause
