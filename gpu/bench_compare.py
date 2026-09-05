"""Interleave complete mining searches from two CUDA/HIP libraries.

Usage: python gpu/bench_compare.py before.dll after.dll --blocks 336 420 504
Only one context is allocated at a time; neither library submits network work.
"""
import argparse
import ctypes as C
import statistics
import time


def library(path):
    lib = C.CDLL(path)
    ptr = C.c_void_p
    integer = C.c_int
    lib.dsg_init.argtypes = [integer, integer, integer, C.POINTER(ptr),
                             C.POINTER(integer), C.POINTER(integer)]
    lib.dsg_free.argtypes = [ptr]
    lib.dsg_free.restype = None
    lib.dsg_search.argtypes = [ptr, C.POINTER(C.c_uint8), C.c_uint32,
                              C.POINTER(C.c_uint64), integer,
                              C.POINTER(C.c_uint32), integer, C.POINTER(integer)]
    lib.dsg_hash_one.argtypes = [ptr, C.POINTER(C.c_uint8), C.c_uint32,
                                C.POINTER(C.c_uint8)]
    lib.dsg_error.argtypes = [C.c_char_p, integer]
    return lib


def check(lib, code):
    if code:
        message = C.create_string_buffer(1024)
        lib.dsg_error(message, len(message))
        raise RuntimeError(f"GPU error {code}: {message.value.decode(errors='replace')}")


def measure(lib, blocks, batch, timed):
    context, actual_batch, actual_blocks = C.c_void_p(), C.c_int(), C.c_int()
    check(lib, lib.dsg_init(0, batch, blocks, C.byref(context),
                           C.byref(actual_batch), C.byref(actual_blocks)))
    try:
        if actual_batch.value != batch or actual_blocks.value != blocks:
            raise RuntimeError("Requested workload does not fit this device")
        work = (C.c_uint8 * 48)()
        work[0] = 1
        for i in range(8, 43):
            work[i] = (i * 7 + 29) & 255
        digest = (C.c_uint8 * 32)()
        check(lib, lib.dsg_hash_one(context, work, 0, digest))
        # A zero target makes qualifying nonces negligible, but still runs the
        # production target comparison and host/device result transfer.
        target, hits, found = (C.c_uint64 * 4)(), (C.c_uint32 * 256)(), C.c_int()
        elapsed = 0.0
        for i in range(2 + timed):
            start = time.perf_counter()
            check(lib, lib.dsg_search(context, work, i * batch, target, 0,
                                     hits, len(hits), C.byref(found)))
            duration = time.perf_counter() - start
            if i >= 2:
                elapsed += duration
        return timed * batch / elapsed, bytes(digest)
    finally:
        lib.dsg_free(context)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("before")
    parser.add_argument("after")
    parser.add_argument("--blocks", nargs="+", type=int, default=[336, 420, 504])
    parser.add_argument("--batch", type=int, default=32768)
    parser.add_argument("--rounds", type=int, default=5)
    parser.add_argument("--timed", type=int, default=8)
    args = parser.parse_args()
    if min(args.batch, args.rounds, args.timed, *args.blocks) < 1:
        parser.error("workload sizes must be positive")
    libs = [library(args.before), library(args.after)]
    rates = {blocks: [[], []] for blocks in args.blocks}
    reference = None
    for r in range(args.rounds):
        for blocks in args.blocks:
            for variant in ([0, 1] if r % 2 == 0 else [1, 0]):
                rate, digest = measure(libs[variant], blocks, args.batch, args.timed)
                if reference is None:
                    reference = digest
                if digest != reference:
                    raise RuntimeError("Libraries disagree on the verification hash")
                rates[blocks][variant].append(rate)
                print(f"round={r + 1} blocks={blocks} variant={variant} H/s={rate:.1f}", flush=True)
    for blocks, pair in rates.items():
        before, after = map(statistics.median, pair)
        print(f"median blocks={blocks}: {before:.1f} -> {after:.1f} H/s "
              f"({100 * (after / before - 1):+.2f}%)", flush=True)


if __name__ == "__main__":
    main()
