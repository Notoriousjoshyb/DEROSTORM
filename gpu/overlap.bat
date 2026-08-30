@echo off
call "C:\Program Files (x86)\Microsoft Visual Studio\18\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul
nvcc -O3 -arch=native -std=c++17 -diag-suppress 177 -DBR_BLOCK=256 -o gpu\overlap.exe gpu\overlap.cu || exit /b 1
echo built gpu\overlap.exe
