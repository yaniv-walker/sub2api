param([int]$Port = 18081)
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$backend = Join-Path $repo 'backend'
$outputDir = Join-Path $repo '.tmp-go-build-cache/observability-smoke'
New-Item -ItemType Directory -Path $outputDir -Force | Out-Null
$exe = Join-Path $outputDir 'demo.exe'
$previousCache = $env:GOCACHE
try {
    $env:GOCACHE = Join-Path $repo '.tmp-go-build-cache'
    Push-Location $backend
    try {
        & go build -o $exe ./cmd/observability-demo
        if ($LASTEXITCODE -ne 0) { throw 'Demo build failed' }
    } finally { Pop-Location }

    foreach ($enabled in @($false, $true)) {
        $mode = if ($enabled) { 'enabled' } else { 'disabled' }
        $stdout = Join-Path $outputDir "$mode.jsonl"
        $stderr = Join-Path $outputDir "$mode.stderr.log"
        # Fail before launching if another process already owns this port.
        $probe = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $Port)
        $probe.Start()
        $probe.Stop()
        $arguments = @('-port', "$Port", "-observe=$($enabled.ToString().ToLowerInvariant())")
        $process = Start-Process -FilePath $exe -ArgumentList $arguments -WindowStyle Hidden -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $stderr
        try {
            $base = "http://127.0.0.1:$Port"
            $ready = $false
            for ($i = 0; $i -lt 40; $i++) {
                if ($process.HasExited) { throw "Demo exited; see $stderr" }
                try { Invoke-WebRequest "$base/demo/plain" -UseBasicParsing -TimeoutSec 1 | Out-Null; $ready = $true; break } catch { Start-Sleep -Milliseconds 100 }
            }
            if (!$ready) { throw 'Demo did not become ready' }
            & curl.exe -sS -N --max-time 5 "$base/demo/stream"
            if ($LASTEXITCODE -ne 0) { throw 'Streaming demo failed' }
            & curl.exe -sS --max-time 5 "$base/demo/retry"
            if ($LASTEXITCODE -ne 0) { throw 'Mock retry failed' }
            # Timeout is intentional: cancel after the first SSE chunk.
            # Windows PowerShell 5.1 turns native stderr into a terminating
            # error under ErrorActionPreference=Stop, even with 2>$null.
            # Run the expected failure separately and inspect its exit code.
            $cancelStdout = Join-Path $outputDir "$mode.cancel.stdout.log"
            $cancelStderr = Join-Path $outputDir "$mode.cancel.stderr.log"
            $cancel = Start-Process -FilePath 'curl.exe' -ArgumentList @('-sS', '-N', '--max-time', '0.5', "$base/demo/stream") -WindowStyle Hidden -Wait -PassThru -RedirectStandardOutput $cancelStdout -RedirectStandardError $cancelStderr
            try {
                Get-Content -LiteralPath $cancelStdout
                if ($cancel.ExitCode -ne 28) { throw "Expected curl cancellation (exit 28), got $($cancel.ExitCode); see $cancelStderr" }
            } finally { $cancel.Dispose() }
            Start-Sleep -Milliseconds 300
            $events = @(Get-Content -LiteralPath $stdout | ForEach-Object { $_ | ConvertFrom-Json })
            $completed = @($events | Where-Object { $_.msg -eq 'http request completed' })
            $retry = @($completed | Where-Object { $_.path -eq '/demo/retry' })[-1]
            $streams = @($completed | Where-Object { $_.path -eq '/demo/stream' })
            if ($enabled) {
                if ($retry.upstream_attempts -ne 2 -or $retry.upstream_errors -ne 1 -or $retry.terminal_reason -ne 'completed') { throw 'Retry counters/outcome mismatch' }
                if ($streams[0].write_count -ne 4 -or $streams[0].streaming -ne $true) { throw 'Streaming measurements missing' }
                if ($streams[-1].terminal_reason -ne 'client_cancelled') { throw 'Client cancellation was not observed' }
                $completed | Select-Object request_id, path, first_write_ms, max_write_gap_ms, terminal_reason, upstream_attempts, upstream_errors | Format-Table -AutoSize
            } elseif ($retry.PSObject.Properties.Name -contains 'upstream_attempts') { throw 'Disabled mode unexpectedly collected telemetry' }
            Write-Output "PASS: $mode; logs: $stdout"
        } finally {
            if (!$process.HasExited) { Stop-Process -Id $process.Id -Force }
            $process.Dispose()
        }
        $Port++
    }
} finally { $env:GOCACHE = $previousCache }
