@echo off
REM Builds the CUDA test binaries. nvcc needs the MSVC host compiler on PATH,
REM which vcvars64 provides. Run from the repository root:  gpu\build.bat
call "C:\Program Files (x86)\Microsoft Visual Studio\18\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul
if errorlevel 1 exit /b 1

REM native is the card in this machine. These are local test binaries, so that
REM is the right target: building the six architectures the shipped library
REM carries would cost six times the build for nothing. gpu\buildlib.bat is the
REM one that has to run on someone else's card.
set ARCH=native
REM BR_BLOCK is option B's threads per hash. 1024 measured fastest; the
REM sweep in gpu\sweep.bat rebuilds at other widths to check.
set FLAGS=-O3 -arch=%ARCH% -diag-suppress 177 -DBR_BLOCK=1024

if "%1"=="" goto all
nvcc %FLAGS% -o gpu\%1.exe gpu\%1.cu || exit /b 1
echo built gpu\%1.exe
exit /b 0

:all
for %%f in (hash_test sa_parallel_test hash_parallel_test) do (
  if exist gpu\%%f.cu (
    nvcc %FLAGS% -o gpu\%%f.exe gpu\%%f.cu || exit /b 1
    echo built gpu\%%f.exe
  )
)
exit /b 0
