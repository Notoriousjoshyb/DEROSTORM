@echo off
REM Builds native\shabench.exe, which checks sha256ni.c and measures one
REM SHA-256 message against two hashed together. Run from the repository root.
call "C:\Program Files (x86)\Microsoft Visual Studio\18\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul
cl /nologo /O2 /Ob3 /Oi /GS- /arch:AVX2 /I native ^
   native\shabench.c native\sha256ni.c ^
   /Fo:native\ /Fe:native\shabench.exe /link /RELEASE || exit /b 1
del native\*.obj >nul 2>&1
echo built native\shabench.exe
