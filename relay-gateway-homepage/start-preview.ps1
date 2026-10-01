param([switch]$LivePublic)
$ErrorActionPreference='Stop'
Set-Location -LiteralPath $PSScriptRoot
$taskArgs=@('tools/server.mjs')
if($LivePublic){$taskArgs+='--live-public'}
node @taskArgs
