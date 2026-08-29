# DeroStorm build script (Windows / PowerShell).
#
#   .\build.ps1              build for this machine into .\bin
#   .\build.ps1 -All         cross-compile every supported platform
#   .\build.ps1 -SkipTests   skip the test run (not recommended, see below)
#
# Two non-obvious build flags are used, both deliberate:
#
#   -gcflags '...astrobwtv3=-B'      AMD64 ONLY -- see below
#       Disables bounds checks in the suffix-sort package only. That package is
#       ~90% of mining CPU time and its inner loops carry two or three bounds
#       checks each; measured +7.8% hashrate. It is only sound because the tests
#       prove no index ever goes out of range -- AstroBWTv3 swallows panics, so
#       an out-of-range access would silently produce a wrong hash rather than
#       crash. Do not skip the tests.
#
#       It is passed on amd64 and nowhere else, because on arm64 it does not
#       produce a wrong hash, it produces a crash:
#
#         unexpected fault address 0x7b681b88b333
#         fatal error: fault
#
#       Reported from a Mac (darwin/arm64) and reproduced on linux/arm64 under
#       qemu, in the miner and in a bare hashing loop. The same loop is clean on
#       amd64 with -B, and clean on arm64 *without* it -- 16,000 hashes each way,
#       astrobwtv3.RecoveredPanics zero on both, so this is not an out-of-range
#       index the checks were hiding. The algorithm is sound on arm64; what is
#       not sound is turning the checks off there.
#
#       This is the failure that flag was always going to have: it was licensed
#       by a test suite that only ever ran on the machine doing the building, and
#       the four cross-compiled targets inherited a guarantee nothing had checked
#       for them. amd64 keeps it because amd64 is what the tests run on.
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
    [switch]$SkipTests,
    [switch]$Clean
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

$Version  = '1.4.0'
$Pkg      = './cmd/derostorm'
$BoundsPkg = 'github.com/deroproject/derohe/astrobwt/astrobwtv3'
$LdFlags  = '-s -w'

# -B is passed on amd64 and nowhere else. See boundsFlags below.
function Get-GcFlags($goarch) {
    if ($goarch -eq 'amd64') { return "$BoundsPkg=-B" }
    return ''
}
$OutDir   = Join-Path $PSScriptRoot 'bin'

# -Clean removes build output and the disposable measurement binaries, then
# exits. It is a separate switch rather than something a build does on its own,
# because a build that quietly deletes things is a build nobody trusts.
#
# What it never touches: the three embedded libraries under cmd\derostorm. They
# are inputs to the build, not outputs of it -- go:embed fails without them and
# rebuilding them needs nvcc, MSVC and WSL. gpuectors.bin is left for the same
# reason: the GPU tests read it.
function Invoke-Clean {
    # bin is emptied of build output, not removed. It also holds derostorm.json,
    # which carries the wallet address the setup wizard asked for, and deleting
    # a user's config as part of "clean the build" is the kind of helpfulness
    # nobody asks for twice.
    $targets = @(
        'bench'
        'derostorm.exe'
        'cmd\derostorm\derostorm.exe'
    )
    $patterns = @(
        'bin\derostorm-*'
        'bin\*.dll'
        'bin\*.log'
        'native\sabench*.exe'
        'native\saprof*.exe'
        'native\sapro_*.exe'
        'native\shabench*.exe'
        'gpu\*_test*.exe'
        'gpu\desc_test*.exe'
        'gpu\hash*.exe'
        'gpu\prof\prof.exe'
        'gpu\*.exp'
        'gpu\*.lib'
        'native\*.exp'
        'native\*.lib'
        'native\*.obj'
    )

    $freed = 0
    foreach ($t in $targets + ($patterns | ForEach-Object { Get-Item $_ -EA SilentlyContinue })) {
        $path = if ($t -is [string]) { $t } else { $t.FullName }
        if (-not (Test-Path $path)) { continue }
        $bytes = (Get-ChildItem $path -Recurse -File -EA SilentlyContinue |
                  Measure-Object -Property Length -Sum).Sum
        if (-not $bytes) { $bytes = (Get-Item $path).Length }
        Remove-Item $path -Recurse -Force -EA SilentlyContinue
        if (-not (Test-Path $path)) {
            $freed += $bytes
            Write-Host "  removed $path" -ForegroundColor DarkGray
        }
    }
    Write-Host ''
    Write-Host ("cleaned {0:N0} MB" -f ($freed / 1MB)) -ForegroundColor Green
}

if ($Clean) { Invoke-Clean; return }

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

# The embedded copies. Their presence is checked on every build, because a
# missing one is a build error from go:embed with no hint as to the cause.
#
# The Linux CUDA library is the odd one out: nvcc targets the host it runs on,
# so a .so cannot come from this toolchain and is built under WSL instead. That
# is a build-time dependency only -- once the file exists, embedding it in a
# GOOS=linux cross-build is no different from embedding the DLL, which is the
# reason the GPU binding avoids cgo (see cmd/derostorm/gpu_cuda.go).
$Embedded = @(
    @{ Path = 'cmd\derostorm\derostorm_gpu.dll';   Script = 'gpu\buildlib.bat'; What = 'CUDA kernels (Windows)' }
    @{ Path = 'cmd\derostorm\libderostorm_gpu.so'; Script = 'gpu/buildlib.sh';  What = 'CUDA kernels (Linux)'; Wsl = $true }
    @{ Path = 'cmd\derostorm\derostorm_sa.dll';    Script = 'native\build.bat'; What = 'libsais suffix sort' }
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
        if ($lib.Wsl) {
            $repo = (& wsl.exe wslpath -a "$PSScriptRoot") -replace "`0", ''
            & wsl.exe -- bash -lc "cd '$repo' && sh $($lib.Script) 2>&1" |
                ForEach-Object { Write-Host "    $($_ -replace "`0", '')" -ForegroundColor DarkGray }
        } else {
            & cmd /c "$($lib.Script) 2>&1" | ForEach-Object { Write-Host "    $_" -ForegroundColor DarkGray }
        }
        $code = $LASTEXITCODE
        $ErrorActionPreference = $prev

        if ($code -ne 0) { throw "$($lib.Script) failed with exit code $code" }
    }
}

foreach ($lib in $Embedded) {
    if (-not (Test-Path $lib.Path)) {
        $how = if ($lib.Wsl) { "run it under WSL with a Linux CUDA toolkit, or .\build.ps1 -Native" }
               else          { "run $($lib.Script), or .\build.ps1 -Native" }
        throw "$($lib.Path) is missing - $how"
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
    $gc = Get-GcFlags $goarch
    if ($gc) {
        go build -trimpath -pgo=auto -gcflags="$gc" -ldflags="$LdFlags" -o $out $Pkg
    } else {
        go build -trimpath -pgo=auto -ldflags="$LdFlags" -o $out $Pkg
    }
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
