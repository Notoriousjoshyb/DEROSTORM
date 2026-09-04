// gpuapi.cuh -- the one place that knows whether this is CUDA or HIP.
//
// The kernels are written once and built twice: with nvcc for NVIDIA and with
// hipcc for AMD. Everything the two runtimes disagree about is named here, so
// derostorm_gpu.cu and the .cuh files beside it read as plain CUDA and no
// second copy of the miner exists to fall out of step. There is no hipify step
// and no generated source; the same files go to both compilers.
//
// Three kinds of difference live here:
//
//   1. Host API names. cudaMalloc against hipMalloc, and forty more like it.
//      They are one for one, so a #define each is the whole of it.
//   2. Warp intrinsics. The shuffles differ only in the mask argument CUDA
//      wants and HIP ignores; __match_any_sync has no HIP equivalent and is
//      rebuilt from ballots.
//   3. Inline PTX. There is exactly one piece (an L2 prefetch hint) and AMD
//      has no equivalent instruction, so that path compiles out.
//
// Wavefront width: this builds for wave32 only, which is every RDNA part
// (gfx10xx, gfx11xx, gfx12xx -- RX 5000 through RX 9000). The block radix sort
// hard-codes 32-lane warps in its shared-memory layout, so a wave64 build
// would be silently wrong; the #error below makes it a compile failure
// instead. Vega, Polaris and CDNA are wave64 and are not supported.

#pragma once

#if defined(__HIP_PLATFORM_AMD__) || defined(__HIPCC__)
#define DSG_HIP 1
#else
#define DSG_HIP 0
#endif

#if DSG_HIP
#include <hip/hip_runtime.h>
#else
#include <cuda_runtime.h>
#endif

#include <cstdint>

// Lanes in a warp. 32 on every NVIDIA part and on RDNA in wave32 mode, which
// is what hipcc emits for gfx10 and newer by default and what buildlib_hip.sh
// pins with -mwavefrontsize32.
#define DSG_WAVE 32

// Checked on the device pass only. hipcc compiles every translation unit twice,
// once per GPU target and once for the host, and the host pass carries a
// wavefront macro that means nothing there -- it reads 64 whatever the card is.
// Testing it outside __HIP_DEVICE_COMPILE__ fails a build that was going to be
// correct.
#if DSG_HIP && defined(__HIP_DEVICE_COMPILE__)
#if defined(__AMDGCN_WAVEFRONT_SIZE__) && __AMDGCN_WAVEFRONT_SIZE__ != 32
#error "DeroStorm's AMD kernels are wave32 only -- build gfx10 and newer with -mno-wavefrontsize64, and do not build Vega/Polaris/CDNA at all."
#endif
#if defined(__AMDGCN_WAVEFRONT_SIZE) && __AMDGCN_WAVEFRONT_SIZE != 32
#error "DeroStorm's AMD kernels are wave32 only -- build gfx10 and newer with -mno-wavefrontsize64, and do not build Vega/Polaris/CDNA at all."
#endif
#endif

// ---------------------------------------------------------------------------
// host API
// ---------------------------------------------------------------------------
//
// Straight renames. Only what derostorm_gpu.cu actually calls is here: a name
// left out is a compile error the first time someone needs it, which is better
// than a half-right alias nobody noticed.

#if DSG_HIP

#define cudaError_t                    hipError_t
#define cudaSuccess                    hipSuccess
#define cudaGetErrorString             hipGetErrorString
#define cudaGetLastError               hipGetLastError

#define cudaDeviceProp                 hipDeviceProp_t
#define cudaGetDeviceCount             hipGetDeviceCount
#define cudaGetDeviceProperties        hipGetDeviceProperties
#define cudaSetDevice                  hipSetDevice
#define cudaSetDeviceFlags             hipSetDeviceFlags
#define cudaDeviceScheduleBlockingSync hipDeviceScheduleBlockingSync
#define cudaDeviceSynchronize          hipDeviceSynchronize
#define cudaMemGetInfo                 hipMemGetInfo

#define cudaMalloc                     hipMalloc
#define cudaFree                       hipFree
// HIP renamed the page-locked pair rather than aliasing it: hipHostMalloc and
// hipHostFree, against cudaHostAlloc and cudaFreeHost.
#define cudaHostAlloc                  hipHostMalloc
#define cudaHostAllocDefault           hipHostMallocDefault
#define cudaFreeHost                   hipHostFree

#define cudaMemcpy                     hipMemcpy
#define cudaMemcpyAsync                hipMemcpyAsync
#define cudaMemcpyHostToDevice         hipMemcpyHostToDevice
#define cudaMemcpyDeviceToHost         hipMemcpyDeviceToHost
#define cudaMemset                     hipMemset
#define cudaMemsetAsync                hipMemsetAsync

#define cudaStream_t                   hipStream_t
#define cudaStreamCreateWithFlags      hipStreamCreateWithFlags
#define cudaStreamNonBlocking          hipStreamNonBlocking
#define cudaStreamSynchronize          hipStreamSynchronize
#define cudaStreamDestroy              hipStreamDestroy
#define cudaStreamWaitEvent            hipStreamWaitEvent

#define cudaEvent_t                    hipEvent_t
#define cudaEventCreateWithFlags       hipEventCreateWithFlags
#define cudaEventDisableTiming         hipEventDisableTiming
#define cudaEventBlockingSync          hipEventBlockingSync
#define cudaEventRecord                hipEventRecord
#define cudaEventDestroy               hipEventDestroy
#define cudaEventSynchronize           hipEventSynchronize

#define cudaFuncSetAttribute           hipFuncSetAttribute
#define cudaFuncAttributeMaxDynamicSharedMemorySize \
                                       hipFuncAttributeMaxDynamicSharedMemorySize
#define cudaFuncSetCacheConfig         hipFuncSetCacheConfig
#define cudaFuncCachePreferShared      hipFuncCachePreferShared
#define cudaOccupancyMaxActiveBlocksPerMultiprocessor \
                                       hipOccupancyMaxActiveBlocksPerMultiprocessor

#endif /* DSG_HIP host API */

// What one unit of the thing multiProcessorCount counts is called, for the
// device description the console prints. NVIDIA calls it a streaming
// multiprocessor, AMD a compute unit, and they are the same field.
#if DSG_HIP
#define DSG_UNIT_PLURAL "CUs"
#else
#define DSG_UNIT_PLURAL "SMs"
#endif

// ---------------------------------------------------------------------------
// device intrinsics
// ---------------------------------------------------------------------------

// Shuffles. CUDA wants a participating-lane mask it uses to reconverge; HIP has
// no such concept and its shuffles take a width instead. Every call site here
// passes a full mask and a full warp, so the two are the same operation and the
// macro is the whole difference.
#if DSG_HIP
#define DSG_SHFL_UP(v, off) __shfl_up((v), (unsigned)(off), DSG_WAVE)
#define DSG_SHFL(v, lane)   __shfl((v), (int)(lane), DSG_WAVE)
#else
#define DSG_SHFL_UP(v, off) __shfl_up_sync(0xffffffffu, (v), (unsigned)(off))
#define DSG_SHFL(v, lane)   __shfl_sync(0xffffffffu, (v), (int)(lane))
#endif

// Lanes below mine, as a mask. CUDA exposes it as a special register; on AMD it
// is cheaper to build than to read, and the lane is threadIdx.x's low bits
// because every block here is one dimensional.
__device__ __forceinline__ unsigned dsgLaneMaskLt()
{
#if DSG_HIP
    return (1u << (threadIdx.x & (DSG_WAVE - 1))) - 1u;
#else
    unsigned m;
    asm("mov.u32 %0, %%lanemask_lt;" : "=r"(m));
    return m;
#endif
}

// How many lanes below mine are set in `m` -- a lane's rank within its group.
//
// __popc(m & lanemask_lt) on NVIDIA, where the mask is a register read. AMD has
// the whole expression as one instruction: v_mbcnt_lo_u32_b32 counts the set
// bits of its operand below the calling lane. Four instructions become one on a
// line the radix scatter runs once per element per pass.
//
// mbcnt_lo alone is the wave32 answer; the wave64 form needs a second
// v_mbcnt_hi for lanes 32..63, and this file refuses to build wave64 at all.
__device__ __forceinline__ int dsgRankBelow(unsigned m)
{
#if DSG_HIP
    return (int)__builtin_amdgcn_mbcnt_lo(m, 0u);
#else
    return __popc(m & dsgLaneMaskLt());
#endif
}

// Lanes of this warp holding the same key as mine.
//
// CUDA has one instruction for it. AMD does not, so it is rebuilt from one
// ballot per key bit: a lane is in my group if it agrees with me on every bit,
// which is an AND of the ballots, or their complements, over `bits` of them.
// The caller narrows the key to that many bits first, so a 6-bit radix digit
// costs seven ballots and not thirty-two.
//
// Worth knowing: this same ballot construction was measured against
// __match_any_sync on NVIDIA -- see the note above the scatter in
// blockradix.cuh -- and came out inside the noise. The AMD path gives up
// nothing here.
__device__ __forceinline__ unsigned dsgMatchAnyBits(unsigned key, int bits)
{
#if DSG_HIP
    unsigned m = (unsigned)__ballot(1);
    for (int b = 0; b < bits; b++) {
        const unsigned one = (unsigned)__ballot((int)((key >> b) & 1u));
        m &= ((key >> b) & 1u) ? one : ~one;
    }
    return m;
#else
    (void)bits;
    return __match_any_sync(0xffffffffu, (int)key);
#endif
}

// __byte_perm: four bytes chosen out of two words.
//
// HIP defines the CUDA name, and the note that used to be here said that made
// it a single V_PERM_B32. It does not. ROCm's `__byte_perm` is a union of a
// four-byte array and a word, indexed by a runtime value -- so the two inputs
// are written to the stack and read back a byte at a time. On AMD the stack is
// scratch, which is global memory with a per-lane address.
//
// descLoadBE32 is the descriptor sort's innermost line and calls it once per
// key; descLoadBE64 calls it twice, and every SHA-256 word swap calls it once.
// A compiled derostorm_gpu.cu carried 973 scratch accesses in the suffix
// kernel, and switching to the instruction below took that to zero, so all of
// them were this.
//
// __builtin_amdgcn_perm is the instruction itself. It differs from CUDA's
// intrinsic in two ways and neither is a choice:
//
//   * it takes one selector BYTE per output byte, where CUDA takes one NIBBLE,
//     so the nibbles are spread out. A constant selector folds this away
//     entirely and a loop-invariant one hoists; there is no case in this
//     codebase where it costs more than the four ANDs and three shifts.
//   * it sees {src0, src1} with src0 on TOP, where CUDA's byte 0 is the low
//     byte of its first argument. So the arguments swap.
//
// Only selector digits 0..7 are used here and only three bits of each are read,
// which is exactly what CUDA does with them.
//
// Verified by constant folding, which LLVM does for this builtin: selectors
// 0x0123, 0x1234, 0x3456, 0x0000 and 0x7777 over {0x07060504, 0x03020100} fold
// to 0x00010203, 0x01020304, 0x03040506, 0x00000000 and 0x07070707, which is
// what CUDA returns for the same arguments.
//
// DSG_BYTE_PERM_FALLBACK restores the portable version -- correct, and slow the
// same way ROCm's own is -- if some future toolchain loses the builtin.
#ifndef DSG_BYTE_PERM_FALLBACK
#define DSG_BYTE_PERM_FALLBACK 0
#endif
#if DSG_HIP
#if DSG_BYTE_PERM_FALLBACK
__device__ __forceinline__ uint32_t dsgBytePerm(uint32_t a, uint32_t b, uint32_t s)
{
    const uint64_t v = ((uint64_t)b << 32) | (uint64_t)a;
    uint32_t r = 0;
#pragma unroll
    for (int i = 0; i < 4; i++) {
        const int idx = (int)((s >> (4 * i)) & 7u);
        r |= (uint32_t)((v >> (8 * idx)) & 0xffu) << (8 * i);
    }
    return r;
}
#else
__device__ __forceinline__ uint32_t dsgBytePerm(uint32_t a, uint32_t b, uint32_t s)
{
    const uint32_t sel = ((s      ) & 0x00000007u)
                       | ((s <<  4) & 0x00000700u)
                       | ((s <<  8) & 0x00070000u)
                       | ((s << 12) & 0x07000000u);
    return __builtin_amdgcn_perm(b, a, sel);
}
#endif
#define __byte_perm dsgBytePerm
#endif

// A permute that takes the selector in the vendor's own form, and the selector
// for the one shape this codebase asks for.
//
// __byte_perm's selector is one nibble per output byte; V_PERM_B32's is one
// byte. dsgBytePerm above widens the nibbles so the CUDA-shaped call sites read
// the same on both, and that costs four ANDs and three shifts whenever the
// selector is not a constant. descLoadBE32's is not: it depends on the low two
// bits of the address, and it is the innermost line of the descriptor sort.
//
// Both vendors want "the four bytes starting at byte m, most significant
// first", and both express walking m by adding a constant -- 0x1111 a nibble,
// 0x01010101 a byte. So the selector is built in the native form and the widen
// disappears. The two agree by construction: for m = 0..3 the CUDA selectors
// 0x0123, 0x1234, 0x2345 and 0x3456 widen to exactly 0x00010203 + m * 0x01010101.
#if DSG_HIP
#if DSG_BYTE_PERM_FALLBACK
#define DSG_PERM_SEQ(m) (0x0123u + 0x1111u * (m))
__device__ __forceinline__ uint32_t dsgBytePermNative(uint32_t a, uint32_t b,
                                                      uint32_t sel)
{
    return dsgBytePerm(a, b, sel);
}
#else
#define DSG_PERM_SEQ(m) (0x00010203u + 0x01010101u * (m))
__device__ __forceinline__ uint32_t dsgBytePermNative(uint32_t a, uint32_t b,
                                                      uint32_t sel)
{
    return __builtin_amdgcn_perm(b, a, sel);
}
#endif
#else
#define DSG_PERM_SEQ(m) (0x0123u + 0x1111u * (m))
__device__ __forceinline__ uint32_t dsgBytePermNative(uint32_t a, uint32_t b,
                                                      uint32_t sel)
{
    return __byte_perm(a, b, sel);
}
#endif

// Byte-wise compares of a packed word. CUDA has both as single instructions
// (VSET4/VSET on the SIMD-in-a-word unit); AMD has no per-byte compare at all,
// so they are rebuilt with the standard SWAR zero-byte test.
//
//   ((v & 0x7f) + 0x7f) | v   has bit 7 set exactly when the byte v is non-zero
//
// Applied to all four lanes at once: the masked add tops out at 0xfe a lane, so
// nothing carries between them. That gives one bit per differing byte, and the
// two intrinsics are that bit spread the two ways their callers want.
//
// Five instructions instead of one, on a path desc.cuh puts at about a third of
// the kernel, so this is the first place to look if the AMD hashrate lands low.
// It is correct, which is what it has to be first.
#if DSG_HIP
__device__ __forceinline__ uint32_t dsgNeqByteBits(uint32_t a, uint32_t b)
{
    const uint32_t x = a ^ b;
    return ((((x & 0x7f7f7f7fu) + 0x7f7f7f7fu) | x) & 0x80808080u) >> 7;
}

// 0x01 in each byte lane where the two differ, 0x00 where they match. The
// caller sums the lanes with a multiply by 0x01010101, so the lanes must be
// exactly 0 or 1 and not merely non-zero.
__device__ __forceinline__ uint32_t dsgVsetne4(uint32_t a, uint32_t b)
{
    return dsgNeqByteBits(a, b);
}

// 0xff in each byte lane where the two match, 0x00 where they differ.
//
// The multiply by 0xff is a lane-wise broadcast and not a mistake: every lane
// of the input is 0 or 1, so each contributes at most 0xff and nothing carries
// into the lane above.
__device__ __forceinline__ uint32_t dsgVcmpeq4(uint32_t a, uint32_t b)
{
    return (dsgNeqByteBits(a, b) ^ 0x01010101u) * 0xffu;
}

#define __vsetne4 dsgVsetne4
#define __vcmpeq4 dsgVcmpeq4
#endif

// An L2 prefetch hint, and the only inline PTX in the miner. AMD has no
// equivalent instruction that reaches L2 without also landing the line in a
// register, so the AMD build takes the other prefetch path in
// derostorm_gpu.cu -- a volatile byte read per stride, which warms the same
// lines and costs a little more.
#if DSG_HIP
#define DSG_HAVE_L2_PREFETCH 0
#else
#define DSG_HAVE_L2_PREFETCH 1
#endif
