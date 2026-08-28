/* derostorm_sa.c -- the suffix-array library the miner binds at run time.
 *
 * AstroBWTv3 spends ~90% of its time building the suffix array of a ~69 KB
 * stage-1 text. The Go SA-IS in astrobwt/astrobwtv3 does that, and libsais does
 * the same job faster on exactly this data: measured on 512 real stage-1 texts,
 * interleaved so background load cancels, libsais is 1.35x on one thread and
 * 1.42x on fifteen, with byte-for-byte identical output. The suffix array of a
 * string is unique, so "identical output" is not luck -- a correct faster route
 * cannot change the proof of work.
 *
 * Bound with LoadLibrary rather than linked with cgo, for the same reasons the
 * CUDA library is (see cmd/derostorm/gpu_windows.go): one executable, no C
 * toolchain needed to build the miner, and Go's own linker does the final link.
 *
 * The context is thread-local, which is what makes the Go side trivial: mining
 * threads call runtime.LockOSThread, so one context per OS thread is one
 * context per mining thread, created on that thread's first hash and reused for
 * the rest of its life. Without it the Go side would have to keep a context
 * per worker and thread it through a hook that has nowhere to put it.
 *
 * libsais is Apache-2.0, Copyright (c) 2021-2025 Ilya Grebnov. See
 * native/libsais/LICENSE. This file is part of DeroStorm.
 */

#include <stdint.h>
#include <stddef.h>

#include "libsais/libsais.h"
#include "descriptor.h"
#include "sha256ni.h"

#if defined(_WIN32)
#define DS_API __declspec(dllexport)
#define DS_TLS __declspec(thread)
#else
#define DS_API __attribute__((visibility("default")))
#define DS_TLS __thread
#endif

/* One libsais context per OS thread. libsais_create_ctx pre-allocates the
 * workspace the construction needs, so a hash costs no allocation at all. */
static DS_TLS void* ctx_tl = NULL;

/* Freed at process exit by the OS. A miner's threads live as long as the
 * process, and a per-thread destructor for this would be platform-specific
 * work with nothing to show for it. */

/* dsa_suffix_array builds the suffix array of t[0:n] into sa[0:n].
 *
 * fs is the number of int32 slots available *after* sa[n], which libsais may
 * borrow to avoid an internal allocation. The miner's scratch has the whole
 * remainder of a fixed-size array there, so it passes what it really has.
 *
 * Returns 0 on success, or a negative libsais error. A non-zero return makes
 * the Go side fall back to its own sort for that hash, so a failure here costs
 * throughput and never correctness.
 */
DS_API int32_t dsa_suffix_array(const uint8_t* t, int32_t* sa, int32_t n, int32_t fs)
{
    if (t == NULL || sa == NULL || n <= 0) return -1;

    /* The descriptor sort first. It exploits the fact that stage 1 rewrites at
     * most 32 bytes of its 256-byte state per iteration, so the text it emits
     * is a sequence of near-copies -- see descriptor.c. Measured over the 512
     * real texts it is 1.75x libsais, and its output is byte-identical, which
     * is not a coincidence: a suffix array is unique.
     *
     * It declines rather than guesses when its own bookkeeping does not add up,
     * and libsais then does the hash. Falling back costs throughput for one
     * hash and never costs correctness. */
    if (dsa_descriptor_suffix_array(t, sa, n) == 0) return 0;

    if (ctx_tl == NULL) {
        ctx_tl = libsais_create_ctx();
        if (ctx_tl == NULL) return -2;
    }
    return libsais_ctx(ctx_tl, t, sa, n, fs, NULL);
}

/* dsa_version copies the libsais version into the caller's buffer, so the miner
 * can say what it loaded rather than asserting it.
 *
 * Into a buffer rather than returning a pointer, for the same reason the CUDA
 * library does it that way: reading a C string back through a uintptr is
 * exactly the unsafe.Pointer misuse `go vet` warns about. Always NUL
 * terminates. */
DS_API void dsa_version(char* buf, int32_t len)
{
    static const char v[] = LIBSAIS_VERSION_STRING;
    int32_t i = 0;
    if (buf == NULL || len <= 0) return;
    for (; i < len - 1 && v[i] != 0; i++) buf[i] = v[i];
    buf[i] = 0;
}

/* dsa_probe is a self-test: build the suffix array of a short string with a
 * known answer. Called once at load, so a library that is present but broken is
 * found before it is trusted with a hash. Returns 0 when it agrees.
 */
DS_API int32_t dsa_probe(void)
{
    static const uint8_t text[] = "abcabxabcd";
    static const int32_t want[] = {0, 6, 3, 1, 7, 4, 2, 8, 9, 5};
    int32_t sa[16];
    const int32_t n = (int32_t)(sizeof(text) - 1);

    const int32_t rc = dsa_suffix_array(text, sa, n, (int32_t)(sizeof(sa) / sizeof(sa[0])) - n);
    if (rc != 0) return rc;
    for (int32_t i = 0; i < n; i++) {
        if (sa[i] != want[i]) return -100 - i;
    }
    return 0;
}

/* ---------------------------------------------------------------------------
 * The paired SHA-256.
 *
 * The suffix array is ~90% of a hash and the SHA-256 over it is most of the
 * rest -- 108 us of a 338 us hash. It cannot be made parallel, but two nonces'
 * worth can be interleaved, which fills the SHA unit's pipeline and runs both
 * in 150 us instead of 217. See native/sha256ni.c for why, and
 * cmd/derostorm/sha_windows.go for how the miner pairs its nonces up.
 * ------------------------------------------------------------------------ */

/* 1 when this CPU has the SHA extensions. The miner asks once at load and
 * leaves the hook unset otherwise, so a CPU without them keeps Go's SHA-256
 * rather than executing an instruction it does not have. */
DS_API int32_t dsa_sha_probe(void)
{
    if (!dsa_sha256_available()) return 0;

    /* Present is not the same as working. One known digest, and one pair
     * checked against two singles, before the miner trusts this. */
    uint8_t d[32], p[32], q[32];
    static const uint8_t abc[3] = {'a', 'b', 'c'};
    static const uint8_t want[32] = {
        0xba,0x78,0x16,0xbf,0x8f,0x01,0xcf,0xea,0x41,0x41,0x40,0xde,0x5d,0xae,0x22,0x23,
        0xb0,0x03,0x61,0xa3,0x96,0x17,0x7a,0x9c,0xb4,0x10,0xff,0x61,0xf2,0x00,0x15,0xad};

    dsa_sha256_one(abc, 3, d);
    for (int i = 0; i < 32; i++) {
        if (d[i] != want[i]) return 0;
    }

    static const uint8_t probe[100] = {0};
    dsa_sha256_pair(abc, 3, p, probe, sizeof(probe), q);
    dsa_sha256_one(probe, sizeof(probe), d);
    for (int i = 0; i < 32; i++) {
        if (p[i] != want[i] || q[i] != d[i]) return 0;
    }
    return 1;
}

/* out_a and out_b each receive 32 bytes. */
DS_API void dsa_sha256_pair_go(const uint8_t* a, int32_t na, uint8_t* out_a,
                               const uint8_t* b, int32_t nb, uint8_t* out_b)
{
    dsa_sha256_pair(a, na, out_a, b, nb, out_b);
}
