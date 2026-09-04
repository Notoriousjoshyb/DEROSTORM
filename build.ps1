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

$Version  = '1.8.7'
$Pkg      = './cmd/derostorm'
$BoundsPkg = 'github.com/deroproject/derohe/astrobwt/astrobwtv3'
$LdFlags  = '-s -w'

# -B is passed on amd64 and nowhere else. See boundsFlags below.
function Get-GcFlags($goarch) {
    if ($goarch -eq 'amd64') {
        return @(
            "$BoundsPkg=-B"
            'github.com/notoriousjoshyb/derostorm/internal/dsa=-B'
        )
    }
    return @()
}
$OutDir   = Join-Path $PSScriptRoot 'bin'

# -Clean removes build output and the disposable measurement binaries, then
# exits. It is a separate switch rather than something a build does on its own,
# because a build that quietly deletes things is a build nobody trusts.
#
# What it never touches: the four embedded libraries under cmd\derostorm. They
# are inputs to the build, not outputs of it -- go:embed fails without them and
# rebuilding them needs nvcc, MSVC and WSL. gpu\vectors.bin is left for the same
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
        'native\sab_*.exe'
        'native\shabench*.exe'
        '*.obj'
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

# The embedded copies. The suffix-sort pair below is required on every build,
# because a missing one is a build error from go:embed with no hint as to the
# cause -- and required is fair for them, since they need only MSVC and gcc,
# which an AMD-only rig has as well.
#
# The two Linux libraries are the odd ones out: a compiler targets the host it
# runs on, so neither .so can come from this toolchain and both are built under
# WSL instead. That is a build-time dependency only -- once the file exists,
# embedding it in a GOOS=linux cross-build is no different from embedding the
# DLL, which is the reason these bindings avoid cgo (see
# cmd/derostorm/gpu_backend.go).
#
# Sources is what each one is compiled from, and it is there for the staleness
# check below rather than for the build. It lists only what the library itself
# pulls in -- gpu\derostorm_gpu.cu and the headers it includes, not the test
# harnesses that sit beside them in the same directory.
$GpuSources = @('gpu\derostorm_gpu.cu', 'gpu\derostorm_gpu.h', 'gpu\*.cuh', 'gpu\*.inc')
$SaSources  = @('native\derostorm_sa.c', 'native\descriptor.c', 'native\descriptor.h',
                'native\sha256ni.c', 'native\sha256ni.h', 'native\sha256arm.c',
                'native\libsais\libsais.c', 'native\libsais\libsais.h')

$Embedded = @(
    @{ Path = 'cmd\derostorm\derostorm_sa.dll';    Goos = 'windows'; Script = 'native\build.bat';   What = 'descriptor suffix sort (Windows)'; Sources = $SaSources }
    @{ Path = 'cmd\derostorm\libderostorm_sa.so';  Goos = 'linux'; Script = 'native/buildlib.sh'; What = 'descriptor suffix sort (Linux)'; Wsl = $true; Needs = 'gcc'; Sources = $SaSources }
)

# The GPU kernels, which are optional in a way the two above are not.
#
# The CUDA pair used to be required, and that is what kept an AMD-only rig from
# building at all: nvcc does not exist there, so the library could never be
# produced and go:embed failed on the missing file. Now both vendors work the
# same way. go:embed takes the whole cmd\derostorm\gpucuda\<goos> and
# cmd\derostorm\gpulib\<goos> directories rather than the files, so a tree
# without them still compiles and the finished miner simply reports no devices
# for that vendor. See the long note in cmd\derostorm\gpu_backend.go.
#
# So missing is a build configuration and not an error. Stale is still an error,
# and a worse one than for the suffix sort: nobody on this side may have the
# card to notice with.
#
# Linux has one per ROCm generation, and that is not a mistake. A HIP library
# links to the ROCm runtime by soname -- libamdhip64.so.7, .so.6 or .so.5 --
# and a rig has one of those installed, not three. No single build satisfies
# each, so one is built per generation and dlopen picks at run time. Only ROCm 6
# and up can target RDNA 4, so a machine with only the ROCm 5 toolchain produces
# a library that is correct and narrower. Windows has one because Windows has
# only ever had the ROCm 6 line.
#
# ROCm 7 is on the list because leaving it off is what shipped 1.7.2 with no AMD
# support on a ROCm 7 rig: buildlib_hip.sh already names its output after the
# runtime it linked against, so such a build produced libderostorm_hip7.so and
# nothing looked for it.
#
# Each generation needs its own toolchain and one machine rarely has more than
# one, so -Native runs buildlib_hip.sh once per script -- for whichever ROCm is
# default there. DSG_HIPCC is how a machine with several builds the rest; see
# gpu/buildlib_hip.sh.
$Optional = @(
    @{ Path = 'cmd\derostorm\gpucuda\windows\derostorm_gpu.dll';   Goos = 'windows'; Script = 'gpu\buildlib.bat';     What = 'CUDA kernels (Windows)'; Needs = 'an NVIDIA CUDA toolkit'; Sources = $GpuSources }
    @{ Path = 'cmd\derostorm\gpucuda\linux\libderostorm_gpu.so';   Goos = 'linux'; Script = 'gpu/buildlib.sh';      What = 'CUDA kernels (Linux)';   Wsl = $true; Needs = 'a Linux CUDA toolkit'; Sources = $GpuSources }
    @{ Path = 'cmd\derostorm\gpulib\windows\derostorm_hip.dll';  Goos = 'windows'; Script = 'gpu\buildlib_hip.bat'; What = 'HIP kernels (Windows)'; Needs = 'the AMD HIP SDK for Windows'; Sources = $GpuSources }
    @{ Path = 'cmd\derostorm\gpulib\linux\libderostorm_hip7.so'; Goos = 'linux'; Script = 'gpu/buildlib_hip.sh';  What = 'HIP kernels (Linux, ROCm 7)'; Wsl = $true; Needs = 'ROCm 7'; Sources = $GpuSources }
    @{ Path = 'cmd\derostorm\gpulib\linux\libderostorm_hip6.so'; Goos = 'linux'; Script = 'gpu/buildlib_hip.sh';  What = 'HIP kernels (Linux, ROCm 6)'; Wsl = $true; Needs = 'ROCm 6'; Sources = $GpuSources }
    @{ Path = 'cmd\derostorm\gpulib\linux\libderostorm_hip5.so'; Goos = 'linux'; Script = 'gpu/buildlib_hip.sh';  What = 'HIP kernels (Linux, ROCm 5)'; Wsl = $true; Needs = 'ROCm 5'; Sources = $GpuSources }
)

# The targets this run will actually build, and therefore the only libraries
# worth having an opinion about. Without -All that is the host alone.
#
# go:embed decides which it needs by build tag and not by the host, so a
# windows/amd64 build embeds the Windows suffix sort and never looks at the
# Linux one. Checking both regardless is what left a Windows machine with no
# WSL unable to build at all: it got past derostorm_sa.dll and was then asked
# for a Linux .so it had no toolchain for and no build of its own needed.
# build.sh has always checked per target; this is the same rule.
$TargetOS = if ($All) { @('windows', 'linux', 'darwin') } else { @(go env GOOS) }
$Needed   = @($Embedded | Where-Object { $TargetOS -contains $_.Goos })
$Watched  = @($Optional | Where-Object { $TargetOS -contains $_.Goos })

# The AMD HIP SDK root, or $null. HIP_PATH wins; otherwise the
# highest-numbered version directory under the ROCm root, so a 7.2 install is
# found without this file being edited for every ROCm release.
function Get-HipPath {
    $roots = @()
    if ($env:HIP_PATH) { $roots += $env:HIP_PATH }
    $base = 'C:\Program Files\AMD\ROCm'
    if (Test-Path $base) {
        $roots += @(Get-ChildItem -Path $base -Directory -ErrorAction SilentlyContinue |
                    Sort-Object { try { [version]$_.Name } catch { [version]'0.0' } } -Descending |
                    ForEach-Object { $_.FullName })
    }
    foreach ($r in $roots) {
        if (Test-Path (Join-Path $r 'bin\clang++.exe')) { return $r }
        if (Test-Path (Join-Path $r 'bin\hipcc.exe'))   { return $r }
    }
    return $null
}

if ($Native) {
    foreach ($lib in $Needed) {
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

    # The AMD kernels, only where a toolchain to build them exists. A machine
    # without ROCm is the normal case and not a failure: the miner builds and
    # mines without them on everything except AMD, so this says so and moves on.
    # Grouped by script: the three Linux entries are three possible outputs of
    # one build, not three builds. Running it once per entry built the same
    # library three times over.
    foreach ($group in $Optional | Group-Object Script) {
        $lib = $group.Group[0]
        # Each GPU library needs its own toolchain, and an AMD-only rig has
        # only some of them: probe for the right one before building, and skip
        # what cannot be built here. A skip is a narrower miner, not a failure.
        $have = switch -Wildcard ($lib.Script) {
            '*hip*' {
                if ($lib.Wsl) {
                    # amdclang++ as well as hipcc: ROCm 6 deprecated the hipcc wrapper
                    # and ROCm 7 on Arch ships without it, so a hipcc-only test skips
                    # the AMD kernels on the machines that build the newest ones.
                    & wsl.exe -- bash -lc '[ -n "$DSG_HIPCC" ] || [ -x /opt/rocm/bin/amdclang++ ] || command -v hipcc >/dev/null 2>&1 || [ -x /opt/rocm/bin/hipcc ]' 2>$null
                    $LASTEXITCODE -eq 0
                } else {
                    # Discover the SDK the way gpuuildlib_hip.bat already does -- the
                    # newest version directory under the ROCm root -- rather than naming
                    # one. A hardcoded 6.2 here is what made a ROCm 7.2 machine report
                    # "no the AMD HIP SDK for Windows" and build a CPU-only miner, while
                    # the batch file beside it would have compiled fine.
                    #
                    # It probes clang++ because that is what buildlib_hip.bat invokes:
                    # ROCm 6 deprecated the hipcc wrapper, so testing for hipcc asks a
                    # question the build does not care about.
                    $null -ne (Get-HipPath)
                }
            }
            default {
                if ($lib.Wsl) {
                    & wsl.exe -- bash -lc 'command -v nvcc >/dev/null 2>&1 || [ -x /usr/local/cuda/bin/nvcc ]' 2>$null
                    $LASTEXITCODE -eq 0
                } else {
                    $null -ne (Get-Command nvcc -ErrorAction SilentlyContinue)
                }
            }
        }
        if (-not $have) {
            Write-Host "skipping $($lib.What) - no $($lib.Needs) here" -ForegroundColor DarkYellow
            continue
        }
        Write-Host "building $($lib.What)" -ForegroundColor Cyan
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

# go:embed takes the whole gpucuda\<goos> and gpulib\<goos> directory, not the
# one file the loader asks for. That is deliberate -- it is what lets a tree
# with no GPU library still compile -- and it means anything else dropped in
# there is embedded too.
#
# Which is a 2 GB linker crash, not a big binary. Copying the ROCm bin
# directory into gpulib\windows to "provide the runtime" is an easy mistake
# (the runtime is not embedded; it comes from the Adrenalin driver), and
# ROCm's rocblas.dll alone is over a gigabyte. Go's linker then dies with
#
#     too much data, last section SRODATA (2186756880, over 2e+09 bytes)
#     pc-relative relocation ... address ... is too big
#
# which says nothing about the cause. So the strays are named here instead.
$EmbedDirs = @(
    @{ Dir = 'cmd\derostorm\gpucuda\windows'; Keep = 'derostorm_gpu.dll' }
    @{ Dir = 'cmd\derostorm\gpucuda\linux';   Keep = 'libderostorm_gpu.so' }
    @{ Dir = 'cmd\derostorm\gpulib\windows';  Keep = 'derostorm_hip.dll' }
    @{ Dir = 'cmd\derostorm\gpulib\linux';    Keep = 'libderostorm_hip7.so|libderostorm_hip6.so|libderostorm_hip5.so' }
)
foreach ($e in $EmbedDirs) {
    if (-not (Test-Path $e.Dir)) { continue }
    $keep = @($e.Keep -split '\|') + @('README.md')
    $stray = @(Get-ChildItem -Path $e.Dir -File -ErrorAction SilentlyContinue |
               Where-Object { $keep -notcontains $_.Name })
    if ($stray) {
        $mb = [math]::Round((($stray | Measure-Object -Property Length -Sum).Sum) / 1MB, 1)
        $names = ($stray | Select-Object -First 6 | ForEach-Object { $_.Name }) -join ', '
        throw ("$($e.Dir) holds $($stray.Count) file(s) that are not the GPU library " +
               "($mb MB: $names) - go:embed takes the whole directory, so these would be " +
               "built into the binary. Delete them; only $($e.Keep -replace '\|', ' or ') belongs here.")
    }
}

foreach ($lib in $Needed) {
    if (-not (Test-Path $lib.Path)) {
        $how = if ($lib.Wsl) { "run $($lib.Script) under WSL with $($lib.Needs), or .\build.ps1 -Native" }
               else          { "run $($lib.Script), or .\build.ps1 -Native" }
        throw "$($lib.Path) is missing - $how"
    }
}

# Said once, so a release cut without GPU support for a vendor is a choice and
# not something a miner discovers three days later.
$MissingOptional = @($Watched | Where-Object { -not (Test-Path $_.Path) })
if ($MissingOptional) {
    $names = ($MissingOptional | ForEach-Object { $_.What }) -join ', '
    Write-Host "not in this build - $names not present; those cards mine on the CPU" -ForegroundColor DarkYellow
}

# Present is not the same as current, and the difference has already shipped
# once. The 1.4.0 Linux archives carried the *previous* CUDA kernels: nvcc
# cannot build a .so under Windows, the machine cutting the release had no
# Linux toolchain, and nothing here objected because the stale file was sitting
# right where go:embed wanted it. Linux mined at 1.3.0 speed for a whole release
# while Windows had everything, and 1.4.1 exists only to undo it.
#
# So a library older than any source it is compiled from stops the build. It is
# a timestamp comparison and it will occasionally fire on a file that changed
# nothing -- editing gpu\prof.cuh, which compiles out of the shipped kernel, is
# the usual one. Rebuilding is cheap and shipping the wrong kernels is not.
foreach ($lib in ($Needed + ($Watched | Where-Object { Test-Path $_.Path }))) {
    $built = (Get-Item $lib.Path).LastWriteTimeUtc
    $newer = Get-ChildItem -Path $lib.Sources -File -ErrorAction SilentlyContinue |
             Where-Object { $_.LastWriteTimeUtc -gt $built } |
             Sort-Object LastWriteTimeUtc -Descending
    if ($newer) {
        $how = if ($lib.Wsl) { "run $($lib.Script) under WSL with $($lib.Needs), or .\build.ps1 -Native" }
               else          { "run $($lib.Script), or .\build.ps1 -Native" }
        $names = ($newer | Select-Object -First 3 | ForEach-Object { $_.Name }) -join ', '
        throw ("$($lib.Path) is older than the source it is built from " +
               "($names) - $how")
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
    $hostOS = go env GOOS
    $hostArch = go env GOARCH
    if ($goos -ne $hostOS -or $goarch -ne $hostArch) {
        $env:CGO_ENABLED = '0'
    }
    $gc = Get-GcFlags $goarch
    $args = @('-trimpath', '-pgo=auto', "-ldflags=$LdFlags", '-o', $out, $Pkg)
    foreach ($g in $gc) {
        $args = @("-gcflags=$g") + $args
    }
    go build @args
    $code = $LASTEXITCODE
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
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
foreach ($lib in ($Needed + ($Watched | Where-Object { Test-Path $_.Path }))) {
    $kb = [math]::Round((Get-Item $lib.Path).Length / 1KB)
    Write-Host ("  embedded {0,-24} {1} KB" -f $lib.What, $kb) -ForegroundColor DarkGray
}
Write-Host ''
Write-Host "DeroStorm $Version built into $OutDir" -ForegroundColor Green
