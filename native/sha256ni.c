/* sha256ni.c -- SHA-256 over two messages at once.
 *
 * The last stage of an AstroBWTv3 hash is one SHA-256 over the suffix array as
 * bytes, ~278 KB of it, and that is about a quarter of the hash. Both Go's
 * standard library and minio's assembly reach 2.52 GB/s here, which is roughly
 * two cycles a byte. That is not the SHA unit's limit, it is the dependency
 * chain: sha256rnds2 has a latency of four cycles and can start more than one
 * per cycle, so a single message leaves it idle most of the time.
 *
 * A hash cannot be split -- round n needs round n-1 -- but two hashes are
 * independent, and a miner always has another nonce to do. Interleaving two
 * measured 3.71 GB/s, 44.6% more, which is 33 us off a 338 us hash.
 *
 * Two, and not four. The SHA instructions exist only in the legacy SSE
 * encoding, so they cannot reach xmm16-31 even on an AVX-512 part. Two streams
 * hold two states (4 registers) and two message schedules (8) and need
 * temporaries on top; four would spill to memory every round and lose more than
 * the extra overlap won.
 *
 * Correctness here is not a matter of degree: the digest either matches SHA-256
 * or the miner submits nothing. native\shabench.exe checks the pair against the
 * single and the single against the published SHA-256 of "abc", and the Go side
 * checks the whole hash against the untouched reference implementation.
 */

#include <string.h>
#include <immintrin.h>

#if defined(_MSC_VER)
#include <intrin.h>
#endif

#include "sha256ni.h"

static const uint32_t K[64] = {
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

/* The shuffle that turns a little-endian 16-byte load into four big-endian
 * words, which is the order SHA-256 defines its message in. */
#define BSWAP_MASK _mm_set_epi64x(0x0c0d0e0f08090a0bULL, 0x0405060700010203ULL)

/* The SHA instructions keep the state as {A,B,E,F} and {C,D,G,H} rather than
 * A..H in order, so it has to be shuffled on the way in and out. */
static void state_in(const uint32_t st[8], __m128i* p0, __m128i* p1)
{
    __m128i t  = _mm_loadu_si128((const __m128i*)&st[0]);
    __m128i s1 = _mm_loadu_si128((const __m128i*)&st[4]);
    t  = _mm_shuffle_epi32(t, 0xB1);      /* CDAB */
    s1 = _mm_shuffle_epi32(s1, 0x1B);     /* EFGH */
    *p0 = _mm_alignr_epi8(t, s1, 8);      /* ABEF */
    *p1 = _mm_blend_epi16(s1, t, 0xF0);   /* CDGH */
}

static void state_out(uint32_t st[8], __m128i s0, __m128i s1)
{
    __m128i t = _mm_shuffle_epi32(s0, 0x1B);    /* FEBA */
    s1        = _mm_shuffle_epi32(s1, 0xB1);    /* DCHG */
    s0        = _mm_blend_epi16(t, s1, 0xF0);   /* DCBA */
    s1        = _mm_alignr_epi8(s1, t, 8);      /* HGFE */
    _mm_storeu_si128((__m128i*)&st[0], s0);
    _mm_storeu_si128((__m128i*)&st[4], s1);
}

/* One group of four rounds, plus the message schedule for the group sixteen
 * rounds later. The schedule stops after round group 11 because rounds 48-63
 * use messages already computed. */
#define R1(r, a, b, c, d)                                                      \
    do {                                                                       \
        __m128i _m = _mm_add_epi32(a, _mm_load_si128((const __m128i*)(K+4*(r)))); \
        s1 = _mm_sha256rnds2_epu32(s1, s0, _m);                                \
        _m = _mm_shuffle_epi32(_m, 0x0E);                                      \
        s0 = _mm_sha256rnds2_epu32(s0, s1, _m);                                \
        if ((r) < 12) {                                                        \
            __m128i _t = _mm_add_epi32(_mm_sha256msg1_epu32(a, b),             \
                                       _mm_alignr_epi8(d, c, 4));             \
            a = _mm_sha256msg2_epu32(_t, d);                                   \
        }                                                                      \
    } while (0)

/* The same, for two messages at once. The K load is shared. */
#define R2(r, a, b, c, d, A, B, C, D)                                          \
    do {                                                                       \
        const __m128i _k = _mm_load_si128((const __m128i*)(K+4*(r)));          \
        __m128i _m = _mm_add_epi32(a, _k), _M = _mm_add_epi32(A, _k);          \
        s1 = _mm_sha256rnds2_epu32(s1, s0, _m);                               \
        S1 = _mm_sha256rnds2_epu32(S1, S0, _M);                               \
        _m = _mm_shuffle_epi32(_m, 0x0E);                                     \
        _M = _mm_shuffle_epi32(_M, 0x0E);                                     \
        s0 = _mm_sha256rnds2_epu32(s0, s1, _m);                               \
        S0 = _mm_sha256rnds2_epu32(S0, S1, _M);                               \
        if ((r) < 12) {                                                       \
            __m128i _t = _mm_add_epi32(_mm_sha256msg1_epu32(a, b),            \
                                       _mm_alignr_epi8(d, c, 4));            \
            __m128i _T = _mm_add_epi32(_mm_sha256msg1_epu32(A, B),            \
                                       _mm_alignr_epi8(D, C, 4));            \
            a = _mm_sha256msg2_epu32(_t, d);                                  \
            A = _mm_sha256msg2_epu32(_T, D);                                  \
        }                                                                     \
    } while (0)

#define ROUNDS1 R1(0,m0,m1,m2,m3);  R1(1,m1,m2,m3,m0);  R1(2,m2,m3,m0,m1);  \
                R1(3,m3,m0,m1,m2);  R1(4,m0,m1,m2,m3);  R1(5,m1,m2,m3,m0);  \
                R1(6,m2,m3,m0,m1);  R1(7,m3,m0,m1,m2);  R1(8,m0,m1,m2,m3);  \
                R1(9,m1,m2,m3,m0);  R1(10,m2,m3,m0,m1); R1(11,m3,m0,m1,m2); \
                R1(12,m0,m1,m2,m3); R1(13,m1,m2,m3,m0); R1(14,m2,m3,m0,m1); \
                R1(15,m3,m0,m1,m2);

#define ROUNDS2 R2(0,m0,m1,m2,m3,n0,n1,n2,n3);  R2(1,m1,m2,m3,m0,n1,n2,n3,n0);  \
                R2(2,m2,m3,m0,m1,n2,n3,n0,n1);  R2(3,m3,m0,m1,m2,n3,n0,n1,n2);  \
                R2(4,m0,m1,m2,m3,n0,n1,n2,n3);  R2(5,m1,m2,m3,m0,n1,n2,n3,n0);  \
                R2(6,m2,m3,m0,m1,n2,n3,n0,n1);  R2(7,m3,m0,m1,m2,n3,n0,n1,n2);  \
                R2(8,m0,m1,m2,m3,n0,n1,n2,n3);  R2(9,m1,m2,m3,m0,n1,n2,n3,n0);  \
                R2(10,m2,m3,m0,m1,n2,n3,n0,n1); R2(11,m3,m0,m1,m2,n3,n0,n1,n2); \
                R2(12,m0,m1,m2,m3,n0,n1,n2,n3); R2(13,m1,m2,m3,m0,n1,n2,n3,n0); \
                R2(14,m2,m3,m0,m1,n2,n3,n0,n1); R2(15,m3,m0,m1,m2,n3,n0,n1,n2);

#define LOAD4(p, x0, x1, x2, x3)                                              \
    x0 = _mm_shuffle_epi8(_mm_loadu_si128((const __m128i*)((p)+0)),  mask);    \
    x1 = _mm_shuffle_epi8(_mm_loadu_si128((const __m128i*)((p)+16)), mask);    \
    x2 = _mm_shuffle_epi8(_mm_loadu_si128((const __m128i*)((p)+32)), mask);    \
    x3 = _mm_shuffle_epi8(_mm_loadu_si128((const __m128i*)((p)+48)), mask)

static void blocks_1(uint32_t st[8], const uint8_t* d, int64_t blocks)
{
    const __m128i mask = BSWAP_MASK;
    __m128i s0, s1, m0, m1, m2, m3;
    state_in(st, &s0, &s1);
    for (int64_t i = 0; i < blocks; i++, d += 64) {
        const __m128i a0 = s0, a1 = s1;
        LOAD4(d, m0, m1, m2, m3);
        ROUNDS1
        s0 = _mm_add_epi32(s0, a0);
        s1 = _mm_add_epi32(s1, a1);
    }
    state_out(st, s0, s1);
}

static void blocks_2(uint32_t sta[8], const uint8_t* da,
                     uint32_t stb[8], const uint8_t* db, int64_t blocks)
{
    const __m128i mask = BSWAP_MASK;
    __m128i s0, s1, S0, S1, m0, m1, m2, m3, n0, n1, n2, n3;
    state_in(sta, &s0, &s1);
    state_in(stb, &S0, &S1);
    for (int64_t i = 0; i < blocks; i++, da += 64, db += 64) {
        const __m128i a0 = s0, a1 = s1, A0 = S0, A1 = S1;
        LOAD4(da, m0, m1, m2, m3);
        LOAD4(db, n0, n1, n2, n3);
        ROUNDS2
        s0 = _mm_add_epi32(s0, a0); s1 = _mm_add_epi32(s1, a1);
        S0 = _mm_add_epi32(S0, A0); S1 = _mm_add_epi32(S1, A1);
    }
    state_out(sta, s0, s1);
    state_out(stb, S0, S1);
}

/* The tail: whatever is left after the whole blocks, plus the 0x80 byte, plus
 * the message length in bits as a big-endian 64-bit number at the end. That
 * needs one more block, or two when the leftover is 56 bytes or more. */
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
#if defined(_MSC_VER)
    int r[4];
    __cpuid(r, 0);
    if (r[0] < 7) return 0;
    __cpuidex(r, 1, 0);
    const int ssse3 = (r[2] >> 9) & 1;
    const int sse41 = (r[2] >> 19) & 1;
    __cpuidex(r, 7, 0);
    const int sha = (r[1] >> 29) & 1;    /* leaf 7, EBX bit 29 */
    return sha && ssse3 && sse41;
#else
    return __builtin_cpu_supports("sha") && __builtin_cpu_supports("sse4.1")
           && __builtin_cpu_supports("ssse3");
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
