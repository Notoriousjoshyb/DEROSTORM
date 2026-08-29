@echo off
REM Builds the suffix-array library the miner binds at run time.
REM
REM /arch:AVX2 because every CPU that can mine this profitably has it, and
REM libsais's inner loops vectorise. /Ob3 for aggressive inlining: libsais is
REM one translation unit built around small hot functions.
REM
REM /GL with /LTCG is whole-program optimisation, and it is the one flag here
REM worth more than it looks. Without it every .c on the line below is its own
REM translation unit, so the descriptor merge cannot inline suffix_less across
REM a file boundary and libsais cannot be specialised for the one way this
REM program calls it. Measured on native\sabench.exe at 15 threads, four
REM interleaved rounds: 44.1-45.0k texts/s without, 46.4-47.0k with -- about
REM +4.7%, every round, with no overlap between the two sets. The sort is ~90%
REM of a CPU hash, so nearly all of that reaches the hashrate.
REM
REM It costs a slower link -- "Generating code" is the whole program being
REM re-optimised once the linker can see all of it -- and nothing else. The
REM output is bit-identical: the 512 vectors in native\sabench.exe still pass.
REM
REM Run from the repository root:  native\build.bat
call "C:\Program Files (x86)\Microsoft Visual Studio\18\BuildTools\VC\Auxiliary\Build\vcvars64.bat" >nul
cl /nologo /O2 /Ob3 /Oi /GS- /GL /arch:AVX2 /LD /I native ^
   native\derostorm_sa.c native\descriptor.c native\sha256ni.c ^
   native\libsais\libsais.c ^
   /Fo:native\ /Fe:native\derostorm_sa.dll /link /RELEASE /LTCG || exit /b 1
copy /y native\derostorm_sa.dll cmd\derostorm\ >nul
copy /y native\derostorm_sa.dll bin\ >nul 2>&1
del native\*.obj native\derostorm_sa.exp native\derostorm_sa.lib >nul 2>&1
echo built native\derostorm_sa.dll and copied it to cmd\derostorm\
