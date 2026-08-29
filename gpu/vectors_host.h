// vectors_host.h -- host-side loader for gpu/vectors.bin, shared by the tests.
//
// The file is written by gpu/vectors/main.go and holds, per vector, the real
// stage-1 text, the suffix array Go computed for it, and (format ABWTVEC2) the
// final AstroBWTv3 hash. Inputs are not stored: they are the deterministic
// sequence workInput() reproduces below, which must stay in step with the Go
// function of the same name.
//
// Texts are padded to a common stride so a kernel can find its slot by index.

#pragma once
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <vector>

#define ASTRO_INPUT_LEN 48

struct Vectors {
    int count = 0;
    int maxLen = 0;
    bool haveHash = false;
    std::vector<int32_t> lens;
    std::vector<uint8_t> texts;   // count * maxLen
    std::vector<int32_t> sa;      // count * maxLen, expected, from Go
    std::vector<uint8_t> hash;    // count * 32, expected, from Go
    std::vector<uint8_t> inputs;  // count * ASTRO_INPUT_LEN
};

// Mirrors workInput in gpu/vectors/main.go.
inline void workInput(int i, uint8_t out[ASTRO_INPUT_LEN])
{
    memset(out, 0, ASTRO_INPUT_LEN);
    out[0] = 1;
    for (int k = 8; k < 43; k++) out[k] = (uint8_t)(k * 7 + 11);
    out[43] = (uint8_t)i;
    out[44] = (uint8_t)(i >> 8);
    out[45] = (uint8_t)(i >> 16);
}

inline Vectors loadVectors(const char* path)
{
    FILE* f = fopen(path, "rb");
    if (!f) { printf("cannot open %s\n", path); exit(1); }

    char magic[8];
    if (fread(magic, 1, 8, f) != 8) { printf("truncated header\n"); exit(1); }
    bool v2;
    if (!memcmp(magic, "ABWTVEC2", 8))      v2 = true;
    else if (!memcmp(magic, "ABWTVEC1", 8)) v2 = false;
    else { printf("%s is not a vector file\n", path); exit(1); }

    uint32_t count = 0;
    if (fread(&count, 4, 1, f) != 1) { printf("truncated header\n"); exit(1); }

    // Two passes: size everything first so the padded arrays are allocated
    // once, then rewind and fill.
    long bodyStart = ftell(f);
    std::vector<int32_t> lens(count);
    for (uint32_t i = 0; i < count; i++) {
        uint32_t n = 0;
        if (fread(&n, 4, 1, f) != 1) { printf("truncated at vector %u\n", i); exit(1); }
        lens[i] = (int32_t)n;
        fseek(f, (long)n + (long)n * 4 + (v2 ? 32 : 0), SEEK_CUR);
    }
    int maxLen = 0;
    for (uint32_t i = 0; i < count; i++) if (lens[i] > maxLen) maxLen = lens[i];

    // Rounded to sixteen, which makes every text in the layout below sixteen-byte
    // aligned. The miner's texts are slices of one allocation at a stride of
    // 277*256 and are aligned by construction; a harness laying them out at the
    // length of the longest one is not, and the run boundary test in
    // gpu/desc.cuh takes its uint4 path only on an aligned text. Without this a
    // harness measures the byte fallback and reports it as the kernel's speed.
    maxLen = (maxLen + 15) & ~15;

    Vectors v;
    v.count = (int)count;
    v.maxLen = maxLen;
    v.haveHash = v2;
    v.lens = lens;
    v.texts.assign((size_t)count * maxLen, 0);
    v.sa.assign((size_t)count * maxLen, 0);
    v.hash.assign(v2 ? (size_t)count * 32 : 0, 0);
    v.inputs.assign((size_t)count * ASTRO_INPUT_LEN, 0);

    fseek(f, bodyStart, SEEK_SET);
    for (uint32_t i = 0; i < count; i++) {
        uint32_t n = 0;
        if (fread(&n, 4, 1, f) != 1 ||
            fread(v.texts.data() + (size_t)i * maxLen, 1, n, f) != n ||
            fread(v.sa.data() + (size_t)i * maxLen, 4, n, f) != n) {
            printf("truncated body at vector %u\n", i); exit(1);
        }
        if (v2 && fread(v.hash.data() + (size_t)i * 32, 1, 32, f) != 32) {
            printf("truncated hash at vector %u\n", i); exit(1);
        }
        workInput((int)i, v.inputs.data() + (size_t)i * ASTRO_INPUT_LEN);
    }
    fclose(f);
    return v;
}
