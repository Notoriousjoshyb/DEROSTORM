/* bundle.c -- one translation unit for the native suffix sort on darwin
 * and linux/arm64. Included by cgo so the descriptor merge can inline
 * suffix_less the way whole-program optimisation does on Windows. */

#include "../../native/descriptor.c"
#include "../../native/libsais/libsais.c"
#if defined(__aarch64__) || defined(__arm64__)
#include "../../native/sha256arm.c"
#else
#include "../../native/sha256ni.c"
#endif
#include "../../native/derostorm_sa.c"
