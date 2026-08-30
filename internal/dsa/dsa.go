// Package dsa is the structure-exploiting suffix sort, in Go.
//
// native/descriptor.c is the fast path on windows/amd64 and linux/amd64.
// This is the same algorithm for every other target -- darwin, linux/arm64 --
// so a Mac is no longer stuck on the Go SA-IS that is a quarter of the native
// hashrate. The suffix array of a string is unique, so a correct port is
// bit-identical to the C and to libsais; internal/dsa/dsa_test.go checks that.
package dsa

import (
	"encoding/binary"
	"sync"
)

const (
	runMax    = 512
	runSplit  = 160
	blockSize = 256
	keyBytes  = 3
	lenBits   = 12
	lenMask   = (1 << lenBits) - 1
	countMin  = 48
	rankMax   = 16
	groupCap  = 8192
	rbits     = 12
	rpass     = 2
	rbins     = 1 << rbits
	rmask     = rbins - 1
)

type desc struct {
	key    uint32
	packed uint32
}

func off(d desc) uint32 { return d.packed >> lenBits }
func dlen(d desc) uint32 { return d.packed & lenMask }
func pack(o, n uint32) uint32 { return (o << lenBits) | n }

type scratch struct {
	arena  []uint32
	desc   []desc
	desc2  []desc
	order  []uint32
	order2 []uint32
	merge  []uint32
	merge2 []uint32
	bnd    []uint32
	bnd2   []uint32
	cap    int
	dcap   int
}

var pool sync.Pool

func getScratch(n, wantDesc int) *scratch {
	s, _ := pool.Get().(*scratch)
	if s != nil && s.cap >= n && s.dcap >= wantDesc {
		return s
	}
	if s == nil {
		s = &scratch{}
	}
	s.cap, s.dcap = n, wantDesc
	s.arena = make([]uint32, n+8)
	s.desc = make([]desc, wantDesc)
	s.desc2 = make([]desc, wantDesc)
	s.order = make([]uint32, runMax+8)
	s.order2 = make([]uint32, runMax+8)
	s.merge = make([]uint32, groupCap+8)
	s.merge2 = make([]uint32, groupCap+8)
	s.bnd = make([]uint32, groupCap+8)
	s.bnd2 = make([]uint32, groupCap+8)
	return s
}

func key32(t []byte, n, p int) uint32 {
	if p+4 <= n {
		return binary.BigEndian.Uint32(t[p:]) >> (8 * (4 - keyBytes))
	}
	var k uint32
	for i := 0; i < keyBytes; i++ {
		k <<= 8
		if p+i < n {
			k |= uint32(t[p+i])
		}
	}
	return k
}

func suffixLessFrom(t []byte, n int, a, b, from uint32) bool {
	la, lb := n-int(a), n-int(b)
	m := la
	if lb < m {
		m = lb
	}
	i := 0
	if m >= int(from) {
		i = int(from)
	}
	remain := m - i
	q := remain
	if q > 32 {
		q = 32
	}
	q &= ^7
	for k := 0; k < q; k += 8 {
		x := binary.BigEndian.Uint64(t[int(a)+i+k:])
		y := binary.BigEndian.Uint64(t[int(b)+i+k:])
		if x != y {
			return x < y
		}
	}
	aa, bb := t[int(a)+i+q:int(a)+m], t[int(b)+i+q:int(b)+m]
	for j := 0; j < len(aa); j++ {
		if aa[j] != bb[j] {
			return aa[j] < bb[j]
		}
	}
	return la < lb
}

func suffixLess(t []byte, n int, a, b uint32) bool {
	return suffixLessFrom(t, n, a, b, 0)
}

func columnStepKeys(order, keys, tmp []uint32, blocks uint32) {
	top := uint32(8 * (keyBytes - 1))
	if blocks == 4 {
		v0 := ((keys[0] >> top) << 9) | 0
		v1 := ((keys[1] >> top) << 9) | 1
		v2 := ((keys[2] >> top) << 9) | 2
		v3 := ((keys[3] >> top) << 9) | 3
		r0 := uint32(0)
		r1 := uint32(0)
		r2 := uint32(0)
		r3 := uint32(0)
		if v1 < v0 {
			r0++
		}
		if v2 < v0 {
			r0++
		}
		if v3 < v0 {
			r0++
		}
		if v0 < v1 {
			r1++
		}
		if v2 < v1 {
			r1++
		}
		if v3 < v1 {
			r1++
		}
		if v0 < v2 {
			r2++
		}
		if v1 < v2 {
			r2++
		}
		if v3 < v2 {
			r2++
		}
		if v0 < v3 {
			r3++
		}
		if v1 < v3 {
			r3++
		}
		if v2 < v3 {
			r3++
		}
		var ko, kk [4]uint32
		ko[r0], kk[r0] = order[0], keys[0]
		ko[r1], kk[r1] = order[1], keys[1]
		ko[r2], kk[r2] = order[2], keys[2]
		ko[r3], kk[r3] = order[3], keys[3]
		copy(order[:4], ko[:])
		copy(keys[:4], kk[:])
		return
	}
	if blocks <= rankMax {
		var v [rankMax]uint32
		var ko, kk [rankMax]uint32
		for x := uint32(0); x < blocks; x++ {
			v[x] = ((keys[x] >> top) << 9) | x
		}
		for x := uint32(0); x < blocks; x++ {
			r := uint32(0)
			for y := uint32(0); y < blocks; y++ {
				if v[y] < v[x] {
					r++
				}
			}
			ko[r] = order[x]
			kk[r] = keys[x]
		}
		copy(order[:int(blocks)], ko[:int(blocks)])
		copy(keys[:int(blocks)], kk[:int(blocks)])
		return
	}
	if blocks < countMin {
		for x := uint32(1); x < blocks; x++ {
			vo, vk := order[x], keys[x]
			kb := vk >> top
			y := x
			for y > 0 && keys[y-1]>>top > kb {
				order[y] = order[y-1]
				keys[y] = keys[y-1]
				y--
			}
			order[y], keys[y] = vo, vk
		}
		return
	}
	var cnt [256]uint32
	for x := uint32(0); x < blocks; x++ {
		cnt[keys[x]>>top]++
	}
	sum := uint32(0)
	for b := 0; b < 256; b++ {
		c := cnt[b]
		cnt[b] = sum
		sum += c
	}
	var ktmp [runMax]uint32
	for x := uint32(0); x < blocks; x++ {
		slot := cnt[keys[x]>>top]
		cnt[keys[x]>>top]++
		tmp[slot] = order[x]
		ktmp[slot] = keys[x]
	}
	copy(order[:int(blocks)], tmp[:int(blocks)])
	copy(keys[:int(blocks)], ktmp[:int(blocks)])
}

func emitRun(t []byte, n int, first, blocks uint32, s *scratch, arenaLen, descLen *int) bool {
	descRoom := s.dcap
	base := first * blockSize
	order := s.order

	for i := uint32(0); i < blocks; i++ {
		order[i] = base + i*blockSize + 255
	}
	for i := uint32(1); i < blocks; i++ {
		v := order[i]
		j := i
		for j > 0 && suffixLess(t, n, v, order[j-1]) {
			order[j] = order[j-1]
			j--
		}
		order[j] = v
	}
	for i := uint32(0); i < blocks; i++ {
		order[i] -= 255
	}

	var constant [blockSize]byte
	for i := range constant {
		constant[i] = 1
	}
	b0 := t[int(base) : int(base)+blockSize]
	for g := uint32(1); g < blocks; g++ {
		bg := t[int(base)+int(g)*blockSize:]
		for i := 0; i < blockSize; i += 8 {
			xor := binary.LittleEndian.Uint64(b0[i:]) ^ binary.LittleEndian.Uint64(bg[i:])
			if xor == 0 {
				continue
			}
			for k := 0; k < 8; k++ {
				if (xor>>(8*k))&0xff != 0 {
					constant[i+k] = 0
				}
			}
		}
	}

	var sameKey [blockSize]byte
	for rel := 0; rel+keyBytes <= blockSize; rel++ {
		s := constant[rel]
		for k := 1; k < keyBytes; k++ {
			s &= constant[rel+k]
		}
		sameKey[rel] = s
	}

	var keys [runMax]uint32
	for i := uint32(0); i < blocks; i++ {
		keys[i] = key32(t, n, int(order[i])+255)
	}
	keyShift := uint32(8 * (keyBytes - 1))

	for rel := blockSize - 1; rel >= 0; rel-- {
		r := uint32(rel)
		if *descLen+int(blocks) > descRoom {
			return false
		}
		if sameKey[r] != 0 {
			s.desc[*descLen] = desc{key: keys[0], packed: pack(uint32(*arenaLen), blocks)}
			for x := uint32(0); x < blocks; x++ {
				s.arena[*arenaLen] = order[x] + r
				*arenaLen++
			}
			*descLen++
		} else {
			i := uint32(0)
			for i < blocks {
				k := keys[i]
				j := i + 1
				for j < blocks && keys[j] == k {
					j++
				}
				s.desc[*descLen] = desc{key: k, packed: pack(uint32(*arenaLen), j-i)}
				for x := i; x < j; x++ {
					s.arena[*arenaLen] = order[x] + r
					*arenaLen++
				}
				*descLen++
				i = j
			}
		}
		if rel == 0 {
			break
		}
		col := r - 1
		for x := uint32(0); x < blocks; x++ {
			c := uint32(t[int(order[x])+int(col)])
			keys[x] = (c << keyShift) | (keys[x] >> 8)
		}
		if constant[col] == 0 {
			columnStepKeys(order, keys[:], s.order2, blocks)
		}
	}
	return true
}

func sortDesc(a, b []desc, count int) []desc {
	shift := [rpass]int{0, rbits}
	var hist [rpass][rbins]uint32
	for i := 0; i < count; i++ {
		k := a[i].key
		for p := 0; p < rpass; p++ {
			hist[p][(k>>shift[p])&rmask]++
		}
	}
	for p := 0; p < rpass; p++ {
		sum := uint32(0)
		for v := 0; v < rbins; v++ {
			c := hist[p][v]
			hist[p][v] = sum
			sum += c
		}
	}
	for p := 0; p < rpass; p++ {
		sh := shift[p]
		for i := 0; i < count; i++ {
			slot := hist[p][(a[i].key>>sh)&rmask]
			hist[p][(a[i].key>>sh)&rmask]++
			b[int(slot)] = a[i]
		}
		a, b = b, a
	}
	return a
}

func blockDiff(a, b []byte) int {
	diff := 0
	for i := 0; i < blockSize && diff <= runSplit; i++ {
		if a[i] != b[i] {
			diff++
		}
	}
	return diff
}

// SuffixArray writes the suffix array of text into sa[0:len(text)].
// Returns false when it declines, and the caller must use another sorter.
func SuffixArray(text []byte, sa []int32) bool {
	n := len(text)
	if n <= 0 || len(sa) < n {
		return false
	}
	s := getScratch(n, n/2+blockSize+8)
	defer pool.Put(s)

retry:
	fullBlocks := n / blockSize
	arenaLen, descLen := 0, 0

	g := 0
	for g < fullBlocks {
		length := 1
		for length < runMax && g+length < fullBlocks {
			a := text[(g+length-1)*blockSize:]
			b := text[(g+length)*blockSize:]
			if blockDiff(a, b) > runSplit {
				break
			}
			length++
		}
		if !emitRun(text, n, uint32(g), uint32(length), s, &arenaLen, &descLen) {
			full := n + blockSize + 8
			if s.dcap >= full {
				return false
			}
			s = getScratch(n, full)
			goto retry
		}
		g += length
	}

	for p := fullBlocks * blockSize; p < n; p++ {
		s.desc[descLen] = desc{key: key32(text, n, p), packed: pack(uint32(arenaLen), 1)}
		s.arena[arenaLen] = uint32(p)
		descLen++
		arenaLen++
	}
	if arenaLen != n {
		return false
	}

	ds := sortDesc(s.desc[:descLen], s.desc2[:descLen], descLen)

	out := 0
	i := 0
	for i < descLen {
		d0 := ds[i]
		if i+1 == descLen || ds[i+1].key != d0.key {
			src := s.arena[off(d0):]
			ln := int(dlen(d0))
			for z := 0; z < ln; z++ {
				sa[out+z] = int32(src[z])
			}
			out += ln
			i++
			continue
		}
		j := i + 1
		for j < descLen && ds[j].key == ds[i].key {
			j++
		}
		need := 0
		for k := i; k < j; k++ {
			need += int(dlen(ds[k]))
		}
		if need > groupCap || (j-i) > groupCap {
			return false
		}
		a, b := s.merge, s.merge2
		ba, bb := s.bnd, s.bnd2
		nlist, total := 0, 0
		for k := i; k < j; k++ {
			d := ds[k]
			ba[nlist] = uint32(total)
			nlist++
			dl := int(dlen(d))
			copy(a[total:total+dl], s.arena[int(off(d)):int(off(d))+dl])
			total += dl
		}
		ba[nlist] = uint32(total)
		for nlist > 1 {
			outLists, pos := 0, 0
			for l := 0; l < nlist; l += 2 {
				bb[outLists] = uint32(pos)
				outLists++
				if l+1 == nlist {
					s0, e0 := ba[l], ba[l+1]
					copy(b[pos:], a[s0:e0])
					pos += int(e0 - s0)
				} else {
					p0, e0 := ba[l], ba[l+1]
					p1, e1 := ba[l+1], ba[l+2]
					for p0 < e0 && p1 < e1 {
						if suffixLessFrom(text, n, a[p1], a[p0], keyBytes) {
							b[pos] = a[p1]
							p1++
						} else {
							b[pos] = a[p0]
							p0++
						}
						pos++
					}
					for p0 < e0 {
						b[pos] = a[p0]
						pos++
						p0++
					}
					for p1 < e1 {
						b[pos] = a[p1]
						pos++
						p1++
					}
				}
			}
			bb[outLists] = uint32(pos)
			a, b = b, a
			ba, bb = bb, ba
			nlist = outLists
		}
		for z := 0; z < total; z++ {
			sa[out+z] = int32(a[z])
		}
		out += total
		i = j
	}
	return out == n
}
