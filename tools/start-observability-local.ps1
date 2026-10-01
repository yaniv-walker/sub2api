param([switch]$Build, [string]$BuildProxy = '')
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
if ((& docker context show).Trim() -ne 'desktop-linux') { throw 'Use the local Docker Desktop context for this test stack' }
$localDir = Join-Path $repo '.tmp-go-build-cache/observability-local-stack'
New-Item -ItemType Directory -Path $localDir -Force | Out-Null
$envFile = Join-Path $localDir '.env'
if (!(Test-Path -LiteralPath $envFile)) {
    function New-TestSecret {
        $bytes = New-Object byte[] 32
        $random = [Security.Cryptography.RandomNumberGenerator]::Create()
        try { $random.GetBytes($bytes) } finally { $random.Dispose() }
        return ([BitConverter]::ToString($bytes)).Replace('-', '').ToLowerInvariant()
    }
    $login = [ordered]@{email='local-admin@sub2api.test';password=('Local-' + (New-TestSecret));url='http://127.0.0.1:18088'}
    $lines = @('OBSERVABILITY_TEST_IMAGE=sub2api:observability-local','OBSERVABILITY_TEST_PORT=18088',"ADMIN_EMAIL=$($login.email)","ADMIN_PASSWORD=$($login.password)","POSTGRES_PASSWORD=$(New-TestSecret)","REDIS_PASSWORD=$(New-TestSecret)","JWT_SECRET=$(New-TestSecret)","TOTP_ENCRYPTION_KEY=$(New-TestSecret)")
    [IO.File]::WriteAllText($envFile, ($lines -join "`n") + "`n", [Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllText((Join-Path $localDir 'local-login.json'), ($login | ConvertTo-Json), [Text.UTF8Encoding]::new($false))
}
Push-Location $repo
try {
    if ($Build) {
        $revision = (& git rev-parse --short HEAD).Trim()
        $buildArgs = @('build','--platform','linux/amd64','--build-arg',"VERSION=obs-$revision",'--build-arg',"COMMIT=$revision",'--build-arg','POSTGRES_IMAGE=docker.m.daocloud.io/library/postgres:18-alpine','-t','sub2api:observability-local')
        if ($BuildProxy) { $buildArgs += @('--build-arg',"HTTPS_PROXY=$BuildProxy",'--build-arg',"HTTP_PROXY=$BuildProxy") }
        & docker @buildArgs .
        if ($LASTEXITCODE -ne 0) { throw 'Local image build failed' }
    }
    $composeArgs = @('compose','-p','sub2api-obs-local','--env-file',$envFile,'-f',(Join-Path $repo 'deploy/docker-compose.observability-test.yml'))
    & docker @composeArgs config --quiet
    if ($LASTEXITCODE -ne 0) { throw 'Invalid local Compose configuration' }
    & docker @composeArgs up -d --wait --wait-timeout 180
    if ($LASTEXITCODE -ne 0) { throw 'Local containers did not become healthy' }
    Write-Output 'Local application: http://127.0.0.1:18088'
    Write-Output "Local administrator login file: $(Join-Path $localDir 'local-login.json')"
} finally { Pop-Location }
