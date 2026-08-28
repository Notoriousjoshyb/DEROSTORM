/* shabench.c -- checks and times sha256ni.c.
 *
 * The point being measured is that two independent SHA-256 messages hashed
 * together run faster than the same two hashed one after the other, because a
 * single message cannot fill the SHA unit's pipeline. See sha256ni.c.
 *
 * Correctness first, because a wrong digest means a miner that never finds a
 * share and never says why: the single is checked against the published
 * SHA-256 of "abc" and of the empty string, and the pair against the single
 * over a spread of lengths that exercises every padding case.
 */
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <windows.h>

#include "sha256ni.h"

#define NBYTES (69632 * 4)

static double now(void)
{
    LARGE_INTEGER f, c;
    QueryPerformanceFrequency(&f);
    QueryPerformanceCounter(&c);
    return (double)c.QuadPart / (double)f.QuadPart;
}

static int hexeq(const uint8_t* got, const char* want)
{
    char s[65];
    for (int i = 0; i < 32; i++) sprintf(s + 2 * i, "%02x", got[i]);
    s[64] = 0;
    return strcmp(s, want) == 0;
}

int main(void)
{
    if (!dsa_sha256_available()) {
        printf("  this CPU has no SHA extensions; nothing to measure\n");
        return 0;
    }

    uint8_t d[32], e[32];

    dsa_sha256_one((const uint8_t*)"abc", 3, d);
    printf("  SHA-256(\"abc\")   %s\n",
           hexeq(d, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
               ? "correct" : "WRONG");
    dsa_sha256_one((const uint8_t*)"", 0, d);
    printf("  SHA-256(\"\")      %s\n",
           hexeq(d, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
               ? "correct" : "WRONG");

    uint8_t* a = (uint8_t*)_aligned_malloc(NBYTES, 64);
    uint8_t* b = (uint8_t*)_aligned_malloc(NBYTES, 64);
    for (int i = 0; i < NBYTES; i++) {
        a[i] = (uint8_t)(i * 7 + 1);
        b[i] = (uint8_t)(i * 13 + 5);
    }

    /* Lengths chosen to land either side of every boundary the padding cares
     * about: empty, under one block, exactly one block, 55/56/57 bytes (where
     * the length field stops fitting in the same block), and unequal pairs so
     * the leftover whole blocks are hashed singly. */
    static const int lens[] = {0, 1, 55, 56, 57, 63, 64, 65, 127, 128, 1000,
                               69631 * 4, 69632 * 4};
    int bad = 0;
    for (int i = 0; i < (int)(sizeof(lens) / sizeof(lens[0])); i++) {
        for (int j = 0; j < (int)(sizeof(lens) / sizeof(lens[0])); j++) {
            uint8_t p[32], q[32];
            dsa_sha256_pair(a, lens[i], p, b, lens[j], q);
            dsa_sha256_one(a, lens[i], d);
            dsa_sha256_one(b, lens[j], e);
            if (memcmp(p, d, 32) || memcmp(q, e, 32)) bad++;
        }
    }
    printf("  pair matches single over %d length pairs: %s\n",
           (int)((sizeof(lens) / sizeof(lens[0])) * (sizeof(lens) / sizeof(lens[0]))),
           bad == 0 ? "yes" : "NO");

    /* The sink keeps the optimiser honest: without a use of the digest it
     * deletes the timing loop outright and reports an infinite rate. */
    volatile uint32_t sink = 0;
    const int reps = 400;

    double t = now();
    for (int i = 0; i < reps; i++) {
        dsa_sha256_one(a, NBYTES, d);
        sink ^= d[0];
    }
    const double d1 = now() - t;

    t = now();
    for (int i = 0; i < reps / 2; i++) {
        dsa_sha256_pair(a, NBYTES, d, b, NBYTES, e);
        sink ^= d[0] ^ e[0];
    }
    const double d2 = now() - t;
    (void)sink;

    printf("\n  %d KB a message, best of one, %d messages\n", NBYTES / 1024, reps);
    printf("  %-10s %10s %10s %10s\n", "at a time", "us each", "GB/s", "vs one");
    printf("  %-10s %10.1f %10.2f %10s\n", "1", d1 / reps * 1e6,
           (double)NBYTES * reps / d1 / 1e9, "--");
    printf("  %-10s %10.1f %10.2f %9.1f%%\n", "2", d2 / reps * 1e6,
           (double)NBYTES * reps / d2 / 1e9, (d1 / d2 - 1) * 100);

    _aligned_free(a);
    _aligned_free(b);
    return bad == 0 ? 0 : 1;
}
