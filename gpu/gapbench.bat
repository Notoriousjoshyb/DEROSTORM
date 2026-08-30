@echo off
REM Builds gpu\gapbench.exe, which measures how much of the wall clock the card
REM is actually working. See the comment at the top of gpu\gapbench.cu.
REM
REM -arch=native: this is a measurement of the card in this machine, so there is
REM nothing to gain from the other architectures.
REM
REM Run from the repository root:  gpu\gapbench.bat
call "C:\Program Files (x86)\Microsoft Visual Studio\18\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul
nvcc -O3 -arch=native -std=c++17 -diag-suppress 177 -DBR_BLOCK=256 -cudart static ^
     -o gpu\gapbench.exe gpu\gapbench.cu || exit /b 1
echo built gpu\gapbench.exe
