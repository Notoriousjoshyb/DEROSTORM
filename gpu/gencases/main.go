// gencases translates the 256-case byte-op switch in AstroBWTv3's pow.go into
// CUDA and writes gpu/stage1_cases.inc.
//
//	go run ./gpu/gencases -pow ../derohe-main/astrobwt/astrobwtv3/pow.go -out gpu/stage1_cases.inc
//
// Hand-porting 2,300 lines of near-identical Go would be a transcription-error
// machine. Generating instead makes the port auditable: every source line has
// to match one of the known op forms exactly, and an unrecognised line is a
// hard error rather than a quietly wrong hash. Re-run it if pow.go ever moves.
//
// The output is a plain statement block, not a macro, so it is included inside
// the stage-1 loop body and stays readable in compiler diagnostics.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// storeMarker stands in for "flush the register copy of step_3[i] here".
// rawPrefix marks a line that is emitted verbatim and does not touch x, so it
// neither dirties the register copy nor forces it to be reloaded.
const (
	storeMarker = "\x00store"
	rawPrefix   = "\x01"
)

// The whole in-loop vocabulary. Survey of pow.go: 18 forms, nothing else.
// "x" is the working copy of step_3[i]; "s" is step_3.
var ops = map[string]string{
	`step_3[i] = step_3[i] ^ byte(bits.OnesCount8(step_3[i]))`: `x ^= (uint8_t)__popc(x);`,
	`step_3[i] = bits.RotateLeft8(step_3[i], 1)`:               `x = rotl8(x, 1);`,
	`step_3[i] = bits.RotateLeft8(step_3[i], 2)`:               `x = rotl8(x, 2);`,
	`step_3[i] = bits.RotateLeft8(step_3[i], 3)`:               `x = rotl8(x, 3);`,
	`step_3[i] = bits.RotateLeft8(step_3[i], 4)`:               `x = rotl8(x, 4);`,
	`step_3[i] = bits.RotateLeft8(step_3[i], 5)`:               `x = rotl8(x, 5);`,
	`step_3[i] = bits.RotateLeft8(step_3[i], int(step_3[i]))`:  `x = rotl8(x, x & 7);`,
	`step_3[i] = step_3[i] ^ bits.RotateLeft8(step_3[i], 2)`:   `x ^= rotl8(x, 2);`,
	`step_3[i] = step_3[i] ^ bits.RotateLeft8(step_3[i], 4)`:   `x ^= rotl8(x, 4);`,
	`step_3[i] = bits.Reverse8(step_3[i])`:                     `x = rev8(x);`,
	`step_3[i] = step_3[i] << (step_3[i] & 3)`:                 `x = (uint8_t)(x << (x & 3));`,
	`step_3[i] = step_3[i] >> (step_3[i] & 3)`:                 `x = (uint8_t)(x >> (x & 3));`,
	`step_3[i] = step_3[i] ^ step_3[pos2]`:                     `x ^= s[pos2];`,
	`step_3[i] = step_3[i] & step_3[pos2]`:                     `x &= s[pos2];`,
	`step_3[i] += step_3[i]`:                                   `x = (uint8_t)(x + x);`,
	`step_3[i] *= step_3[i]`:                                   `x = (uint8_t)(x * x);`,
	`step_3[i] = ^step_3[i]`:                                   `x = (uint8_t)(~x);`,
	`step_3[i] -= (step_3[i] ^ 97)`:                            `x = (uint8_t)(x - (uint8_t)(x ^ 97));`,
}

// Statements that are not plain byte ops. These touch state outside step_3[i],
// so the register copy is flushed first.
var specials = map[string][]string{
	`step_3[pos2], step_3[pos1] = bits.Reverse8(step_3[pos1]), bits.Reverse8(step_3[pos2])`: {
		storeMarker,
		rawPrefix + `{ uint8_t a = rev8(s[pos1]), b = rev8(s[pos2]); s[pos2] = a; s[pos1] = b; }`,
	},
	`prev_lhash = lhash + prev_lhash`:     {rawPrefix + `prev_lhash = lhash + prev_lhash;`},
	`lhash = xxhash.Sum64(step_3[:pos2])`: {storeMarker, rawPrefix + `lhash = xxhash64(s, pos2);`},
}

// Statements legal only before the loop.
var preLoop = map[string]string{
	`rc4s = NewCipher(step_3[:])`: `rc4Init(&rc4s, s, 256);`,
}

// The same vocabulary again, as a numbered instruction set.
//
// Every one of the 256 cases turns out to be exactly four of these sixteen, in
// some order -- an observation first published by @Wolf9466 in tnn-miner. It is
// what stage1_table.inc is built on: one 512-byte table replaces 2,300 lines of
// switch, and a warp whose lanes drew different ops runs one shared loop over
// four instructions instead of branching thirty-two ways. See gpu/stage1.cuh.
//
// The order is Wolf's, kept so the two tables can be compared entry for entry;
// nothing in the code depends on it, because the numbering and the decoder in
// stage1_table.inc are both generated from this one map.
//
// Two forms in `ops` above -- a plain rotate by 2 and by 4 -- have no entry
// here, because no case in pow.go uses them: they appear only XORed with x, as
// instructions 12 and 14. If pow.go ever grows one, the four-instruction check
// below fails loudly rather than emitting a wrong table.
var prims = []struct {
	goSrc string
	cuda  string
}{
	0:  {`step_3[i] += step_3[i]`, `(uint8_t)(x + x)`},
	1:  {`step_3[i] -= (step_3[i] ^ 97)`, `(uint8_t)(x - (uint8_t)(x ^ 97))`},
	2:  {`step_3[i] *= step_3[i]`, `(uint8_t)(x * x)`},
	3:  {`step_3[i] = step_3[i] ^ step_3[pos2]`, `(uint8_t)(x ^ p2)`},
	4:  {`step_3[i] = ^step_3[i]`, `(uint8_t)(~x)`},
	5:  {`step_3[i] = step_3[i] & step_3[pos2]`, `(uint8_t)(x & p2)`},
	6:  {`step_3[i] = step_3[i] << (step_3[i] & 3)`, `(uint8_t)(x << (x & 3))`},
	7:  {`step_3[i] = step_3[i] >> (step_3[i] & 3)`, `(uint8_t)(x >> (x & 3))`},
	8:  {`step_3[i] = bits.Reverse8(step_3[i])`, `rev8(x)`},
	9:  {`step_3[i] = step_3[i] ^ byte(bits.OnesCount8(step_3[i]))`, `(uint8_t)(x ^ (uint8_t)__popc(x))`},
	10: {`step_3[i] = bits.RotateLeft8(step_3[i], int(step_3[i]))`, `rotl8(x, x & 7)`},
	11: {`step_3[i] = bits.RotateLeft8(step_3[i], 1)`, `rotl8(x, 1)`},
	12: {`step_3[i] = step_3[i] ^ bits.RotateLeft8(step_3[i], 2)`, `(uint8_t)(x ^ rotl8(x, 2))`},
	13: {`step_3[i] = bits.RotateLeft8(step_3[i], 3)`, `rotl8(x, 3)`},
	14: {`step_3[i] = step_3[i] ^ bits.RotateLeft8(step_3[i], 4)`, `(uint8_t)(x ^ rotl8(x, 4))`},
	15: {`step_3[i] = bits.RotateLeft8(step_3[i], 5)`, `rotl8(x, 5)`},
}

// The statements outside the four instructions, and the only cases allowed to
// carry them. stage1.cuh handles these four by name; anything else appearing
// here has to be dealt with there before the table can be trusted, so this is
// checked rather than assumed.
var allowedSpecials = map[string][]int{
	`step_3[pos2], step_3[pos1] = bits.Reverse8(step_3[pos1]), bits.Reverse8(step_3[pos2])`: {0},
	`prev_lhash = lhash + prev_lhash`:     {253},
	`lhash = xxhash.Sum64(step_3[:pos2])`: {253},
	`rc4s = NewCipher(step_3[:])`:         {254, 255},
}

// Lines that exist only for CPU instrumentation or bounds-check elimination.
var ignored = map[string]bool{
	`ops[op]++`:                   true,
	`_ = step_3[pos1:pos2]`:       true,
	`if CALCULATE_DISTRIBUTION {`: true,
}

var caseRe = regexp.MustCompile(`^case ([0-9, ]+):$`)

type caseBlock struct {
	labels []int
	pre    []string // emitted before the for loop
	body   []string // emitted inside the for loop

	// The Go source these came from, kept in order and untranslated. The
	// instruction table is built from this rather than from the CUDA above, so
	// the two outputs are two readings of the same lines instead of one of them
	// being a reading of the other.
	rawPre  []string
	rawBody []string
}

func main() {
	pow := flag.String("pow", "../derohe-main/astrobwt/astrobwtv3/pow.go", "path to pow.go")
	out := flag.String("out", "gpu/stage1_cases.inc", "output include")
	table := flag.String("table", "gpu/stage1_table.inc", "output include for the instruction table")
	flag.Parse()

	raw, err := os.ReadFile(*pow)
	if err != nil {
		die(err)
	}
	lines := strings.Split(string(raw), "\n")

	start, end := -1, -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "switch op {" && start < 0 {
			start = i + 1
		}
		if start >= 0 && t == "default:" {
			end = i
			break
		}
	}
	if start < 0 || end < 0 {
		die(fmt.Errorf("could not locate the op switch in %s", *pow))
	}

	blocks, err := parse(lines, start, end)
	if err != nil {
		die(err)
	}

	seen := map[int]bool{}
	for _, b := range blocks {
		for _, l := range b.labels {
			if seen[l] {
				die(fmt.Errorf("duplicate case %d", l))
			}
			seen[l] = true
		}
	}
	missing := 0
	for i := 0; i < 256; i++ {
		if !seen[i] {
			missing++
		}
	}

	f, err := os.Create(*out)
	if err != nil {
		die(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)

	header := "" +
		"// stage1_cases.inc -- GENERATED by gpu/gencases. Do not edit.\n" +
		"//\n" +
		"// Source: %s, the op switch at lines %d-%d.\n" +
		"// %d case labels covered; %d fall through to the no-op default, as in Go.\n" +
		"//\n" +
		"// Included inside the stage-1 loop body, which must provide:\n" +
		"//   uint8_t*  s                  step_3, 256 bytes\n" +
		"//   uint8_t   op, pos1, pos2\n" +
		"//   uint64_t  lhash, prev_lhash\n" +
		"//   RC4       rc4s\n" +
		"// plus rotl8/rev8/xxhash64/rc4Init from stage1.cuh.\n" +
		"//\n" +
		"// pos1 <= pos2 always holds, so the uint8_t loop counter cannot wrap.\n" +
		"\nswitch (op) {\n"
	fmt.Fprintf(w, header, *pow, start+1, end, len(seen), missing)

	for _, b := range blocks {
		emit(w, b)
	}
	fmt.Fprintln(w, "default: break;")
	fmt.Fprintln(w, "}")

	if err := w.Flush(); err != nil {
		die(err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s: %d case labels in %d blocks, %d default\n",
		*out, len(seen), len(blocks), missing)

	if err := writeTable(*table, *pow, blocks); err != nil {
		die(err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s: 256 ops as 4 of 16 instructions each\n", *table)
}

func parse(lines []string, start, end int) ([]*caseBlock, error) {
	var blocks []*caseBlock
	var cur *caseBlock
	inLoop := false

	for ln := start; ln < end; ln++ {
		t := clean(lines[ln])
		if t == "" {
			continue
		}

		if m := caseRe.FindStringSubmatch(t); m != nil {
			cur = &caseBlock{}
			for _, fld := range strings.Split(m[1], ",") {
				fld = strings.TrimSpace(fld)
				if fld == "" {
					continue
				}
				n, err := strconv.Atoi(fld)
				if err != nil {
					return nil, fmt.Errorf("line %d: bad case label %q", ln+1, fld)
				}
				cur.labels = append(cur.labels, n)
			}
			blocks = append(blocks, cur)
			inLoop = false
			continue
		}
		if cur == nil {
			continue // preamble between "switch op {" and "case 0:"
		}
		if ignored[t] {
			continue
		}
		if t == "for i := pos1; i < pos2; i++ {" {
			if inLoop {
				return nil, fmt.Errorf("line %d: nested loop, not supported", ln+1)
			}
			inLoop = true
			continue
		}
		if t == "}" {
			inLoop = false
			continue
		}

		if !inLoop {
			c, ok := preLoop[t]
			if !ok {
				return nil, fmt.Errorf("line %d: unhandled pre-loop statement %q", ln+1, t)
			}
			cur.pre = append(cur.pre, c)
			cur.rawPre = append(cur.rawPre, t)
			continue
		}
		if c, ok := ops[t]; ok {
			cur.body = append(cur.body, c)
			cur.rawBody = append(cur.rawBody, t)
			continue
		}
		if cs, ok := specials[t]; ok {
			cur.body = append(cur.body, cs...)
			cur.rawBody = append(cur.rawBody, t)
			continue
		}
		return nil, fmt.Errorf("line %d: unhandled statement %q", ln+1, t)
	}
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no case blocks found")
	}
	return blocks, nil
}

// emit writes one case block. x is a register copy of s[i]; a special statement
// may read or write s[pos1]/s[pos2], either of which can alias s[i], so x is
// flushed before one and reloaded afterwards if further ops follow.
func emit(w *bufio.Writer, b *caseBlock) {
	for _, l := range b.labels {
		fmt.Fprintf(w, "case %d:\n", l)
	}
	for _, p := range b.pre {
		fmt.Fprintf(w, "    %s\n", p)
	}
	fmt.Fprintln(w, "    for (uint8_t i = pos1; i < pos2; i++) {")
	fmt.Fprintln(w, "        uint8_t x = s[i];")

	dirty, live := true, true
	for _, c := range b.body {
		if c == storeMarker {
			if dirty {
				fmt.Fprintln(w, "        s[i] = x;")
				dirty = false
			}
			live = false
			continue
		}
		if strings.HasPrefix(c, rawPrefix) {
			fmt.Fprintf(w, "        %s\n", strings.TrimPrefix(c, rawPrefix))
			continue
		}
		if !live {
			fmt.Fprintln(w, "        x = s[i];")
			live, dirty = true, true
		}
		fmt.Fprintf(w, "        %s\n", c)
		dirty = true
	}
	if dirty {
		fmt.Fprintln(w, "        s[i] = x;")
	}
	fmt.Fprintln(w, "    }")
	fmt.Fprintln(w, "    break;")
}

// writeTable emits the instruction table and its decoder.
//
// Everything here is a check before it is an output. Each case must reduce to
// exactly four entries of `prims`, and any statement that is not one of those
// four must be a special stage1.cuh already knows about, on a case it expects
// it on. A pow.go that broke either of those would produce a table that hashed
// wrongly and silently, so it produces a build failure instead.
func writeTable(path, powPath string, blocks []*caseBlock) error {
	code := make([]uint16, 256)
	filled := make([]bool, 256)

	byGoSrc := map[string]int{}
	for i, p := range prims {
		byGoSrc[p.goSrc] = i
	}

	for _, b := range blocks {
		var seq []int
		for _, t := range b.rawBody {
			if n, ok := byGoSrc[t]; ok {
				seq = append(seq, n)
				continue
			}
			if err := checkSpecial(t, b.labels); err != nil {
				return err
			}
		}
		for _, t := range b.rawPre {
			if err := checkSpecial(t, b.labels); err != nil {
				return err
			}
		}
		if len(seq) != 4 {
			return fmt.Errorf("case %v: %d instructions, want exactly 4 -- the table "+
				"cannot represent it, and stage1.cuh assumes four", b.labels, len(seq))
		}
		// Most significant nibble first, so the decoder reads them left to
		// right in the order pow.go applies them.
		packed := uint16(seq[0])<<12 | uint16(seq[1])<<8 | uint16(seq[2])<<4 | uint16(seq[3])
		for _, l := range b.labels {
			code[l], filled[l] = packed, true
		}
	}
	for i := range filled {
		if !filled[i] {
			return fmt.Errorf("case %d has no instructions; the table would hash it as a no-op", i)
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)

	fmt.Fprintf(w, ""+
		"// stage1_table.inc -- GENERATED by gpu/gencases. Do not edit.\n"+
		"//\n"+
		"// Source: %s.\n"+
		"//\n"+
		"// Every one of AstroBWTv3's 256 byte operations is exactly four of the sixteen\n"+
		"// instructions below, so the whole switch is a 512-byte table. The insight is\n"+
		"// @Wolf9466's, published in tnn-miner; the table here is derived from pow.go\n"+
		"// rather than copied, and gencases fails the build if any case stops being\n"+
		"// four instructions. See gpu/stage1.cuh for why a table beats a switch on a\n"+
		"// GPU, and CREDITS.md.\n"+
		"//\n"+
		"// The four statements that are not instructions -- the case 0 byte swap, case\n"+
		"// 253's hash deviation, and the case 254/255 RC4 re-key -- stay in stage1.cuh,\n"+
		"// which is checked against this file's expectations at generation time.\n\n", powPath)

	fmt.Fprintln(w, "// One instruction. p2 is s[pos2], re-read per byte because case 0 moves it.")
	fmt.Fprintln(w, "__device__ __forceinline__ uint8_t stage1Step(uint8_t x, uint8_t p2, uint32_t insn)")
	fmt.Fprintln(w, "{")
	fmt.Fprintln(w, "    switch (insn) {")
	for i, p := range prims {
		fmt.Fprintf(w, "    case %2d: return %s;  // %s\n", i, p.cuda, p.goSrc)
	}
	fmt.Fprintln(w, "    }")
	fmt.Fprintln(w, "    return x;  // unreachable: insn is a nibble and all sixteen are above")
	fmt.Fprintln(w, "}")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "// Four instructions per op, most significant nibble applied first.")
	fmt.Fprintln(w, "__device__ const uint16_t STAGE1_CODE[256] = {")
	for i := 0; i < 256; i += 8 {
		fmt.Fprint(w, "   ")
		for j := i; j < i+8; j++ {
			fmt.Fprintf(w, " 0x%04X,", code[j])
		}
		fmt.Fprintf(w, "  // %3d\n", i)
	}
	fmt.Fprintln(w, "};")

	return w.Flush()
}

func checkSpecial(stmt string, labels []int) error {
	allowed, ok := allowedSpecials[stmt]
	if !ok {
		return fmt.Errorf("case %v: %q is neither an instruction nor a known special; "+
			"stage1.cuh would drop it", labels, stmt)
	}
	for _, l := range labels {
		found := false
		for _, a := range allowed {
			if a == l {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("case %d carries %q, which stage1.cuh only applies to case %v",
				l, stmt, allowed)
		}
	}
	return nil
}

// clean strips indentation, line comments and trailing space.
func clean(l string) string {
	if i := strings.Index(l, "//"); i >= 0 {
		l = l[:i]
	}
	return strings.TrimSpace(l)
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "gencases:", err)
	os.Exit(1)
}
