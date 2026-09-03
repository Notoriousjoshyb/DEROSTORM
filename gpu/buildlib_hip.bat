@echo off
setlocal enabledelayedexpansion
REM Builds the AMD library the miner embeds, for Windows x86-64.
REM
REM The Windows twin of gpu\buildlib_hip.sh. Same source, same architectures;
REM what differs is the object format, a .dll bound with LoadLibrary instead of
REM a .so bound with dlopen.
REM
REM Needs the AMD "HIP SDK for Windows", which is a separate download from the
REM display driver. The driver alone is enough to *run* the finished miner --
REM the DLL this produces imports amdhip64_6.dll, which Adrenalin ships -- but
REM not to build it. Nothing here needs an AMD card.
REM
REM The compiler is clang++ from the SDK rather than the hipcc wrapper beside
REM it: ROCm 6 deprecated hipcc, and clang++ takes --rocm-path so the whole
REM toolchain can live anywhere, including a folder extracted from the
REM installer rather than a system install.
REM
REM Set HIP_PATH if the SDK is somewhere other than the default.
REM
REM Run from the repository root:  gpu\buildlib_hip.bat

if not "%HIP_PATH%"=="" goto :havepath
for /d %%d in ("C:\Program Files\AMD\ROCm\*") do set HIP_PATH=%%d
:havepath
if "%HIP_PATH%"=="" (
  echo cannot find the AMD HIP SDK under C:\Program Files\AMD\ROCm
  echo install the AMD HIP SDK for Windows, or set HIP_PATH to where it lives
  exit /b 1
)
set CLANG=%HIP_PATH%\bin\clang++.exe
if not exist "%CLANG%" (
  echo no clang++ at %CLANG%
  echo HIP_PATH does not look like a HIP SDK
  exit /b 1
)

REM One code object per architecture. See the long note in gpu\buildlib_hip.sh:
REM AMD has no PTX equivalent, so a card whose gfx target is missing here does
REM not JIT -- it fails to launch and mines on the CPU. Keep the two lists in
REM step.
REM
REM   gfx1010 .. gfx1013 RDNA 1    RX 5700 XT, RX 5500 XT
REM   gfx1030 .. gfx1036 RDNA 2    RX 6900/6800/6700/6600
REM   gfx1100 .. gfx1103 RDNA 3    RX 7900/7800/7700/7600
REM   gfx1150, gfx1151   RDNA 3.5  Strix Point APUs
REM   gfx1200, gfx1201   RDNA 4    RX 9070
REM
REM Override the list outright with:  set DSG_HIP_ARCHS=gfx1030 gfx1100
if "%DSG_HIP_ARCHS%"=="" set DSG_HIP_ARCHS=gfx1010 gfx1011 gfx1012 gfx1013 gfx1030 gfx1031 gfx1032 gfx1033 gfx1034 gfx1035 gfx1036 gfx1100 gfx1101 gfx1102 gfx1103 gfx1150 gfx1151 gfx1200 gfx1201

REM Each target is offered to the compiler before it is used, because an older
REM SDK rejects a target it has never heard of and that stops the whole build.
REM A narrower library is a fine outcome; a failed build over one unknown name
REM is not. What gets skipped is printed -- a silently narrower library is
REM exactly the kind of thing that ships and then fails on somebody else's card.
REM An empty file is enough: an unknown target ID is rejected by the driver
REM before it looks at any source.
set PROBE=%TEMP%\dsg_probe_%RANDOM%.hip
type nul > "%PROBE%"
set OFFLOAD=
set USED=
set SKIPPED=
for %%a in (%DSG_HIP_ARCHS%) do (
  "%CLANG%" -x hip --rocm-path="%HIP_PATH%" --offload-arch=%%a -c -o "%PROBE%.o" "%PROBE%" >nul 2>&1
  if errorlevel 1 (
    set SKIPPED=!SKIPPED! %%a
  ) else (
    set OFFLOAD=!OFFLOAD! --offload-arch=%%a
    set USED=!USED! %%a
  )
)
del "%PROBE%" "%PROBE%.o" >nul 2>&1

if "%USED%"=="" (
  echo %CLANG% accepts none of the gfx targets -- is this a HIP SDK?
  exit /b 1
)
if not "%SKIPPED%"=="" (
  echo note: this SDK cannot target!SKIPPED!
  echo       those cards will mine on the CPU; a newer HIP SDK builds them.
)
echo compiler: %CLANG%
echo targets: !USED!

REM -x hip so a .cu extension is read as HIP rather than guessed at, and
REM -mno-wavefrontsize64 to pin wave32 on every RDNA target. RDNA defaults to
REM wave32 already; pinning it means a driver or compiler default that changes
REM under us is a compile error and not a silent miscompile.
REM
REM -Wno-deprecated-pragma silences one warning per translation unit about
REM __AMDGCN_WAVEFRONT_SIZE__ being on its way out. gpuapi.cuh reads it on
REM purpose, to turn a wave64 build into a compile error, and there is no
REM replacement for that yet.
if not exist cmd\derostorm\gpulib\windows mkdir cmd\derostorm\gpulib\windows
"%CLANG%" -O3 -x hip --rocm-path="%HIP_PATH%" !OFFLOAD! ^
    -mno-wavefrontsize64 -Wno-deprecated-pragma ^
    -DDSG_BUILD_DLL -DBR_BLOCK=256 ^
    -shared -o gpu\derostorm_hip.dll gpu\derostorm_gpu.cu ^
    -L"%HIP_PATH%\lib" -lamdhip64 || exit /b 1

copy /y gpu\derostorm_hip.dll cmd\derostorm\gpulib\windows\ >nul || exit /b 1
del gpu\derostorm_hip.exp gpu\derostorm_hip.lib >nul 2>&1
echo built gpu\derostorm_hip.dll and copied it to cmd\derostorm\gpulib\windows\
