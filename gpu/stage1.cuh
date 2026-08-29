// stage1.cuh -- AstroBWTv3 stage 1 on the GPU, one hash per thread.
//
// Stage 1 is the ~7% of the hash that is not the suffix sort. It runs a 256-way
// switch over byte operations on a 256-byte state, 261-277 times, accumulating
// every intermediate state into a text buffer. The suffix array of that text is
// stage 2 (sais.cuh).
//
// The 256-way switch is the part AstroBWTv3 aims at GPUs on purpose: a warp
// executes 32 lanes in lockstep, so 32 lanes picking 32 different cases run one
// after another. That looked inherent, and the case bodies were generated as a
// literal switch to match.
//
// It is not inherent. All 256 operations are exactly four instructions drawn
// from one set of sixteen -- @Wolf9466's observation, published in tnn-miner
// (see CREDITS.md) -- so the op can select data rather than code: a 512-byte
// table, and one loop every lane runs together. What is left diverges sixteen
// ways at worst instead of 256, and lanes that drew the same instruction at the
// same step share it. Both the table and its decoder are generated from pow.go
// by gpu/gencases, which fails the build if any case stops being four
// instructions, and the four statements that are not instructions are applied
// by op number here.
//
// Building with -DSTAGE1_SWITCH selects the old literal switch instead. It is
// kept because it is the readable form of the same thing, and because a claim
// that the table is faster is only worth what the measurement beside it is.
//
// Both forms are generated, not hand-written: see gpu/gencases.
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

// STAGE1_CODE and stage1Step. After crypto.cuh, which is where the rotl8 and
// rev8 they use come from. The -DSTAGE1_SWITCH build does not need it.
#ifndef STAGE1_SWITCH
#include "stage1_table.inc"
#endif

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

#ifdef STAGE1_SWITCH
#include "stage1_cases.inc"
#else
        // The same 256 cases as one loop over four instructions. The op decides
        // the data the loop reads, not the code it runs, so a warp whose lanes
        // drew 32 different ops now walks one body together instead of taking
        // 32 branches one after another.
        {
            if (op >= 254) rc4Init(&rc4s, s, 256);   // case 254/255, before the loop

            // Bytes outside, the four instructions inside, so a byte is read
            // and written once and stays in a register between them.
            //
            // The other nesting -- instruction outside, bytes inside -- is also
            // correct, because a byte's result depends only on s[i] and s[pos2]
            // and no op but case 0 writes s[pos2], so the loops commute for the
            // other 255. It takes the branch four times per op instead of four
            // times per byte, which sounds strictly better and is not: it reads
            // and writes shared memory four times per byte instead of once, and
            // measured 23.4 ms against 22.8. Whether that trade pays depends on
            // which resource is scarce, and the two answers are opposite -- the
            // same swap in Go is worth 4.7x, which is why the CPU keeps its
            // switch. See the CPU note in README.md.
            const uint16_t insns = STAGE1_CODE[op];
            for (uint8_t i = pos1; i < pos2; i++) {
                // Re-read per byte, because case 0's swap below writes s[pos2].
                const uint8_t p2 = s[pos2];
                uint8_t x = s[i];
#pragma unroll
                for (int sh = 12; sh >= 0; sh -= 4) {
                    x = stage1Step(x, p2, (insns >> sh) & 0xF);
                }
                s[i] = x;

                if (op == 0) {
                    uint8_t a = rev8(s[pos1]), b = rev8(s[pos2]);
                    s[pos2] = a;
                    s[pos1] = b;
                }
                if (op == 253) {
                    prev_lhash = lhash + prev_lhash;
                    lhash = xxhash64(s, pos2);
                }
            }
        }
#endif

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
