// stage1.cuh -- AstroBWTv3 stage 1 on the GPU, one hash per thread.
//
// Stage 1 is the ~7% of the hash that is not the suffix sort. It runs a 256-way
// switch over byte operations on a 256-byte state, 261-277 times, accumulating
// every intermediate state into a text buffer. The suffix array of that text is
// stage 2 (sais.cuh).
//
// The 256-way switch is the part AstroBWTv3 aims at GPUs on purpose: a warp
// executes 32 lanes in lockstep, so 32 lanes picking 32 different cases run one
// after another. Nothing here fixes that -- it is inherent to the algorithm and
// to the thread-per-hash mapping. It is cheap in absolute terms only because
// each case body touches at most 32 bytes.
//
// The case bodies are generated, not hand-written: see gpu/gencases.
//
// Storage is the caller's choice and it matters more than anything else here.
// step_3 and the RC4 permutation are both 256-byte arrays indexed by runtime
// values, so as plain locals they land in local memory -- global memory with a
// per-thread stride, where neighbouring lanes are 256 bytes apart and every
// access costs its own 32-byte sector. Handing in shared memory instead is
// worth several times the throughput, so both buffers are parameters.
//
// Returns the text length, which is 65,000-71,000 bytes and varies per nonce.

#pragma once
#include <cstdint>
#include "crypto.cuh"

// tries stops at 277, and each try appends 256 bytes.
#define ASTRO_MAX_TRIES 277
#define ASTRO_MAX_TEXT  (ASTRO_MAX_TRIES * 256)

// Runs stage 1 for one input.
//
//   text    ASTRO_MAX_TEXT bytes; the return value says how many are live
//   s       256 bytes of scratch for step_3      -- put these in shared memory
//   rc4buf  256 bytes of scratch for RC4's state
//
// s and rc4buf may carry any per-thread stride the caller likes; this only ever
// indexes them 0..255, so a padded shared-memory stride costs nothing here.
__device__ uint32_t astroStage1(const uint8_t* input, int inlen, uint8_t* text,
                                uint8_t* s, uint8_t* rc4buf)
{
    uint8_t shaKey[32];
    uint8_t counter[16];
    RC4 rc4s;
    rc4s.s = rc4buf;

    for (int i = 0; i < 256; i++) s[i] = 0;
    for (int i = 0; i < 16; i++) counter[i] = 0;

    sha256(shaKey, input, inlen);
    salsaXOR(s, s, 256, counter, shaKey);

    rc4Init(&rc4s, s, 256);
    rc4XOR(&rc4s, s, s, 256);

    uint64_t lhash = fnv1a64(s, 256);
    uint64_t prev_lhash = lhash;

    uint64_t tries = 0;
    for (;;) {
        tries++;

        uint64_t random_switcher = prev_lhash ^ lhash ^ tries;

        uint8_t op   = (uint8_t)(random_switcher);
        uint8_t pos1 = (uint8_t)(random_switcher >> 8);
        uint8_t pos2 = (uint8_t)(random_switcher >> 16);

        if (pos1 > pos2) { uint8_t t = pos1; pos1 = pos2; pos2 = t; }

        // Cap a case body at 32 bytes. pos2 >= pos1, so this stays in range.
        if ((uint8_t)(pos2 - pos1) > 32) {
            pos2 = (uint8_t)(pos1 + ((uint8_t)(pos2 - pos1) & 0x1f));
        }

#include "stage1_cases.inc"

        // Four probabilistic deviations. The Go source compares byte
        // subtraction, which wraps, so the cast to uint8_t is load-bearing.
        uint8_t d = (uint8_t)(s[pos1] - s[pos2]);

        if (d < 0x10) {
            prev_lhash = lhash + prev_lhash;
            lhash = xxhash64(s, pos2);
        }
        if (d < 0x20) {
            prev_lhash = lhash + prev_lhash;
            lhash = fnv1a64(s, pos2);
        }
        if (d < 0x30) {
            prev_lhash = lhash + prev_lhash;
            lhash = siphash24(tries, prev_lhash, s, pos2);
        }
        if (d <= 0x40) {
            rc4XOR(&rc4s, s, s, 256);
        }

        s[255] = (uint8_t)(s[255] ^ s[pos1] ^ s[pos2]);

        uint8_t* dst = text + (tries - 1) * 256;
        for (int i = 0; i < 256; i++) dst[i] = s[i];

        if (tries > 260 + 16 || (s[255] >= 0xf0 && tries > 260)) break;
    }

    // Up to ~1 KB of the stream is discarded, by an amount the stream itself
    // chooses, so the text length varies per nonce.
    return (uint32_t)((tries - 4) * 256 +
                      ((((uint32_t)s[253] << 8) | (uint32_t)s[254]) & 0x3ff));
}

// Convenience overload for callers with no shared memory to spare. This is the
// slow path by a wide margin; it exists so correctness tests and any non-mining
// use do not have to set up a shared-memory tile.
__device__ uint32_t astroStage1(const uint8_t* input, int inlen, uint8_t* text)
{
    uint8_t s[256], rc4buf[256];
    return astroStage1(input, inlen, text, s, rc4buf);
}
