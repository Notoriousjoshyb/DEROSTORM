/* sha256arm.c -- SHA-256 over two messages at once, using ARMv8 SHA-2.
 *
 * The x86 twin is sha256ni.c and the API is the same, declared in sha256ni.h.
 * The block function is Jeffrey Walton's public-domain ARM SHA-intrinsics
 * (noloader/SHA-Intrinsics, based on ARM / mbedTLS). Pairing is a schedule
 * only: each digest is exactly SHA-256. Apple Silicon has no SMT, so two
 * independent messages on one core is how the SHA unit stays fed -- the
 * second mining thread that does that job on Zen does not exist here.
 */

#include <string.h>
#include <stdint.h>

#include <arm_neon.h>

#if defined(__linux__)
#include <sys/auxv.h>
#include <asm/hwcap.h>
#endif

#include "sha256ni.h"

static const uint32_t K[64] __attribute__((aligned(16))) = {
0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2};

static const uint32_t IV[8] = {0x6a09e667,0xbb67ae85,0x3c6ef372,0xa54ff53a,
                               0x510e527f,0x9b05688c,0x1f83d9ab,0x5be0cd19};

/* One 64-byte block. Walton's sequence, public domain. STATE0 is A B C D,
 * STATE1 is E F G H. */
static inline void sha256_one_block(uint32x4_t* s0, uint32x4_t* s1, const uint8_t* data)
{
    uint32x4_t STATE0 = *s0, STATE1 = *s1;
    const uint32x4_t ABEF_SAVE = STATE0, CDGH_SAVE = STATE1;
    uint32x4_t MSG0, MSG1, MSG2, MSG3, TMP0, TMP1, TMP2;

    MSG0 = vreinterpretq_u32_u8(vrev32q_u8(vld1q_u8(data + 0)));
    MSG1 = vreinterpretq_u32_u8(vrev32q_u8(vld1q_u8(data + 16)));
    MSG2 = vreinterpretq_u32_u8(vrev32q_u8(vld1q_u8(data + 32)));
    MSG3 = vreinterpretq_u32_u8(vrev32q_u8(vld1q_u8(data + 48)));

    TMP0 = vaddq_u32(MSG0, vld1q_u32(K + 0x00));

    MSG0 = vsha256su0q_u32(MSG0, MSG1);
    TMP2 = STATE0;
    TMP1 = vaddq_u32(MSG1, vld1q_u32(K + 0x04));
    STATE0 = vsha256hq_u32(STATE0, STATE1, TMP0);
    STATE1 = vsha256h2q_u32(STATE1, TMP2, TMP0);
    MSG0 = vsha256su1q_u32(MSG0, MSG2, MSG3);

    MSG1 = vsha256su0q_u32(MSG1, MSG2);
    TMP2 = STATE0;
    TMP0 = vaddq_u32(MSG2, vld1q_u32(K + 0x08));
    STATE0 = vsha256hq_u32(STATE0, STATE1, TMP1);
    STATE1 = vsha256h2q_u32(STATE1, TMP2, TMP1);
    MSG1 = vsha256su1q_u32(MSG1, MSG3, MSG0);

    MSG2 = vsha256su0q_u32(MSG2, MSG3);
    TMP2 = STATE0;
    TMP1 = vaddq_u32(MSG3, vld1q_u32(K + 0x0c));
    STATE0 = vsha256hq_u32(STATE0, STATE1, TMP0);
    STATE1 = vsha256h2q_u32(STATE1, TMP2, TMP0);
    MSG2 = vsha256su1q_u32(MSG2, MSG0, MSG1);

    MSG3 = vsha256su0q_u32(MSG3, MSG0);
    TMP2 = STATE0;
    TMP0 = vaddq_u32(MSG0, vld1q_u32(K + 0x10));
    STATE0 = vsha256hq_u32(STATE0, STATE1, TMP1);
    STATE1 = vsha256h2q_u32(STATE1, TMP2, TMP1);
    MSG3 = vsha256su1q_u32(MSG3, MSG1, MSG2);

    MSG0 = vsha256su0q_u32(MSG0, MSG1);
    TMP2 = STATE0;
    TMP1 = vaddq_u32(MSG1, vld1q_u32(K + 0x14));
    STATE0 = vsha256hq_u32(STATE0, STATE1, TMP0);
    STATE1 = vsha256h2q_u32(STATE1, TMP2, TMP0);
    MSG0 = vsha256su1q_u32(MSG0, MSG2, MSG3);

    MSG1 = vsha256su0q_u32(MSG1, MSG2);
    TMP2 = STATE0;
    TMP0 = vaddq_u32(MSG2, vld1q_u32(K + 0x18));
    STATE0 = vsha256hq_u32(STATE0, STATE1, TMP1);
    STATE1 = vsha256h2q_u32(STATE1, TMP2, TMP1);
    MSG1 = vsha256su1q_u32(MSG1, MSG3, MSG0);

    MSG2 = vsha256su0q_u32(MSG2, MSG3);
    TMP2 = STATE0;
    TMP1 = vaddq_u32(MSG3, vld1q_u32(K + 0x1c));
    STATE0 = vsha256hq_u32(STATE0, STATE1, TMP0);
    STATE1 = vsha256h2q_u32(STATE1, TMP2, TMP0);
    MSG2 = vsha256su1q_u32(MSG2, MSG0, MSG1);

    MSG3 = vsha256su0q_u32(MSG3, MSG0);
    TMP2 = STATE0;
    TMP0 = vaddq_u32(MSG0, vld1q_u32(K + 0x20));
    STATE0 = vsha256hq_u32(STATE0, STATE1, TMP1);
    STATE1 = vsha256h2q_u32(STATE1, TMP2, TMP1);
    MSG3 = vsha256su1q_u32(MSG3, MSG1, MSG2);

    MSG0 = vsha256su0q_u32(MSG0, MSG1);
    TMP2 = STATE0;
    TMP1 = vaddq_u32(MSG1, vld1q_u32(K + 0x24));
    STATE0 = vsha256hq_u32(STATE0, STATE1, TMP0);
    STATE1 = vsha256h2q_u32(STATE1, TMP2, TMP0);
    MSG0 = vsha256su1q_u32(MSG0, MSG2, MSG3);

    MSG1 = vsha256su0q_u32(MSG1, MSG2);
    TMP2 = STATE0;
    TMP0 = vaddq_u32(MSG2, vld1q_u32(K + 0x28));
    STATE0 = vsha256hq_u32(STATE0, STATE1, TMP1);
    STATE1 = vsha256h2q_u32(STATE1, TMP2, TMP1);
    MSG1 = vsha256su1q_u32(MSG1, MSG3, MSG0);

    MSG2 = vsha256su0q_u32(MSG2, MSG3);
    TMP2 = STATE0;
    TMP1 = vaddq_u32(MSG3, vld1q_u32(K + 0x2c));
    STATE0 = vsha256hq_u32(STATE0, STATE1, TMP0);
    STATE1 = vsha256h2q_u32(STATE1, TMP2, TMP0);
    MSG2 = vsha256su1q_u32(MSG2, MSG0, MSG1);

    MSG3 = vsha256su0q_u32(MSG3, MSG0);
    TMP2 = STATE0;
    TMP0 = vaddq_u32(MSG0, vld1q_u32(K + 0x30));
    STATE0 = vsha256hq_u32(STATE0, STATE1, TMP1);
    STATE1 = vsha256h2q_u32(STATE1, TMP2, TMP1);
    MSG3 = vsha256su1q_u32(MSG3, MSG1, MSG2);

    TMP2 = STATE0;
    TMP1 = vaddq_u32(MSG1, vld1q_u32(K + 0x34));
    STATE0 = vsha256hq_u32(STATE0, STATE1, TMP0);
    STATE1 = vsha256h2q_u32(STATE1, TMP2, TMP0);

    TMP2 = STATE0;
    TMP0 = vaddq_u32(MSG2, vld1q_u32(K + 0x38));
    STATE0 = vsha256hq_u32(STATE0, STATE1, TMP1);
    STATE1 = vsha256h2q_u32(STATE1, TMP2, TMP1);

    TMP2 = STATE0;
    TMP1 = vaddq_u32(MSG3, vld1q_u32(K + 0x3c));
    STATE0 = vsha256hq_u32(STATE0, STATE1, TMP0);
    STATE1 = vsha256h2q_u32(STATE1, TMP2, TMP0);

    TMP2 = STATE0;
    STATE0 = vsha256hq_u32(STATE0, STATE1, TMP1);
    STATE1 = vsha256h2q_u32(STATE1, TMP2, TMP1);

    *s0 = vaddq_u32(STATE0, ABEF_SAVE);
    *s1 = vaddq_u32(STATE1, CDGH_SAVE);
}

#define SHA_H(s0, s1, tmp)                                                     \
    do {                                                                       \
        const uint32x4_t _t = (s0);                                           \
        (s0) = vsha256hq_u32((s0), (s1), (tmp));                               \
        (s1) = vsha256h2q_u32((s1), _t, (tmp));                                \
    } while (0)

/* Round-level pair: A's SHA256H then B's, so the unit is not waiting on A's
 * four-cycle H result before starting the other message. Same digest as two
 * singles -- the pairing is a schedule, not a different function. */
static inline void sha256_two_block(uint32x4_t* a0, uint32x4_t* a1, const uint8_t* da,
                                    uint32x4_t* b0, uint32x4_t* b1, const uint8_t* db)
{
    uint32x4_t A0 = *a0, A1 = *a1, B0 = *b0, B1 = *b1;
    const uint32x4_t ASA0 = A0, ASA1 = A1, BSB0 = B0, BSB1 = B1;
    uint32x4_t AM0, AM1, AM2, AM3, BM0, BM1, BM2, BM3, AT0, AT1, BT0, BT1;

    AM0 = vreinterpretq_u32_u8(vrev32q_u8(vld1q_u8(da + 0)));
    BM0 = vreinterpretq_u32_u8(vrev32q_u8(vld1q_u8(db + 0)));
    AM1 = vreinterpretq_u32_u8(vrev32q_u8(vld1q_u8(da + 16)));
    BM1 = vreinterpretq_u32_u8(vrev32q_u8(vld1q_u8(db + 16)));
    AM2 = vreinterpretq_u32_u8(vrev32q_u8(vld1q_u8(da + 32)));
    BM2 = vreinterpretq_u32_u8(vrev32q_u8(vld1q_u8(db + 32)));
    AM3 = vreinterpretq_u32_u8(vrev32q_u8(vld1q_u8(da + 48)));
    BM3 = vreinterpretq_u32_u8(vrev32q_u8(vld1q_u8(db + 48)));

    AT0 = vaddq_u32(AM0, vld1q_u32(K + 0x00));
    BT0 = vaddq_u32(BM0, vld1q_u32(K + 0x00));

#define PAIR_SU(m0, m1, m2, m3, n0, n1, n2, n3, tinA, tinB, toutA, toutB, koff) \
    do {                                                                       \
        m0 = vsha256su0q_u32(m0, m1);                                          \
        n0 = vsha256su0q_u32(n0, n1);                                          \
        toutA = vaddq_u32(m1, vld1q_u32(K + (koff)));                          \
        toutB = vaddq_u32(n1, vld1q_u32(K + (koff)));                          \
        SHA_H(A0, A1, tinA);                                                    \
        SHA_H(B0, B1, tinB);                                                    \
        m0 = vsha256su1q_u32(m0, m2, m3);                                      \
        n0 = vsha256su1q_u32(n0, n2, n3);                                      \
    } while (0)

    PAIR_SU(AM0, AM1, AM2, AM3, BM0, BM1, BM2, BM3, AT0, BT0, AT1, BT1, 0x04);
    PAIR_SU(AM1, AM2, AM3, AM0, BM1, BM2, BM3, BM0, AT1, BT1, AT0, BT0, 0x08);
    PAIR_SU(AM2, AM3, AM0, AM1, BM2, BM3, BM0, BM1, AT0, BT0, AT1, BT1, 0x0c);
    PAIR_SU(AM3, AM0, AM1, AM2, BM3, BM0, BM1, BM2, AT1, BT1, AT0, BT0, 0x10);
    PAIR_SU(AM0, AM1, AM2, AM3, BM0, BM1, BM2, BM3, AT0, BT0, AT1, BT1, 0x14);
    PAIR_SU(AM1, AM2, AM3, AM0, BM1, BM2, BM3, BM0, AT1, BT1, AT0, BT0, 0x18);
    PAIR_SU(AM2, AM3, AM0, AM1, BM2, BM3, BM0, BM1, AT0, BT0, AT1, BT1, 0x1c);
    PAIR_SU(AM3, AM0, AM1, AM2, BM3, BM0, BM1, BM2, AT1, BT1, AT0, BT0, 0x20);
    PAIR_SU(AM0, AM1, AM2, AM3, BM0, BM1, BM2, BM3, AT0, BT0, AT1, BT1, 0x24);
    PAIR_SU(AM1, AM2, AM3, AM0, BM1, BM2, BM3, BM0, AT1, BT1, AT0, BT0, 0x28);
    PAIR_SU(AM2, AM3, AM0, AM1, BM2, BM3, BM0, BM1, AT0, BT0, AT1, BT1, 0x2c);
    PAIR_SU(AM3, AM0, AM1, AM2, BM3, BM0, BM1, BM2, AT1, BT1, AT0, BT0, 0x30);

#undef PAIR_SU

    AT1 = vaddq_u32(AM1, vld1q_u32(K + 0x34));
    BT1 = vaddq_u32(BM1, vld1q_u32(K + 0x34));
    SHA_H(A0, A1, AT0);
    SHA_H(B0, B1, BT0);
    AT0 = vaddq_u32(AM2, vld1q_u32(K + 0x38));
    BT0 = vaddq_u32(BM2, vld1q_u32(K + 0x38));
    SHA_H(A0, A1, AT1);
    SHA_H(B0, B1, BT1);
    AT1 = vaddq_u32(AM3, vld1q_u32(K + 0x3c));
    BT1 = vaddq_u32(BM3, vld1q_u32(K + 0x3c));
    SHA_H(A0, A1, AT0);
    SHA_H(B0, B1, BT0);
    SHA_H(A0, A1, AT1);
    SHA_H(B0, B1, BT1);

    *a0 = vaddq_u32(A0, ASA0);
    *a1 = vaddq_u32(A1, ASA1);
    *b0 = vaddq_u32(B0, BSB0);
    *b1 = vaddq_u32(B1, BSB1);
}
#undef SHA_H

static void blocks_1(uint32_t st[8], const uint8_t* d, int64_t blocks)
{
    uint32x4_t s0 = vld1q_u32(st), s1 = vld1q_u32(st + 4);
    for (int64_t i = 0; i < blocks; i++, d += 64)
        sha256_one_block(&s0, &s1, d);
    vst1q_u32(st, s0);
    vst1q_u32(st + 4, s1);
}

/* Two messages, round-interleaved so both states stay live. */
static void blocks_2(uint32_t sta[8], const uint8_t* da,
                     uint32_t stb[8], const uint8_t* db, int64_t blocks)
{
    uint32x4_t a0 = vld1q_u32(sta), a1 = vld1q_u32(sta + 4);
    uint32x4_t b0 = vld1q_u32(stb), b1 = vld1q_u32(stb + 4);
    for (int64_t i = 0; i < blocks; i++, da += 64, db += 64)
        sha256_two_block(&a0, &a1, da, &b0, &b1, db);
    vst1q_u32(sta, a0); vst1q_u32(sta + 4, a1);
    vst1q_u32(stb, b0); vst1q_u32(stb + 4, b1);
}

static void finish(uint32_t st[8], const uint8_t* tail, int64_t tail_len,
                   int64_t total_len, uint8_t* out)
{
    uint8_t pad[128];
    memset(pad, 0, sizeof(pad));
    memcpy(pad, tail, (size_t)tail_len);
    pad[tail_len] = 0x80;

    const int64_t nblk = (tail_len >= 56) ? 2 : 1;
    const uint64_t bits = (uint64_t)total_len * 8u;
    uint8_t* len_at = pad + nblk * 64 - 8;
    for (int i = 0; i < 8; i++) len_at[i] = (uint8_t)(bits >> (56 - 8 * i));

    blocks_1(st, pad, nblk);

    for (int i = 0; i < 8; i++) {
        out[4*i + 0] = (uint8_t)(st[i] >> 24);
        out[4*i + 1] = (uint8_t)(st[i] >> 16);
        out[4*i + 2] = (uint8_t)(st[i] >> 8);
        out[4*i + 3] = (uint8_t)(st[i]);
    }
}

int dsa_sha256_available(void)
{
#if defined(__APPLE__)
    return 1;
#elif defined(__linux__)
    return (getauxval(AT_HWCAP) & HWCAP_SHA2) != 0;
#else
    return 1;
#endif
}

void dsa_sha256_one(const uint8_t* a, int64_t na, uint8_t* out_a)
{
    uint32_t st[8];
    memcpy(st, IV, sizeof(st));
    const int64_t blk = na / 64;
    blocks_1(st, a, blk);
    finish(st, a + blk * 64, na - blk * 64, na, out_a);
}

void dsa_sha256_pair(const uint8_t* a, int64_t na, uint8_t* out_a,
                     const uint8_t* b, int64_t nb, uint8_t* out_b)
{
    uint32_t sa[8], sb[8];
    memcpy(sa, IV, sizeof(sa));
    memcpy(sb, IV, sizeof(sb));

    const int64_t ba = na / 64, bb = nb / 64;
    const int64_t common = ba < bb ? ba : bb;

    blocks_2(sa, a, sb, b, common);
    if (ba > common) blocks_1(sa, a + common * 64, ba - common);
    if (bb > common) blocks_1(sb, b + common * 64, bb - common);

    finish(sa, a + ba * 64, na - ba * 64, na, out_a);
    finish(sb, b + bb * 64, nb - bb * 64, nb, out_b);
}
