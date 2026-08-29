@echo off
REM Builds native\sabench.exe, which checks and times the two suffix sorts
REM against the real texts. Run from the repository root.
REM
REM   native\benchbuild.bat            production settings
REM   native\benchbuild.bat _r8 8      blocks per descriptor run = 8, named _r8
REM
REM %1 is appended to the output name; %2 sets DSA_RUN_MAX and %3 DSA_RUN_SPLIT. They
REM are separate arguments because cmd splits an argument on "=", so passing
REM the whole /D flag through does not survive.
call "C:\Program Files (x86)\Microsoft Visual Studio\18\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul
set SUFFIX=%1
set RUNDEF=
if not "%2"=="" set RUNDEF=/DDSA_RUN_MAX=%2
if not "%3"=="" set RUNDEF=%RUNDEF% /DDSA_RUN_SPLIT=%3
cl /nologo /O2 /Ob3 /Oi /GS- /GL /arch:AVX2 %RUNDEF% /I native ^
   native\sabench.c native\descriptor.c native\libsais\libsais.c ^
   /Fo:native\ /Fe:native\sabench%SUFFIX%.exe /link /RELEASE /LTCG || exit /b 1
del native\*.obj >nul 2>&1
echo built native\sabench%SUFFIX%.exe
