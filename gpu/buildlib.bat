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
REM The copy in cmd\derostorm\ is the one go:embed picks up. Forgetting it is
REM how you end up shipping a stale kernel, so it happens here and not by hand.
REM
REM Run from the repository root:  gpu\buildlib.bat
call "C:\Program Files (x86)\Microsoft Visual Studio\18\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul
nvcc -O3 -arch=sm_120 -diag-suppress 177 -DDSG_BUILD_DLL -DBR_BLOCK=256 -cudart static ^
     -shared -o gpu\derostorm_gpu.dll gpu\derostorm_gpu.cu || exit /b 1
copy /y gpu\derostorm_gpu.dll cmd\derostorm\ >nul || exit /b 1
del gpu\derostorm_gpu.exp gpu\derostorm_gpu.lib >nul 2>&1
echo built gpu\derostorm_gpu.dll and copied it to cmd\derostorm\
