# DeroStorm build script (Windows / PowerShell).
#
#   .\build.ps1              build for this machine into .\bin
#   .\build.ps1 -All         cross-compile every supported platform
#   .\build.ps1 -SkipTests   skip the test run (not recommended, see below)
#
# Two non-obvious build flags are used, both deliberate:
#
#   -gcflags '...astrobwtv3=-B'
#       Disables bounds checks in the suffix-sort package only. That package is
#       ~90% of mining CPU time and its inner loops carry two or three bounds
#       checks each; measured +7.8% hashrate. It is only sound because the tests
#       prove no index ever goes out of range -- AstroBWTv3 swallows panics, so
#       an out-of-range access would silently produce a wrong hash rather than
#       crash. Do not skip the tests.
#
#   -pgo=auto
#       Profile-guided optimisation from cmd/derostorm/default.pgo.
#
#   -ldflags '-s -w'
#       Strips the symbol table and DWARF, which is purely a size saving. It
#       used to be load bearing, back when GPU support went through cgo and the
#       MinGW that ships with TDM-GCC 10.3.0 wrote debug sections Windows would
#       not load. Binding the CUDA library at run time instead removed cgo, and
#       with it that whole class of problem: no C toolchain is involved in a
#       build any more.

param(
    [switch]$All,
    [switch]$Native,
    [switch]$SkipTests
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$Version  = '1.1.1'
$Pkg      = './cmd/derostorm'
$BoundsPkg = 'github.com/deroproject/derohe/astrobwt/astrobwtv3'
$GcFlags  = "$BoundsPkg=-B"
$LdFlags  = '-s -w'
$OutDir   = Join-Path $PSScriptRoot 'bin'

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

# The embedded copies. Their presence is checked on every build, because a
# missing one is a build error from go:embed with no hint as to the cause.
$Embedded = @(
    @{ Path = 'cmd\derostorm\derostorm_gpu.dll'; Script = 'gpu\buildlib.bat'; What = 'CUDA kernels' }
    @{ Path = 'cmd\derostorm\derostorm_sa.dll';  Script = 'native\build.bat'; What = 'libsais suffix sort' }
)

if ($Native) {
    foreach ($lib in $Embedded) {
        Write-Host "building $($lib.What)" -ForegroundColor Cyan

        # $ErrorActionPreference is Stop for this script, which turns *anything*
        # a native command writes to stderr into a terminating error. vcvars64
        # writes a harmless "vswhere.exe is not recognized" line, so the exit
        # code is the only thing worth believing here.
        $prev = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        & cmd /c "$($lib.Script) 2>&1" | ForEach-Object { Write-Host "    $_" -ForegroundColor DarkGray }
        $code = $LASTEXITCODE
        $ErrorActionPreference = $prev

        if ($code -ne 0) { throw "$($lib.Script) failed with exit code $code" }
    }
}

foreach ($lib in $Embedded) {
    if (-not (Test-Path $lib.Path)) {
        throw "$($lib.Path) is missing - run $($lib.Script), or .\build.ps1 -Native"
    }
}

if (-not $SkipTests) {
    Write-Host 'running tests...' -ForegroundColor Cyan
    go test ./cmd/derostorm/
    if ($LASTEXITCODE -ne 0) { throw 'tests failed - not building' }
}

function Build-Target($goos, $goarch) {
    $name = "derostorm-$goos-$goarch"
    if ($goos -eq 'windows') { $name += '.exe' }
    $out = Join-Path $OutDir $name

    Write-Host "building $name" -ForegroundColor Cyan
    $env:GOOS = $goos
    $env:GOARCH = $goarch
    go build -trimpath -pgo=auto -gcflags="$GcFlags" -ldflags="$LdFlags" -o $out $Pkg
    $code = $LASTEXITCODE
    Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
    if ($code -ne 0) { throw "build failed for $goos/$goarch" }

    $kb = [math]::Round((Get-Item $out).Length / 1MB, 1)
    Write-Host "  -> $out ($kb MB)" -ForegroundColor Green
}

if ($All) {
    Build-Target 'windows' 'amd64'
    Build-Target 'linux'   'amd64'
    Build-Target 'linux'   'arm64'
    Build-Target 'darwin'  'amd64'
    Build-Target 'darwin'  'arm64'
} else {
    Build-Target (go env GOOS) (go env GOARCH)
}



Write-Host ''
foreach ($lib in $Embedded) {
    $kb = [math]::Round((Get-Item $lib.Path).Length / 1KB)
    Write-Host ("  embedded {0,-24} {1} KB" -f $lib.What, $kb) -ForegroundColor DarkGray
}
Write-Host ''
Write-Host "DeroStorm $Version built into $OutDir" -ForegroundColor Green
