@echo off
REM Rebuilds the option B benchmark across the knobs that matter and runs each.
REM
REM   BR_BITS   bits per radix pass: fewer passes against longer same-digit runs
REM   SAD_SEED  bytes the first sort orders by, i.e. where the doubling starts
REM
REM Both trade one cost against another, so the best point is a measurement.
REM Run from the repository root:  gpu\sweep.bat
REM
REM -arch=native builds for the card in this machine. A sweep is a measurement
REM of this card, so there is nothing to gain from the other architectures and a
REM six-fold build time to lose. See gpu\buildlib.bat for the shipped library,
REM which does need all of them.
call "C:\Program Files (x86)\Microsoft Visual Studio\18\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul
for %%s in (1 2 4) do (
  nvcc -O3 -arch=native -diag-suppress 177 -DBR_BLOCK=1024 -DSAD_SEED=%%s ^
       -o gpu\sa_s%%s.exe gpu\sa_parallel_test.cu || exit /b 1
  echo built seed %%s
)
for %%b in (6 8) do (
  nvcc -O3 -arch=native -diag-suppress 177 -DBR_BLOCK=1024 -DBR_BITS=%%b ^
       -o gpu\sa_b%%b.exe gpu\sa_parallel_test.cu || exit /b 1
  echo built bits %%b
)
