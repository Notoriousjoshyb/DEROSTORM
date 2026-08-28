// sais.cuh -- SA-IS suffix sort on the GPU, one hash per thread.
//
// This does NOT transcribe sais.go. It does not have to: the suffix array of a
// string is unique, so any correct algorithm gives a byte-identical result and
// therefore an identical PoW. sais.go is 1400 lines because it carries two type
// variants plus a heavily branch-tuned driver aimed at a CPU; the classic
// Nong-Zhang-Chan SA-IS below is ~200 lines and is what a GPU wants anyway.
//
// One difference in convention has to be bridged. Go's text_32_fast sorts the
// n suffixes of text[0:n] with no sentinel. Classic SA-IS requires a unique
// smallest sentinel. So the text is presented one longer, with every real byte
// shifted up by one and a 0 sentinel at index n:
//
//     TextU8[i] = (i == n) ? 0 : text[i] + 1        alphabet 257
//
// A unique smallest suffix appended to a string cannot reorder the real
// suffixes and always sorts first, so SA[1..n] of the extended text is exactly
// the array Go produces. saisSuffixArray() returns that shifted view.
//
// Scratch, per hash, for a text of n bytes:
//
//   sa    n+1 int32     the answer; also holds every recursion level's
//                       reduced string and its suffix array (the standard
//                       in-place SA-IS trick)
//   t     SAIS_T_WORDS  S/L type bitmaps, STACKED: step 5 of each level reads
//                       its own bitmap after the recursive call returns, so a
//                       level cannot hand its buffer down. Levels at least
//                       halve, so twice the level-0 size covers all of them.
//   bkt   SAIS_BKT      bucket cursors, SHARED: every level recomputes buckets
//                       after its recursive call, so this one can be reused.
//
// Per-hash footprint is what caps occupancy here, so both of those bounds are
// tight on purpose rather than rounded up to something comfortable.

#pragma once
#include <cstdint>

// Scratch sizing for a text of at most n bytes (the sentinel makes it n+1).
// A level needs ceil(m/32) words and levels at least halve, so the stack sums
// to under 2*ceil(m/32); the +24 absorbs the per-level ceiling across the ~17
// levels a 69 KB text bottoms out at.
#define SAIS_T_WORDS(n) (2 * (((n) + 2 + 31) / 32) + 24)
#define SAIS_BKT(n)     ((n) / 2 + 260)

// S/L type bitmap. Bit set means S-type. Packing to bits rather than bytes is
// not a micro-optimisation here: the type array is read randomly by the induce
// passes, and every uncoalesced read costs a whole 32-byte sector, so shrinking
// it 8x directly buys sector efficiency.
__device__ __forceinline__ bool tGet(const uint32_t* t, int i) {
    return (t[i >> 5] >> (i & 31)) & 1u;
}
__device__ __forceinline__ void tSet(uint32_t* t, int i, bool s) {
    uint32_t m = 1u << (i & 31);
    if (s) t[i >> 5] |= m; else t[i >> 5] &= ~m;
}

// isLMS: position i starts an LMS substring when it is S-type and its
// predecessor is L-type. The i > 0 test also makes this safe to call on the
// -1 holes left in SA, because && short-circuits before any load.
__device__ __forceinline__ bool isLMS(const uint32_t* t, int i) {
    return i > 0 && tGet(t, i) && !tGet(t, i - 1);
}

// Text accessors. Level 0 reads the caller's bytes through the +1/sentinel
// shift; recursion levels read a plain int32 string built inside SA.
struct TextU8 {
    const uint8_t* p;
    int n;                                   // index of the sentinel
    __device__ __forceinline__ int operator[](int i) const {
        return i == n ? 0 : (int)p[i] + 1;
    }
};
struct TextI32 {
    const int32_t* p;
    __device__ __forceinline__ int operator[](int i) const { return (int)p[i]; }
};

// Solves an int32 level. Declared up front so saisBody can call it: level 0 is
// instantiated over TextU8 and every level below over TextI32, so this closes
// the cycle as ordinary runtime recursion. Passing a lambda instead would make
// each level a fresh template argument and never terminate at compile time.
__device__ void saisI32(const int32_t* s, int32_t* SA, int n, int K,
                        uint32_t* t, int32_t* bkt);

// bkt[c] becomes the start (end=false) or the end (end=true) of character c's
// bucket. Called six times per level, so it is worth keeping tight.
template <class S>
__device__ void getBuckets(S s, int32_t* bkt, int n, int K, bool end) {
    for (int i = 0; i < K; i++) bkt[i] = 0;
    for (int i = 0; i < n; i++) bkt[s[i]]++;
    int sum = 0;
    for (int i = 0; i < K; i++) { sum += bkt[i]; bkt[i] = end ? sum : sum - bkt[i]; }
}

// Left-to-right scan placing every L-type suffix at the front of its bucket.
template <class S>
__device__ void induceSAl(const uint32_t* t, S s, int32_t* SA, int32_t* bkt, int n, int K) {
    getBuckets(s, bkt, n, K, false);
    for (int i = 0; i < n; i++) {
        int j = SA[i] - 1;
        if (j >= 0 && !tGet(t, j)) SA[bkt[s[j]]++] = j;
    }
}

// Right-to-left scan placing every S-type suffix at the back of its bucket.
template <class S>
__device__ void induceSAs(const uint32_t* t, S s, int32_t* SA, int32_t* bkt, int n, int K) {
    getBuckets(s, bkt, n, K, true);
    for (int i = n - 1; i >= 0; i--) {
        int j = SA[i] - 1;
        if (j >= 0 && tGet(t, j)) SA[--bkt[s[j]]] = j;
    }
}

// The body shared by every level. s must have a unique smallest value at
// s[n-1]; SA has n entries, t has SAIS_T_WORDS worth left, bkt has K.
template <class S>
__device__ void saisBody(S s, int32_t* SA, uint32_t* t, int32_t* bkt, int n, int K) {
    // ---- 1. classify S/L ------------------------------------------------
    tSet(t, n - 1, true);                       // the sentinel is S-type
    if (n >= 2) tSet(t, n - 2, s[n - 2] < s[n - 1]);
    for (int i = n - 3; i >= 0; i--) {
        int a = s[i], b = s[i + 1];
        tSet(t, i, a < b || (a == b && tGet(t, i + 1)));
    }

    // ---- 2. sort the LMS substrings -------------------------------------
    getBuckets(s, bkt, n, K, true);
    for (int i = 0; i < n; i++) SA[i] = -1;
    for (int i = 1; i < n; i++) if (isLMS(t, i)) SA[--bkt[s[i]]] = i;
    induceSAl(t, s, SA, bkt, n, K);
    induceSAs(t, s, SA, bkt, n, K);

    // Compact the sorted LMS positions into SA[0:n1].
    int n1 = 0;
    for (int i = 0; i < n; i++) if (isLMS(t, SA[i])) SA[n1++] = SA[i];
    for (int i = n1; i < n; i++) SA[i] = -1;

    // ---- 3. name the LMS substrings -------------------------------------
    // Names go to SA[n1 + pos/2]. LMS starts are never adjacent, so pos/2 is
    // collision free, and n1 <= n/2 keeps the highest index inside SA.
    int name = 0, prev = -1;
    for (int i = 0; i < n1; i++) {
        int pos = SA[i];
        bool diff = false;
        for (int d = 0; d < n; d++) {
            if (prev < 0 || pos + d == n || prev + d == n ||
                s[pos + d] != s[prev + d] || tGet(t, pos + d) != tGet(t, prev + d)) {
                diff = true; break;
            }
            if (d > 0 && (isLMS(t, pos + d) || isLMS(t, prev + d))) break;
        }
        if (diff) { name++; prev = pos; }
        SA[n1 + (pos >> 1)] = name - 1;
    }
    for (int i = n - 1, j = n - 1; i >= n1; i--) if (SA[i] >= 0) SA[j--] = SA[i];

    // ---- 4. solve the reduced problem -----------------------------------
    int32_t* SA1 = SA;
    int32_t* s1 = SA + n - n1;
    if (name < n1) {
        // Names repeat, so the reduced string still needs sorting. It gets the
        // slice of the type stack above the words this level occupies.
        saisI32(s1, SA1, n1, name, t + ((n + 31) >> 5), bkt);
    } else {
        for (int i = 0; i < n1; i++) SA1[s1[i]] = i;   // all distinct: direct
    }

    // ---- 5. induce the full suffix array from the sorted LMS -------------
    getBuckets(s, bkt, n, K, true);
    for (int i = 1, j = 0; i < n; i++) if (isLMS(t, i)) s1[j++] = i;
    for (int i = 0; i < n1; i++) SA1[i] = s1[SA1[i]];
    for (int i = n1; i < n; i++) SA[i] = -1;
    for (int i = n1 - 1; i >= 0; i--) { int j = SA[i]; SA[i] = -1; SA[--bkt[s[j]]] = j; }
    induceSAl(t, s, SA, bkt, n, K);
    induceSAs(t, s, SA, bkt, n, K);
}

__device__ void saisI32(const int32_t* s, int32_t* SA, int n, int K,
                        uint32_t* t, int32_t* bkt) {
    if (n == 1) { SA[0] = 0; return; }
    saisBody(TextI32{s}, SA, t, bkt, n, K);
}

// Entry point. text is n bytes; sa must have n+1 entries (the extra one holds
// the sentinel suffix). Returns the n Go-compatible entries.
__device__ int32_t* saisSuffixArray(const uint8_t* text, int n, int32_t* sa,
                                    uint32_t* t, int32_t* bkt) {
    saisBody(TextU8{text, n}, sa, t, bkt, n + 1, 257);
    return sa + 1;                              // sa[0] is the sentinel suffix
}
