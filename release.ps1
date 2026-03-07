param(
    [Parameter(Mandatory = $true)]
    [string]$Version
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Invoke-GoBuild {
    param(
        [string]$WorkDir,
        [hashtable]$EnvMap,
        [string]$Output,
        [string]$Ldflags
    )

    Push-Location $WorkDir
    try {
        go mod tidy

        $oldEnv = @{}
        foreach ($key in $EnvMap.Keys) {
            $oldEnv[$key] = [Environment]::GetEnvironmentVariable($key, "Process")
            [Environment]::SetEnvironmentVariable($key, $EnvMap[$key], "Process")
        }

        try {
            go build -ldflags $Ldflags -o $Output .
        }
        finally {
            foreach ($key in $EnvMap.Keys) {
                [Environment]::SetEnvironmentVariable($key, $oldEnv[$key], "Process")
            }
        }
    }
    finally {
        Pop-Location
    }
}

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
$distRoot = Join-Path $root "dist"
$distDir = Join-Path $distRoot $Version

if (Test-Path $distDir) {
    Remove-Item $distDir -Recurse -Force
}

New-Item -ItemType Directory -Path $distDir | Out-Null

$winBuildDir = Join-Path $root "win"
$openwrtBuildDir = Join-Path $root "openwrt"

$stageWin = Join-Path $distDir "stage-win"
$stageArm64 = Join-Path $distDir "stage-openwrt-arm64"
$stageMipsle = Join-Path $distDir "stage-openwrt-mipsle"

New-Item -ItemType Directory -Path $stageWin, $stageArm64, $stageMipsle | Out-Null

Write-Host "[1/6] 构建 Windows 版本..."
Invoke-GoBuild -WorkDir $winBuildDir -EnvMap @{ GOOS = "windows"; GOARCH = "amd64" } -Output (Join-Path $stageWin "CampusNet.exe") -Ldflags "-s -w -H windowsgui"

Write-Host "[2/6] 拷贝 Windows 配置文件..."
Copy-Item (Join-Path $winBuildDir "account.ini") (Join-Path $stageWin "account.ini") -Force
if (Test-Path (Join-Path $winBuildDir "info.htm")) {
    Copy-Item (Join-Path $winBuildDir "info.htm") (Join-Path $stageWin "info.htm") -Force
}

Write-Host "[3/6] 构建 OpenWrt arm64 版本..."
Invoke-GoBuild -WorkDir $openwrtBuildDir -EnvMap @{ GOOS = "linux"; GOARCH = "arm64" } -Output (Join-Path $stageArm64 "srunlogin") -Ldflags "-s -w"
Copy-Item (Join-Path $openwrtBuildDir "account.ini") (Join-Path $stageArm64 "account.ini") -Force

Write-Host "[4/6] 构建 OpenWrt mipsle 版本..."
Invoke-GoBuild -WorkDir $openwrtBuildDir -EnvMap @{ GOOS = "linux"; GOARCH = "mipsle"; GOMIPS = "softfloat" } -Output (Join-Path $stageMipsle "srunlogin") -Ldflags "-s -w"
Copy-Item (Join-Path $openwrtBuildDir "account.ini") (Join-Path $stageMipsle "account.ini") -Force

$winZip = Join-Path $distDir ("CampusNet-windows-amd64-" + $Version + ".zip")
$arm64Tar = Join-Path $distDir ("srunlogin-openwrt-arm64-" + $Version + ".tar.gz")
$mipsleTar = Join-Path $distDir ("srunlogin-openwrt-mipsle-" + $Version + ".tar.gz")

Write-Host "[5/6] 打包 Windows ZIP..."
Compress-Archive -Path (Join-Path $stageWin "*") -DestinationPath $winZip -Force

Write-Host "[6/6] 打包 OpenWrt tar.gz..."
tar -czf $arm64Tar -C $stageArm64 .
tar -czf $mipsleTar -C $stageMipsle .

Remove-Item $stageWin, $stageArm64, $stageMipsle -Recurse -Force

Write-Host ""
Write-Host "Release 完成，产物目录: $distDir"
Get-ChildItem $distDir | Select-Object Name, Length
