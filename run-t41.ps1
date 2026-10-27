param([string]$Pattern, [string]$Extra = '')
$env:GOTOOLCHAIN = 'auto'
$env:GOPROXY = 'https://goproxy.cn,direct'
$env:GOSUMDB = 'sum.golang.google.cn'
$env:CGO_ENABLED = '0'
$env:MYGO_E2E = '1'
if ($Extra -ne '') { $env:WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS = $Extra }
Set-Location $PSScriptRoot
$out = Join-Path $env:USERPROFILE 'mygo-t41-out.txt'
$err = Join-Path $env:USERPROFILE 'mygo-t41-err.txt'
$p = Start-Process -FilePath go -ArgumentList "test ./internal/e2e -run `"$Pattern`" -v -count=1 -timeout 120s" -RedirectStandardOutput $out -RedirectStandardError $err -PassThru -WindowStyle Hidden
$p.WaitForExit(55000) | Out-Null
Get-Content $out | Out-String -Width 4000
Get-Content $err | Select-Object -Last 5
