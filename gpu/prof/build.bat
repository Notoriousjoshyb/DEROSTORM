@echo off
REM Builds the cycle-attribution harness.
REM
REM It compiles the *real* headers with DS_PROF defined, rather than keeping
REM instrumented copies of them. The copies it used to keep went stale the
REM moment the originals changed, which is exactly when a profile is wanted.
REM With DS_PROF undefined every mark is empty and the shipped kernel is
REM byte-identical, so this costs the miner nothing.
REM
REM Run from the repository root:  gpu\prof\build.bat
REM
REM -arch=native builds for the card in this machine; a profile is of that card
REM anyway. gpu\buildlib.bat is where the multi-architecture build lives.
call "C:\Program Files (x86)\Microsoft Visual Studio\18\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul
nvcc -O3 -arch=native -diag-suppress 177 -DBR_BLOCK=256 -DDS_PROF ^
     -o gpu\prof\prof.exe gpu\prof\prof.cu || exit /b 1
echo built gpu\prof\prof.exe -- run: gpu\prof\prof.exe gpu\vectors.bin
