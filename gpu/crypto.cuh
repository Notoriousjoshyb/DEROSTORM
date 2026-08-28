// crypto.cuh -- the six primitives AstroBWTv3 stage 1 calls, as device code.
//
// Each one reproduces the exact Go package the CPU hash uses, because any
// difference produces a different hash and therefore a rejected share:
//
//   sha256      github.com/minio/sha256-simd   (plain FIPS 180-4)
//   salsa20     golang.org/x/crypto/salsa20/salsa, 20 rounds, 16-byte counter
//   rc4         astrobwtv3/rc4.go (Go stdlib RC4, allocation-free variant)
//   fnv1a64     github.com/segmentio/fasthash/fnv1a HashBytes64
//   xxhash64    github.com/cespare/xxhash Sum64        (XXH64, seed 0)
//   siphash     github.com/dchest/siphash Hash         (SipHash-2-4)
//
// All of these run once per thread on private state, so they are written for
// small register footprint rather than for parallelism. The buffers they touch
// in stage 1 are at most 256 bytes; only sha256 ever sees a large input, and
// only once per hash, over the finished suffix array.

#pragma once
#include <cstdint>

// ---------------------------------------------------------------------------
// bit helpers
// ---------------------------------------------------------------------------

// Go's bits.RotateLeft8 masks the shift to 3 bits, so k is taken mod 8 here
// too. x is promoted to int before shifting, so x >> 8 is 0 rather than
// undefined when k is 0.
__device__ __forceinline__ uint8_t rotl8(uint8_t x, unsigned k) {
    k &= 7;
    return (uint8_t)((x << k) | (x >> (8 - k)));
}

// bits.Reverse8. __brev reverses 32 bits, so the byte lands in the top octet.
__device__ __forceinline__ uint8_t rev8(uint8_t x) {
    return (uint8_t)(__brev((uint32_t)x) >> 24);
}

__device__ __forceinline__ uint32_t rotl32(uint32_t x, unsigned k) {
    return (x << k) | (x >> (32 - k));
}
__device__ __forceinline__ uint32_t rotr32(uint32_t x, unsigned k) {
    return (x >> k) | (x << (32 - k));
}
__device__ __forceinline__ uint64_t rotl64(uint64_t x, unsigned k) {
    return (x << k) | (x >> (64 - k));
}

// Unaligned little-endian loads. The suffix-array and state buffers are byte
// arrays at arbitrary offsets, so these must not assume alignment.
__device__ __forceinline__ uint32_t ld32le(const uint8_t* p) {
    return (uint32_t)p[0] | ((uint32_t)p[1] << 8) |
           ((uint32_t)p[2] << 16) | ((uint32_t)p[3] << 24);
}
__device__ __forceinline__ uint64_t ld64le(const uint8_t* p) {
    return (uint64_t)ld32le(p) | ((uint64_t)ld32le(p + 4) << 32);
}

// ---------------------------------------------------------------------------
// FNV-1a 64
// ---------------------------------------------------------------------------

__device__ __forceinline__ uint64_t fnv1a64(const uint8_t* p, int n) {
    uint64_t h = 14695981039346656037ULL;
    for (int i = 0; i < n; i++) h = (h ^ (uint64_t)p[i]) * 1099511628211ULL;
    return h;
}

// ---------------------------------------------------------------------------
// XXH64, seed 0
// ---------------------------------------------------------------------------

#define XXH_P1 11400714785074694791ULL
#define XXH_P2 14029467366897019727ULL
#define XXH_P3  1609587929392839161ULL
#define XXH_P4  9650029242287828579ULL
#define XXH_P5  2870177450012600261ULL

__device__ __forceinline__ uint64_t xxhRound(uint64_t acc, uint64_t in) {
    acc += in * XXH_P2;
    acc = rotl64(acc, 31);
    return acc * XXH_P1;
}
__device__ __forceinline__ uint64_t xxhMerge(uint64_t acc, uint64_t val) {
    acc ^= xxhRound(0, val);
    return acc * XXH_P1 + XXH_P4;
}

__device__ uint64_t xxhash64(const uint8_t* p, int n) {
    const uint8_t* end = p + n;
    uint64_t h;

    if (n >= 32) {
        uint64_t v1 = XXH_P1 + XXH_P2, v2 = XXH_P2, v3 = 0, v4 = 0ULL - XXH_P1;
        const uint8_t* limit = end - 32;
        do {
            v1 = xxhRound(v1, ld64le(p));      p += 8;
            v2 = xxhRound(v2, ld64le(p));      p += 8;
            v3 = xxhRound(v3, ld64le(p));      p += 8;
            v4 = xxhRound(v4, ld64le(p));      p += 8;
        } while (p <= limit);

        h = rotl64(v1, 1) + rotl64(v2, 7) + rotl64(v3, 12) + rotl64(v4, 18);
        h = xxhMerge(h, v1);
        h = xxhMerge(h, v2);
        h = xxhMerge(h, v3);
        h = xxhMerge(h, v4);
    } else {
        h = XXH_P5;
    }

    h += (uint64_t)n;

    while (end - p >= 8) {
        h ^= xxhRound(0, ld64le(p));
        h = rotl64(h, 27) * XXH_P1 + XXH_P4;
        p += 8;
    }
    if (end - p >= 4) {
        h ^= (uint64_t)ld32le(p) * XXH_P1;
        h = rotl64(h, 23) * XXH_P2 + XXH_P3;
        p += 4;
    }
    while (p < end) {
        h ^= (uint64_t)(*p++) * XXH_P5;
        h = rotl64(h, 11) * XXH_P1;
    }

    h ^= h >> 33; h *= XXH_P2;
    h ^= h >> 29; h *= XXH_P3;
    h ^= h >> 32;
    return h;
}

// ---------------------------------------------------------------------------
// SipHash-2-4
// ---------------------------------------------------------------------------

#define SIP_ROUND()                                    \
    do {                                               \
        v0 += v1; v1 = rotl64(v1, 13); v1 ^= v0;       \
        v0 = rotl64(v0, 32);                           \
        v2 += v3; v3 = rotl64(v3, 16); v3 ^= v2;       \
        v0 += v3; v3 = rotl64(v3, 21); v3 ^= v0;       \
        v2 += v1; v1 = rotl64(v1, 17); v1 ^= v2;       \
        v2 = rotl64(v2, 32);                           \
    } while (0)

__device__ uint64_t siphash24(uint64_t k0, uint64_t k1, const uint8_t* p, int n) {
    uint64_t v0 = k0 ^ 0x736f6d6570736575ULL;
    uint64_t v1 = k1 ^ 0x646f72616e646f6dULL;
    uint64_t v2 = k0 ^ 0x6c7967656e657261ULL;
    uint64_t v3 = k1 ^ 0x7465646279746573ULL;
    uint64_t t = (uint64_t)n << 56;

    int i = 0;
    for (; n - i >= 8; i += 8) {
        uint64_t m = ld64le(p + i);
        v3 ^= m;
        SIP_ROUND();
        SIP_ROUND();
        v0 ^= m;
    }

    // Tail: the remaining bytes packed under the length byte already in t.
    switch (n - i) {
        case 7: t |= (uint64_t)p[i + 6] << 48;  // fall through
        case 6: t |= (uint64_t)p[i + 5] << 40;  // fall through
        case 5: t |= (uint64_t)p[i + 4] << 32;  // fall through
        case 4: t |= (uint64_t)p[i + 3] << 24;  // fall through
        case 3: t |= (uint64_t)p[i + 2] << 16;  // fall through
        case 2: t |= (uint64_t)p[i + 1] << 8;   // fall through
        case 1: t |= (uint64_t)p[i];            // fall through
        default: break;
    }

    v3 ^= t;
    SIP_ROUND();
    SIP_ROUND();
    v0 ^= t;

    v2 ^= 0xff;
    SIP_ROUND();
    SIP_ROUND();
    SIP_ROUND();
    SIP_ROUND();
    return v0 ^ v1 ^ v2 ^ v3;
}

// ---------------------------------------------------------------------------
// RC4
// ---------------------------------------------------------------------------
//
// astrobwtv3/rc4.go keeps the permutation in [256]uint32 because the CPU is
// faster on word loads. Every value is 0..255 and the only place width could
// matter is the s[uint8(x+y)] index, which is masked to 8 bits anyway, so a
// byte array is bit-identical here and costs a quarter of the memory -- which
// is the resource that actually binds on the GPU.

// The permutation lives behind a pointer rather than inside the struct so the
// caller chooses its storage. A 256-byte array indexed by a runtime value lands
// in local memory, which on this GPU is global memory with a per-thread stride:
// every one of RC4's ~18,000 random accesses per hash would then cost its own
// 32-byte sector fetch. Pointing this at shared memory instead is the single
// biggest lever in stage 1.
struct RC4 {
    uint8_t* s;     // 256 bytes, caller-owned
    uint8_t  i, j;
};

__device__ void rc4Init(RC4* c, const uint8_t* key, int klen) {
    uint8_t* s = c->s;
    for (int i = 0; i < 256; i++) s[i] = (uint8_t)i;
    uint8_t j = 0;
    for (int i = 0; i < 256; i++) {
        j = (uint8_t)(j + s[i] + key[i % klen]);
        uint8_t t = s[i]; s[i] = s[j]; s[j] = t;
    }
    c->i = 0;
    c->j = 0;
}

// dst and src may be the same pointer, which is how stage 1 uses it.
__device__ void rc4XOR(RC4* c, uint8_t* dst, const uint8_t* src, int n) {
    uint8_t* st = c->s;
    uint8_t i = c->i, j = c->j;
    for (int k = 0; k < n; k++) {
        i = (uint8_t)(i + 1);
        uint8_t x = st[i];
        j = (uint8_t)(j + x);
        uint8_t y = st[j];
        st[i] = y;
        st[j] = x;
        dst[k] = (uint8_t)(src[k] ^ st[(uint8_t)(x + y)]);
    }
    c->i = i;
    c->j = j;
}

// ---------------------------------------------------------------------------
// Salsa20/20
// ---------------------------------------------------------------------------

__device__ void salsaCore(uint8_t out[64], const uint8_t in[16], const uint8_t k[32]) {
    // Sigma, "expand 32-byte k", is folded in as constants.
    uint32_t j0  = 0x61707865u;
    uint32_t j1  = ld32le(k + 0),  j2  = ld32le(k + 4);
    uint32_t j3  = ld32le(k + 8),  j4  = ld32le(k + 12);
    uint32_t j5  = 0x3320646eu;
    uint32_t j6  = ld32le(in + 0), j7  = ld32le(in + 4);
    uint32_t j8  = ld32le(in + 8), j9  = ld32le(in + 12);
    uint32_t j10 = 0x79622d32u;
    uint32_t j11 = ld32le(k + 16), j12 = ld32le(k + 20);
    uint32_t j13 = ld32le(k + 24), j14 = ld32le(k + 28);
    uint32_t j15 = 0x6b206574u;

    uint32_t x0 = j0, x1 = j1, x2 = j2, x3 = j3, x4 = j4, x5 = j5, x6 = j6, x7 = j7;
    uint32_t x8 = j8, x9 = j9, x10 = j10, x11 = j11, x12 = j12, x13 = j13, x14 = j14, x15 = j15;

    for (int i = 0; i < 20; i += 2) {
        uint32_t u;
        u = x0  + x12; x4  ^= rotl32(u, 7);
        u = x4  + x0;  x8  ^= rotl32(u, 9);
        u = x8  + x4;  x12 ^= rotl32(u, 13);
        u = x12 + x8;  x0  ^= rotl32(u, 18);

        u = x5  + x1;  x9  ^= rotl32(u, 7);
        u = x9  + x5;  x13 ^= rotl32(u, 9);
        u = x13 + x9;  x1  ^= rotl32(u, 13);
        u = x1  + x13; x5  ^= rotl32(u, 18);

        u = x10 + x6;  x14 ^= rotl32(u, 7);
        u = x14 + x10; x2  ^= rotl32(u, 9);
        u = x2  + x14; x6  ^= rotl32(u, 13);
        u = x6  + x2;  x10 ^= rotl32(u, 18);

        u = x15 + x11; x3  ^= rotl32(u, 7);
        u = x3  + x15; x7  ^= rotl32(u, 9);
        u = x7  + x3;  x11 ^= rotl32(u, 13);
        u = x11 + x7;  x15 ^= rotl32(u, 18);

        u = x0  + x3;  x1  ^= rotl32(u, 7);
        u = x1  + x0;  x2  ^= rotl32(u, 9);
        u = x2  + x1;  x3  ^= rotl32(u, 13);
        u = x3  + x2;  x0  ^= rotl32(u, 18);

        u = x5  + x4;  x6  ^= rotl32(u, 7);
        u = x6  + x5;  x7  ^= rotl32(u, 9);
        u = x7  + x6;  x4  ^= rotl32(u, 13);
        u = x4  + x7;  x5  ^= rotl32(u, 18);

        u = x10 + x9;  x11 ^= rotl32(u, 7);
        u = x11 + x10; x8  ^= rotl32(u, 9);
        u = x8  + x11; x9  ^= rotl32(u, 13);
        u = x9  + x8;  x10 ^= rotl32(u, 18);

        u = x15 + x14; x12 ^= rotl32(u, 7);
        u = x12 + x15; x13 ^= rotl32(u, 9);
        u = x13 + x12; x14 ^= rotl32(u, 13);
        u = x14 + x13; x15 ^= rotl32(u, 18);
    }

    uint32_t w[16] = { x0 + j0,   x1 + j1,   x2 + j2,   x3 + j3,
                       x4 + j4,   x5 + j5,   x6 + j6,   x7 + j7,
                       x8 + j8,   x9 + j9,   x10 + j10, x11 + j11,
                       x12 + j12, x13 + j13, x14 + j14, x15 + j15 };
    for (int i = 0; i < 16; i++) {
        out[i * 4 + 0] = (uint8_t)(w[i]);
        out[i * 4 + 1] = (uint8_t)(w[i] >> 8);
        out[i * 4 + 2] = (uint8_t)(w[i] >> 16);
        out[i * 4 + 3] = (uint8_t)(w[i] >> 24);
    }
}

// Matches salsa.XORKeyStream: counter holds nonce and block counter together,
// and the block counter is the little-endian 64-bit value at counter[8].
__device__ void salsaXOR(uint8_t* out, const uint8_t* in, int n,
                         const uint8_t counter[16], const uint8_t key[32]) {
    uint8_t block[64], ctr[16];
    for (int i = 0; i < 16; i++) ctr[i] = counter[i];

    int off = 0;
    while (n - off >= 64) {
        salsaCore(block, ctr, key);
        for (int i = 0; i < 64; i++) out[off + i] = (uint8_t)(in[off + i] ^ block[i]);
        uint32_t u = 1;
        for (int i = 8; i < 16; i++) { u += ctr[i]; ctr[i] = (uint8_t)u; u >>= 8; }
        off += 64;
    }
    if (off < n) {
        salsaCore(block, ctr, key);
        for (int i = 0; off + i < n; i++) out[off + i] = (uint8_t)(in[off + i] ^ block[i]);
    }
}

// ---------------------------------------------------------------------------
// SHA-256
// ---------------------------------------------------------------------------

__device__ __constant__ uint32_t SHA256_K[64] = {
    0x428a2f98u,0x71374491u,0xb5c0fbcfu,0xe9b5dba5u,0x3956c25bu,0x59f111f1u,0x923f82a4u,0xab1c5ed5u,
    0xd807aa98u,0x12835b01u,0x243185beu,0x550c7dc3u,0x72be5d74u,0x80deb1feu,0x9bdc06a7u,0xc19bf174u,
    0xe49b69c1u,0xefbe4786u,0x0fc19dc6u,0x240ca1ccu,0x2de92c6fu,0x4a7484aau,0x5cb0a9dcu,0x76f988dau,
    0x983e5152u,0xa831c66du,0xb00327c8u,0xbf597fc7u,0xc6e00bf3u,0xd5a79147u,0x06ca6351u,0x14292967u,
    0x27b70a85u,0x2e1b2138u,0x4d2c6dfcu,0x53380d13u,0x650a7354u,0x766a0abbu,0x81c2c92eu,0x92722c85u,
    0xa2bfe8a1u,0xa81a664bu,0xc24b8b70u,0xc76c51a3u,0xd192e819u,0xd6990624u,0xf40e3585u,0x106aa070u,
    0x19a4c116u,0x1e376c08u,0x2748774cu,0x34b0bcb5u,0x391c0cb3u,0x4ed8aa4au,0x5b9cca4fu,0x682e6ff3u,
    0x748f82eeu,0x78a5636fu,0x84c87814u,0x8cc70208u,0x90befffau,0xa4506cebu,0xbef9a3f7u,0xc67178f2u
};

struct SHA256 {
    uint32_t h[8];
    uint8_t  buf[64];
    int      fill;      // bytes currently in buf
    uint64_t total;     // total bytes fed
};

__device__ void sha256Block(uint32_t h[8], const uint8_t* p) {
    uint32_t w[64];
    for (int i = 0; i < 16; i++) {
        w[i] = ((uint32_t)p[i * 4] << 24) | ((uint32_t)p[i * 4 + 1] << 16) |
               ((uint32_t)p[i * 4 + 2] << 8) | (uint32_t)p[i * 4 + 3];
    }
    for (int i = 16; i < 64; i++) {
        uint32_t s0 = rotr32(w[i - 15], 7) ^ rotr32(w[i - 15], 18) ^ (w[i - 15] >> 3);
        uint32_t s1 = rotr32(w[i - 2], 17) ^ rotr32(w[i - 2], 19) ^ (w[i - 2] >> 10);
        w[i] = w[i - 16] + s0 + w[i - 7] + s1;
    }
    uint32_t a = h[0], b = h[1], c = h[2], d = h[3];
    uint32_t e = h[4], f = h[5], g = h[6], hh = h[7];
    for (int i = 0; i < 64; i++) {
        uint32_t S1 = rotr32(e, 6) ^ rotr32(e, 11) ^ rotr32(e, 25);
        uint32_t ch = (e & f) ^ (~e & g);
        uint32_t t1 = hh + S1 + ch + SHA256_K[i] + w[i];
        uint32_t S0 = rotr32(a, 2) ^ rotr32(a, 13) ^ rotr32(a, 22);
        uint32_t mj = (a & b) ^ (a & c) ^ (b & c);
        uint32_t t2 = S0 + mj;
        hh = g; g = f; f = e; e = d + t1;
        d = c; c = b; b = a; a = t1 + t2;
    }
    h[0] += a; h[1] += b; h[2] += c; h[3] += d;
    h[4] += e; h[5] += f; h[6] += g; h[7] += hh;
}

__device__ void sha256Init(SHA256* s) {
    s->h[0] = 0x6a09e667u; s->h[1] = 0xbb67ae85u;
    s->h[2] = 0x3c6ef372u; s->h[3] = 0xa54ff53au;
    s->h[4] = 0x510e527fu; s->h[5] = 0x9b05688cu;
    s->h[6] = 0x1f83d9abu; s->h[7] = 0x5be0cd19u;
    s->fill = 0;
    s->total = 0;
}

__device__ void sha256Update(SHA256* s, const uint8_t* p, int n) {
    s->total += (uint64_t)n;
    int off = 0;
    if (s->fill) {
        int want = 64 - s->fill;
        int take = n < want ? n : want;
        for (int i = 0; i < take; i++) s->buf[s->fill + i] = p[i];
        s->fill += take;
        off = take;
        if (s->fill < 64) return;
        sha256Block(s->h, s->buf);
        s->fill = 0;
    }
    for (; n - off >= 64; off += 64) sha256Block(s->h, p + off);
    for (; off < n; off++) s->buf[s->fill++] = p[off];
}

__device__ void sha256Final(SHA256* s, uint8_t out[32]) {
    uint64_t bits = s->total * 8;
    s->buf[s->fill++] = 0x80;
    if (s->fill > 56) {
        while (s->fill < 64) s->buf[s->fill++] = 0;
        sha256Block(s->h, s->buf);
        s->fill = 0;
    }
    while (s->fill < 56) s->buf[s->fill++] = 0;
    for (int i = 7; i >= 0; i--) s->buf[s->fill++] = (uint8_t)(bits >> (i * 8));
    sha256Block(s->h, s->buf);
    for (int i = 0; i < 8; i++) {
        out[i * 4 + 0] = (uint8_t)(s->h[i] >> 24);
        out[i * 4 + 1] = (uint8_t)(s->h[i] >> 16);
        out[i * 4 + 2] = (uint8_t)(s->h[i] >> 8);
        out[i * 4 + 3] = (uint8_t)(s->h[i]);
    }
}

__device__ void sha256(uint8_t out[32], const uint8_t* p, int n) {
    SHA256 s;
    sha256Init(&s);
    sha256Update(&s, p, n);
    sha256Final(&s, out);
}
