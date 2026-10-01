# Only for the isolated sub2api-obs-local acceptance project. No production targets.
$ErrorActionPreference = 'Stop'
if ((& docker context show).Trim() -ne 'desktop-linux') { throw 'Use the local Docker Desktop context for this test stack' }
$repo = Split-Path -Parent $PSScriptRoot
$envFile = Join-Path $repo '.tmp-go-build-cache/observability-local-stack/.env'
$composeFile = Join-Path $repo 'deploy/docker-compose.observability-test.yml'
$composeArgs = @('compose', '-p', 'sub2api-obs-local', '--env-file', $envFile, '-f', $composeFile)
$app = 'sub2api-obs-local-sub2api-1'
$database = 'sub2api-obs-local-postgres-1'
foreach ($name in @($app, $database)) {
    $labels = & docker inspect --format '{{json .Config.Labels}}' $name
    if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect test container' }
    $project = ($labels | ConvertFrom-Json).'com.docker.compose.project'
    if ($project -ne 'sub2api-obs-local') { throw 'Refusing a non-test container' }
}

function Set-TestObservationFlag([bool]$enabled) {
    $value = if ($enabled) { 'true' } else { 'false' }
    $sql = 'INSERT INTO settings (key,value,updated_at) VALUES (''request_observability'',''{"enabled":FLAG}'',NOW()) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value,updated_at=EXCLUDED.updated_at;'.Replace('FLAG', $value)
    $sql | & docker exec -i $database psql -U sub2api -d sub2api -v ON_ERROR_STOP=1 -q
    if ($LASTEXITCODE -ne 0) { throw 'Test fixture write failed' }
}

function Read-ProbeLog {
    $response = Invoke-WebRequest 'http://127.0.0.1:18088/api/v1/settings/public' -UseBasicParsing -TimeoutSec 5
    $requestId = $response.Headers['X-Request-ID']
    $lines = @(& docker @composeArgs logs --no-color --no-log-prefix --tail 200 sub2api)
    if ($LASTEXITCODE -ne 0) { throw 'Cannot read test logs' }
    foreach ($line in $lines) {
        if (!$line.TrimStart().StartsWith('{')) { continue }
        try { $event = $line | ConvertFrom-Json } catch { continue }
        if ($event.msg -eq 'http request completed' -and $event.request_id -eq $requestId) { return $event }
    }
    throw 'Correlated request completion log not found'
}

function Wait-TestObservationFlag([bool]$enabled) {
    for ($attempt = 0; $attempt -lt 45; $attempt++) {
        $event = Read-ProbeLog
        $observed = $event.PSObject.Properties.Name -contains 'first_write_ms'
        if ($observed -eq $enabled) { return }
        Start-Sleep -Seconds 1
    }
    throw 'Runtime cache did not reflect the test fixture'
}

try {
    Set-TestObservationFlag $false
    Wait-TestObservationFlag $false
    Write-Output 'PASS: observation disabled on the real application'
    # Database fixture simulates another node updating shared settings. The
    # normal administrator PUT path is still gated by initial acknowledgement.
    Set-TestObservationFlag $true
    Wait-TestObservationFlag $true
    Write-Output 'PASS: shared setting refresh enables request fields without restart'
    & docker @composeArgs restart sub2api | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Test application restart failed' }
    $ready = $false
    for ($attempt = 0; $attempt -lt 30; $attempt++) {
        try { Invoke-WebRequest 'http://127.0.0.1:18088/health' -UseBasicParsing -TimeoutSec 2 | Out-Null; $ready = $true; break } catch { Start-Sleep -Seconds 1 }
    }
    if (!$ready) { throw 'Test application did not restart' }
    Wait-TestObservationFlag $true
    Write-Output 'PASS: persisted observation setting survives container restart'
    Set-TestObservationFlag $false
    Wait-TestObservationFlag $false
    Write-Output 'PASS: observation disabled again without restart'
} finally {
    Set-TestObservationFlag $false
}
