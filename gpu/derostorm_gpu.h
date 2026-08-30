// derostorm_gpu.h -- C API for the CUDA AstroBWTv3 miner.
//
// The Go side (cmd/derostorm/gpu_cuda.go) drives this. Everything is plain C so
// cgo can call it without a C++ runtime dependency.
//
// Lifecycle: dsg_init once, dsg_search many times, dsg_free at shutdown. Not
// safe to call concurrently on one context; use one context per GPU.

#ifndef DEROSTORM_GPU_H
#define DEROSTORM_GPU_H

#ifdef __cplusplus
extern "C" {
#endif

#include <stdint.h>

/* The library is built as a DLL on Windows: nvcc uses MSVC, and Go's cgo
 * toolchain there is MinGW, which cannot read an MSVC static archive. A DLL is
 * a stable ABI both agree on, and linking it statically against cudart means
 * the miner has no CUDA runtime DLL to ship beside it. */
#if defined(_WIN32) && defined(DSG_BUILD_DLL)
#define DSG_API __declspec(dllexport)
#else
#define DSG_API
#endif

#define DSG_WORK_SIZE 48   /* block.MINIBLOCK_SIZE */

/* Error codes. dsg_error() gives the text of the last failure. */
#define DSG_OK            0
#define DSG_ERR_NO_DEVICE 1
#define DSG_ERR_ALLOC     2
#define DSG_ERR_LAUNCH    3
#define DSG_ERR_STATE     4

typedef struct dsg_context dsg_context;

/* Opens device `device` and allocates scratch for `batch` concurrent hashes.
 * Pass batch = 0 to let the library pick from free VRAM.
 *
 * `blocks` is the resident block count to allocate suffix-kernel scratch for;
 * pass 0 for a generous default. It is an upper bound, not a setting: what is
 * actually used starts equal to it and can be lowered with dsg_set_blocks.
 *
 * On success *out holds the context and *batch_out the batch size actually
 * allocated, which is what dsg_search will hash per call. */
DSG_API int dsg_init(int device, int batch, int blocks, dsg_context** out,
             int* batch_out, int* blocks_out);

/* Changes the resident block count of the suffix kernel, which is the one knob
 * worth tuning per card: it decides how many hashes the bandwidth-bound stage
 * has in flight. Accepts 1..the count dsg_init allocated for; ask dsg_init for
 * a large `blocks` and then sweep downward with this.
 *
 * Safe to call between dsg_search calls, not during one. */
DSG_API int dsg_set_blocks(dsg_context* ctx, int blocks);

/* Reports what a tuning sweep needs to know: the device's SM count, the
 * largest block count this context can be set to, and the chunk size. Any
 * pointer may be NULL. */
DSG_API int dsg_device_shape(dsg_context* ctx, int* sms, int* max_blocks, int* chunk);

/* Hashes `batch` nonces starting at nonce_start and reports those meeting the
 * target. work is the 48-byte miniblock; bytes 43..46 are overwritten with the
 * nonce big-endian, exactly as the CPU path does.
 *
 * target is floor(2^256/difficulty) as four 64-bit limbs, limb[3] most
 * significant; target_all != 0 means every hash qualifies (difficulty 1).
 *
 * Winning nonces are written to nonces[0..max_nonces-1] and the count returned
 * in *found. A share is rare, so overflow of that buffer is not expected; if it
 * happens the surplus is dropped and *found is capped. */
DSG_API int dsg_search(dsg_context* ctx,
               const uint8_t work[DSG_WORK_SIZE],
               uint32_t nonce_start,
               const uint64_t target[4],
               int target_all,
               uint32_t* nonces, int max_nonces, int* found);

/* dsg_search split in two, so the caller can keep the card fed.
 *
 * dsg_search enqueues a batch and then blocks until it is done, which leaves
 * the GPU idle for the whole of the host's wake-up and re-enqueue. On a machine
 * whose cores are all busy mining CPU hashes, that gap is a scheduler quantum
 * and it costs real hashrate.
 *
 * dsg_submit queues a batch and returns at once; dsg_collect waits for the
 * oldest outstanding one. Submitting a second batch before collecting the first
 * means the card starts it the instant the first ends. Up to two batches may be
 * in flight; a third dsg_submit fails rather than overwriting a live slot.
 *
 * Arguments are as dsg_search's. dsg_inflight reports how many batches are
 * queued and not yet collected, which is what a caller draining before a job
 * change loops on. */
DSG_API int dsg_submit(dsg_context* ctx,
               const uint8_t work[DSG_WORK_SIZE],
               uint32_t nonce_start,
               const uint64_t target[4],
               int target_all);
DSG_API int dsg_collect(dsg_context* ctx, uint32_t* nonces, int max_nonces, int* found);
DSG_API int dsg_inflight(dsg_context* ctx);

/* Hashes one nonce and writes the 32-byte result. For verifying the GPU
 * against the CPU at start-up; not for the hot path. */
DSG_API int dsg_hash_one(dsg_context* ctx,
                 const uint8_t work[DSG_WORK_SIZE],
                 uint32_t nonce, uint8_t out[32]);

DSG_API void dsg_free(dsg_context* ctx);

/* These two write into a caller-owned buffer rather than returning a pointer
 * into the library. The only caller is Go, and reading a C string back through
 * a uintptr is exactly the unsafe.Pointer misuse the race detector and go vet
 * warn about; handing over a buffer removes the question. Both always NUL
 * terminate, and both truncate rather than overflow. */

/* Name of the device a context is using. */
DSG_API int dsg_device_name(dsg_context* ctx, char* buf, int len);

/* Text of the most recent failure on this thread. */
DSG_API int dsg_error(char* buf, int len);

/* Number of CUDA devices visible, or 0 if the driver is missing. */
DSG_API int dsg_device_count(void);

/* Describes device `device` into buf without opening it or allocating any
 * VRAM, so it is safe to call from a setup wizard. Returns DSG_OK on success;
 * buf is always NUL terminated. */
DSG_API int dsg_device_info(int device, char* buf, int len);

#ifdef __cplusplus
}
#endif

#endif /* DEROSTORM_GPU_H */
