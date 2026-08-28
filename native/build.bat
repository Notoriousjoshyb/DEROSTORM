@echo off
REM Builds the suffix-array library the miner binds at run time.
REM
REM /arch:AVX2 because every CPU that can mine this profitably has it, and
REM libsais's inner loops vectorise. /Ob3 for aggressive inlining: libsais is
REM one translation unit built around small hot functions.
REM
REM Run from the repository root:  native\build.bat
call "C:\Program Files (x86)\Microsoft Visual Studio\18\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul
cl /nologo /O2 /Ob3 /Oi /GS- /arch:AVX2 /LD /I native ^
   native\derostorm_sa.c native\descriptor.c native\sha256ni.c ^
   native\libsais\libsais.c ^
   /Fo:native\ /Fe:native\derostorm_sa.dll /link /RELEASE || exit /b 1
copy /y native\derostorm_sa.dll cmd\derostorm\ >nul
copy /y native\derostorm_sa.dll bin\ >nul 2>&1
del native\*.obj native\derostorm_sa.exp native\derostorm_sa.lib >nul 2>&1
echo built native\derostorm_sa.dll and copied it to cmd\derostorm\
