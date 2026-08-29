@echo off
REM Builds native\saprof%1.exe with the descriptor's phase timers on.
REM   %1 name suffix   %2 DSA_RUN_MAX   %3 DSA_RUN_SPLIT   %4 DSA_COUNT_MIN
call "C:\Program Files (x86)\Microsoft Visual Studio\18\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul
set D=
if not "%2"=="" set D=/DDSA_RUN_MAX=%2
if not "%3"=="" set D=%D% /DDSA_RUN_SPLIT=%3
if not "%4"=="" set D=%D% /DDSA_COUNT_MIN=%4
cl /nologo /O2 /Ob3 /Oi /GS- /GL /arch:AVX2 /DDSA_PROF %D% /I native ^
   native\sabench.c native\descriptor.c native\libsais\libsais.c ^
   /Fo:native\ /Fe:native\saprof%1.exe /link /RELEASE /LTCG >nul || exit /b 1
del native\*.obj >nul 2>&1
