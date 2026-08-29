@echo off
REM Builds the descriptor-vs-doubling comparison. Run from the repository root:
REM   gpu\desc_test_build.bat
REM   gpu\desc_test.exe gpu\vectors.bin
REM
REM -arch=native builds for the card in this machine, which is all a local
REM measurement needs. The shipped library is the one that has to run anywhere,
REM and gpu\buildlib.bat gives it a cubin per architecture for that reason.
call "C:\Program Files (x86)\Microsoft Visual Studio\18\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul
nvcc -O3 -arch=native -diag-suppress 177 -DBR_BLOCK=1024 ^
     -o gpu\desc_test.exe gpu\desc_test.cu || exit /b 1
echo built gpu\desc_test.exe
