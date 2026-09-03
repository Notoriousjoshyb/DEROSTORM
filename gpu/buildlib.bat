@echo off
REM Builds the CUDA library the miner embeds.
REM
REM It is a DLL, not a static library, and it is bound with LoadLibrary rather
REM than linked: that keeps cgo out of the build entirely. cgo hands the final
REM link to MinGW, and the MinGW that ships with TDM-GCC 10.3.0 writes debug
REM sections Windows refuses to load, so every cgo build died with "This app
REM can't run on your PC" before reaching main(). -cudart static folds the CUDA
REM runtime in, so the finished miner needs only the display driver.
REM
REM The copy in cmd\derostorm\gpucuda\windows\ is the one go:embed picks up.
REM Forgetting it is how you end up shipping a stale kernel, so it happens here
REM and not by hand.
REM
REM Run from the repository root:  gpu\buildlib.bat
call "C:\Program Files (x86)\Microsoft Visual Studio\18\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul

REM One cubin per architecture, all in the one DLL (a "fat binary"). Building
REM for the local card alone is what makes dsg_init fail on someone else's with
REM "no kernel image is available for execution": the driver will not run a
REM cubin built for a different compute capability, and it will not lower one
REM either. The list is every consumer and datacentre part CUDA 13 still
REM supports -- 13 dropped Pascal and Volta, so sm_75 (Turing, RTX 20xx) is the
REM floor. compute_120 PTX rides along last so a card newer than this toolkit
REM JITs from it at load instead of failing.
REM
REM   sm_75  RTX 20xx, GTX 16xx, T4
REM   sm_80  A100
REM   sm_86  RTX 30xx, A40
REM   sm_89  RTX 40xx, L4, L40
REM   sm_90  H100, H200
REM   sm_120 RTX 50xx
REM
REM Each arch is a separate compile, so this takes roughly six times as long as
REM a single-arch build and the DLL grows by about a cubin each. Both are worth
REM it: the alternative is a miner that only runs on the machine that built it.
set GENCODE=-gencode arch=compute_75,code=sm_75 ^
 -gencode arch=compute_80,code=sm_80 ^
 -gencode arch=compute_86,code=sm_86 ^
 -gencode arch=compute_89,code=sm_89 ^
 -gencode arch=compute_90,code=sm_90 ^
 -gencode arch=compute_120,code=sm_120 ^
 -gencode arch=compute_120,code=compute_120

nvcc -O3 %GENCODE% -diag-suppress 177 -DDSG_BUILD_DLL -DBR_BLOCK=256 -cudart static ^
     -shared -o gpu\derostorm_gpu.dll gpu\derostorm_gpu.cu || exit /b 1
if not exist cmd\derostorm\gpucuda\windows mkdir cmd\derostorm\gpucuda\windows
copy /y gpu\derostorm_gpu.dll cmd\derostorm\gpucuda\windows\ >nul || exit /b 1
del gpu\derostorm_gpu.exp gpu\derostorm_gpu.lib >nul 2>&1
echo built gpu\derostorm_gpu.dll and copied it to cmd\derostorm\gpucuda\windows\
