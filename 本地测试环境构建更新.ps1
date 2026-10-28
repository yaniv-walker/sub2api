#requires -Version 5.1
$ErrorActionPreference = 'Stop'

# 与项目 CI 保持 pnpm 9，避免全局新版 pnpm 忽略 package.json 的 overrides。
$pnpmVersion = '9.15.9'
$repo = $PSScriptRoot
$buildDir = Join-Path $repo '.tmp-go-build-cache/local-update'
$envFile = Join-Path $repo '.tmp-go-build-cache/observability-local-stack/.env'
$version = [IO.File]::ReadAllText((Join-Path $repo 'backend/cmd/server/VERSION')).Trim()
$buildEnvNames = @('GOCACHE', 'GOOS', 'GOARCH', 'CGO_ENABLED', 'CI')
$previousEnv = @{}
foreach ($name in $buildEnvNames) {
    $previousEnv[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}

Push-Location $repo
try {
    foreach ($command in @('npx.cmd', 'go', 'docker')) {
        Get-Command $command -ErrorAction Stop | Out-Null
    }
    if ((& docker context show).Trim() -ne 'desktop-linux' -or $LASTEXITCODE -ne 0) {
        throw '请先切换到本机 Docker Desktop 的 desktop-linux 上下文'
    }
    if (!(Test-Path -LiteralPath $envFile)) {
        throw '未找到本地环境配置，请先运行 tools/start-observability-local.ps1 初始化'
    }
    $content = [IO.File]::ReadAllText($envFile)
    if ($content -notmatch '(?m)^OBSERVABILITY_TEST_IMAGE=[^\r\n]+') {
        throw '本地环境配置缺少 OBSERVABILITY_TEST_IMAGE，已停止更新'
    }
    $port = '18088'
    if ($content -match '(?m)^OBSERVABILITY_TEST_PORT=(\d+)\r?$') { $port = $Matches[1] }

    # 1. 使用固定的 pnpm 版本安装锁定依赖并构建前端。
    $env:CI = 'true'
    & npx.cmd --yes "pnpm@$pnpmVersion" --dir frontend install --frozen-lockfile --prefer-offline
    if ($LASTEXITCODE -ne 0) { throw '前端依赖安装失败' }
    & npx.cmd --yes "pnpm@$pnpmVersion" --dir frontend run build
    if ($LASTEXITCODE -ne 0) { throw '前端构建失败' }

    # 2. 交叉编译 Linux 后端，嵌入前端和源码 VERSION。
    New-Item -ItemType Directory -Force -Path $buildDir | Out-Null
    $env:GOCACHE = Join-Path $repo '.tmp-go-build-cache/go-build'
    $env:GOOS = 'linux'
    $env:GOARCH = 'amd64'
    $env:CGO_ENABLED = '0'
    & go build -C backend -tags embed -trimpath `
        -o (Join-Path $buildDir 'sub2api') ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw '后端构建失败' }

    # 3. 构建包含最新程序的镜像。
    $dockerfile = "FROM sub2api:observability-ui-latest`nCOPY --chown=1000:1000 sub2api /app/sub2api`n"
    [IO.File]::WriteAllText((Join-Path $buildDir 'Dockerfile'), $dockerfile, [Text.Encoding]::ASCII)
    $image = 'sub2api:local-update-' + (Get-Date -Format 'yyyyMMdd-HHmmss')
    & docker build -t $image $buildDir
    if ($LASTEXITCODE -ne 0) { throw '镜像构建失败' }
    # 4. 更新镜像配置，保留原有密码和数据卷。
    $updatedContent = [regex]::Replace($content, '(?m)^OBSERVABILITY_TEST_IMAGE=[^\r\n]*', "OBSERVABILITY_TEST_IMAGE=$image")
    [IO.File]::WriteAllText($envFile, $updatedContent, [Text.UTF8Encoding]::new($false))

    # 5. 重建本地容器并等待健康检查。
    & (Join-Path $repo 'tools/start-observability-local.ps1')
    $baseUrl = "http://127.0.0.1:$port"
    Invoke-RestMethod "$baseUrl/health" -TimeoutSec 10 | Out-Null
    $settings = Invoke-RestMethod "$baseUrl/api/v1/settings/public" -TimeoutSec 10
    if ($settings.data.version -ne $version) {
        throw "运行版本校验失败：预期 $version，实际 $($settings.data.version)"
    }
    Write-Host "本地环境更新成功：$baseUrl，版本 $version，镜像 $image"
} finally {
    foreach ($name in $buildEnvNames) {
        [Environment]::SetEnvironmentVariable($name, $previousEnv[$name], 'Process')
    }
    Pop-Location
}
