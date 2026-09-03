@echo off
REM Builds the AMD library the miner embeds, for Windows x86-64.
REM
REM The Windows twin of gpu\buildlib_hip.sh. Same source, same architectures;
REM what differs is the object format, a .dll bound with LoadLibrary instead of
REM a .so bound with dlopen.
REM
REM Needs the AMD "HIP SDK for Windows", which is a separate download from the
REM display driver. The driver alone is enough to *run* the finished miner --
REM amdhip64_6.dll ships inside it -- but not to build this.
REM
REM Set HIP_PATH if the SDK is somewhere other than the default.
REM
REM Run from the repository root:  gpu\buildlib_hip.bat

if "%HIP_PATH%"=="" set HIP_PATH=C:\Program Files\AMD\ROCm\6.2
set HIPCC="%HIP_PATH%\bin\hipcc.exe"
if not exist %HIPCC% (
  echo cannot find hipcc at %HIPCC%
  echo install the AMD HIP SDK for Windows, or set HIP_PATH to where it lives
  exit /b 1
)

REM One code object per architecture. See the long note in gpu\buildlib_hip.sh:
REM AMD has no PTX equivalent, so a card whose gfx target is missing here does
REM not JIT -- it fails to launch and mines on the CPU. Keep the two lists in
REM step.
REM
REM   gfx1010, gfx1012   RDNA 1   RX 5700 XT, RX 5500 XT
REM   gfx1030 .. gfx1036 RDNA 2   RX 6900/6800/6700/6600
REM   gfx1100 .. gfx1103 RDNA 3   RX 7900/7800/7700/7600
REM   gfx1200, gfx1201   RDNA 4   RX 9070
REM
REM Override for an older SDK that does not know the newest targets:
REM   set DSG_HIP_ARCHS=gfx1030 gfx1100
if "%DSG_HIP_ARCHS%"=="" set DSG_HIP_ARCHS=gfx1010 gfx1012 gfx1030 gfx1031 gfx1032 gfx1034 gfx1035 gfx1036 gfx1100 gfx1101 gfx1102 gfx1103 gfx1200 gfx1201

set OFFLOAD=
for %%a in (%DSG_HIP_ARCHS%) do call set OFFLOAD=%%OFFLOAD%% --offload-arch=%%a

REM -x hip so a .cu extension is read as HIP rather than guessed at, and
REM -mno-wavefrontsize64 to pin wave32 on every RDNA target.
%HIPCC% -O3 -x hip %OFFLOAD% -mno-wavefrontsize64 ^
        -DDSG_BUILD_DLL -DBR_BLOCK=256 ^
        -shared -o gpu\derostorm_hip.dll gpu\derostorm_gpu.cu || exit /b 1

if not exist cmd\derostorm\gpulib mkdir cmd\derostorm\gpulib
copy /y gpu\derostorm_hip.dll cmd\derostorm\gpulib\ >nul || exit /b 1
del gpu\derostorm_hip.exp gpu\derostorm_hip.lib >nul 2>&1
echo built gpu\derostorm_hip.dll and copied it to cmd\derostorm\gpulib\
