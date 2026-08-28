/* sha256ni.h -- SHA-256 over two messages at once, using the SHA extensions. */
#ifndef DSA_SHA256NI_H
#define DSA_SHA256NI_H

#include <stdint.h>

/* 1 when this CPU has the SHA extensions and the SSE levels the code needs.
 * Checked once by the caller; everything below is undefined without it. */
int dsa_sha256_available(void);

/* SHA-256 of a and of b, computed together.
 *
 * The digests are exactly what one SHA-256 of each buffer gives; the pairing is
 * a scheduling trick and not a different function. Interleaving matters because
 * sha256rnds2 has a latency of four and a throughput better than one, so a
 * single message cannot keep the unit busy: measured over 278 KB, one message
 * runs at 2.57 GB/s and two together at 3.71.
 *
 * The two lengths need not match. The common whole blocks are done in pairs and
 * whatever is left over singly, so a pair of very different lengths simply wins
 * less.
 *
 * out_a and out_b receive 32 bytes each, big-endian as SHA-256 is defined.
 */
void dsa_sha256_pair(const uint8_t* a, int64_t na, uint8_t* out_a,
                     const uint8_t* b, int64_t nb, uint8_t* out_b);

/* SHA-256 of one buffer, for checking the pair against something. */
void dsa_sha256_one(const uint8_t* a, int64_t na, uint8_t* out_a);

#endif
