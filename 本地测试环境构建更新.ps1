Set-Location 'E:\ai\claude\dev\sub2api-original'
$ErrorActionPreference = 'Stop'

# 1. 安装依赖并构建前端
pnpm --dir frontend install --frozen-lockfile --prefer-offline
if ($LASTEXITCODE -ne 0) { throw '前端依赖安装失败' }

pnpm --dir frontend run build
if ($LASTEXITCODE -ne 0) { throw '前端构建失败' }

# 2. 编译后端，嵌入最新前端页面
$repo = (Get-Location).Path
$buildDir = Join-Path $repo '.tmp-go-build-cache/local-update'
New-Item -ItemType Directory -Force -Path $buildDir | Out-Null

$env:GOCACHE = Join-Path $repo '.tmp-go-build-cache/go-build'
$env:GOMODCACHE = Join-Path $repo '.tmp-go-build-cache/go-mod'
$env:GOOS = 'linux'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'

go build -C backend -tags embed -trimpath `
  -o (Join-Path $buildDir 'sub2api') ./cmd/server
if ($LASTEXITCODE -ne 0) { throw '后端构建失败' }

# 3. 使用现有本地运行环境构建新镜像
@'
FROM sub2api:observability-ui-latest
COPY --chown=1000:1000 sub2api /app/sub2api
'@ | Set-Content (Join-Path $buildDir 'Dockerfile') -Encoding ascii

$image = 'sub2api:local-update-' + (Get-Date -Format 'yyyyMMdd-HHmmss')
docker build -t $image $buildDir
if ($LASTEXITCODE -ne 0) { throw '镜像构建失败' }

# 4. 更新本地镜像配置
$envFile = Join-Path $repo '.tmp-go-build-cache/observability-local-stack/.env'
$content = [IO.File]::ReadAllText($envFile)
$content = [regex]::Replace(
    $content,
    '(?m)^OBSERVABILITY_TEST_IMAGE=[^\r\n]*',
    "OBSERVABILITY_TEST_IMAGE=$image"
)
[IO.File]::WriteAllText($envFile, $content, [Text.UTF8Encoding]::new($false))

# 5. 启动更新后的本地容器，等待健康检查通过
.\tools\start-observability-local.ps1

# 6. 检查服务
Invoke-RestMethod 'http://127.0.0.1:18088/health'