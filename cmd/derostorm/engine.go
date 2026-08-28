package main

// The mining engine. This is the optimised DERO hot path: a thread-owned
// AstroBWTv3 scratch buffer, a per-job precomputed target, and per-thread hash
// counters on their own cache lines. Nothing in here changes what is hashed or
// how the result is judged -- see target.go for why the fast difficulty check
// is exactly equivalent to the reference one.

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/url"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/deroproject/derohe/astrobwt/astrobwt_fast"
	"github.com/deroproject/derohe/astrobwt/astrobwtv3"
	"github.com/deroproject/derohe/block"
	"github.com/deroproject/derohe/globals"
	"github.com/deroproject/derohe/rpc"
	"github.com/gorilla/websocket"
)

const maxThreads = 256

// paddedCounter keeps each thread's hash count on its own cache line. The
// upstream miner did an atomic increment on one shared word per hash, which
// puts every core in contention for a single line inside the hot loop.
type paddedCounter struct {
	n uint64
	_ [56]byte
}

// Engine runs getwork plus the mining threads and exposes a consistent view of
// its state to the console.
type Engine struct {
	node   string
	wallet string

	mu         sync.RWMutex
	job        rpc.GetBlockTemplate_Result
	jobCounter int64

	counters [maxThreads]paddedCounter

	// GPU workers are counted separately so the console can attribute the
	// hashrate. gpuWorkers is keyed by device index; see gpu_worker.go.
	gpuCounters [maxGPUs]paddedCounter
	gmu         sync.Mutex
	gpuWorkers  map[int]chan struct{}
	ngpu        int32

	// gputuning is how many devices are still measuring their block count. The
	// console shows a different headline while it is non-zero: the hashrate
	// during a sweep is not the hashrate the machine settles at, and reading it
	// as one would be the wrong lesson to draw from a first run.
	gputuning int32

	state   int32
	nthread int32

	// workers[i] is the stop channel for the thread occupying slot i. Slots are
	// added and removed from the end so a running worker never changes slot,
	// which keeps its CPU pinning and its counter stable.
	wmu     sync.Mutex
	workers []chan struct{}

	conn   *websocket.Conn
	connMu sync.Mutex

	events chan LogEntry
	quit   chan struct{}
	once   sync.Once
}

func NewEngine(node, wallet string) *Engine {
	return &Engine{
		node:       node,
		wallet:     wallet,
		events:     make(chan LogEntry, 256),
		quit:       make(chan struct{}),
		gpuWorkers: make(map[int]chan struct{}),
	}
}

func (e *Engine) Events() <-chan LogEntry { return e.events }

func (e *Engine) Stop() {
	e.once.Do(func() {
		close(e.quit)
	})
}

// Threads returns the number of mining threads currently running.
func (e *Engine) Threads() int { return int(atomic.LoadInt32(&e.nthread)) }

// SetThreads grows or shrinks the mining pool at runtime. Growing starts new
// workers on the free slots; shrinking signals the highest slots to finish
// their current job iteration and exit.
func (e *Engine) SetThreads(n int) error {
	if n < 1 || n > maxThreads {
		return fmt.Errorf("threads must be between 1 and %d", maxThreads)
	}
	e.wmu.Lock()
	defer e.wmu.Unlock()

	for len(e.workers) < n {
		slot := len(e.workers)
		stop := make(chan struct{})
		e.workers = append(e.workers, stop)
		go e.RunMiner(slot, stop)
	}
	for len(e.workers) > n {
		last := len(e.workers) - 1
		close(e.workers[last])
		e.workers = e.workers[:last]
	}

	atomic.StoreInt32(&e.nthread, int32(n))
	return nil
}

func (e *Engine) stopping() bool {
	select {
	case <-e.quit:
		return true
	default:
		return false
	}
}

func (e *Engine) post(level LogLevel, tag, format string, args ...interface{}) {
	select {
	case e.events <- LogEntry{At: time.Now(), Level: level, Tag: tag, Text: fmt.Sprintf(format, args...)}:
	default: // console is behind; drop rather than stall a mining thread
	}
}

func (e *Engine) setState(s MinerState) { atomic.StoreInt32(&e.state, int32(s)) }
func (e *Engine) State() MinerState     { return MinerState(atomic.LoadInt32(&e.state)) }

// TotalHashes sums the per-thread counters, plus any GPU workers. Called once
// per console tick, which is the only place the total is needed.
func (e *Engine) TotalHashes() (t uint64) {
	for i := 0; i < maxThreads; i++ {
		t += atomic.LoadUint64(&e.counters[i].n)
	}
	return t + e.GPUHashes()
}

// CPUHashes is TotalHashes without the GPU contribution.
func (e *Engine) CPUHashes() (t uint64) {
	for i := 0; i < maxThreads; i++ {
		t += atomic.LoadUint64(&e.counters[i].n)
	}
	return
}

// Job returns a copy of the current work.
func (e *Engine) Job() rpc.GetBlockTemplate_Result {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.job
}

// ---------------------------------------------------------------- getwork

func (e *Engine) RunGetWork() {
	first := true
	for !e.stopping() {
		if first {
			e.setState(StateConnecting)
			e.post(LogInfo, "connect", "dialing %s", e.node)
			first = false
		} else {
			e.setState(StateReconnecting)
		}

		u := url.URL{Scheme: "wss", Host: e.node, Path: "/ws/" + e.wallet}
		dialer := *websocket.DefaultDialer
		dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

		conn, _, err := dialer.Dial(u.String(), nil)
		if err != nil {
			e.post(LogError, "connect", "%v — retrying in 10s", err)
			select {
			case <-time.After(10 * time.Second):
			case <-e.quit:
				return
			}
			continue
		}

		e.connMu.Lock()
		e.conn = conn
		e.connMu.Unlock()
		e.post(LogGood, "connect", "connected to %s", e.node)

		for {
			var result rpc.GetBlockTemplate_Result
			if err := conn.ReadJSON(&result); err != nil {
				if e.stopping() {
					return
				}
				e.setState(StateReconnecting)
				e.post(LogWarn, "connect", "lost connection: %v", err)
				break
			}

			e.mu.Lock()
			prevHeight := e.job.Height
			prevMini := e.job.MiniBlocks
			prevBlocks := e.job.Blocks
			prevRejected := e.job.Rejected
			e.job = result
			atomic.AddInt64(&e.jobCounter, 1)
			e.mu.Unlock()

			e.setState(StateMining)

			if result.LastError != "" {
				e.post(LogError, "node", "%s", result.LastError)
			}
			if result.Height != prevHeight {
				e.post(LogInfo, "job", "height %d · difficulty %s", result.Height, result.Difficulty)
			}
			if result.MiniBlocks > prevMini {
				e.post(LogGood, "accepted", "miniblock %d at height %d", result.MiniBlocks, result.Height)
			}
			if result.Blocks > prevBlocks {
				e.post(LogGood, "block", "block %d found at height %d", result.Blocks, result.Height)
			}
			if result.Rejected > prevRejected {
				e.post(LogWarn, "rejected", "share rejected (total %d)", result.Rejected)
			}
		}
	}
}

// ---------------------------------------------------------------- mining

func (e *Engine) RunMiner(tid int, stop <-chan struct{}) {
	var diff big.Int
	var work [block.MINIBLOCK_SIZE]byte
	var workB [block.MINIBLOCK_SIZE]byte
	var randomBuf [12]byte
	rand.Read(randomBuf[:])

	// Owned by this thread for its whole life: no pool round trip per hash, and
	// it stays resident in this core's caches.
	scratchV3 := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
	defer astrobwtv3.Pool.Put(scratchV3)

	// The second scratch, and the second work buffer above, exist only for the
	// paired path: two nonces reach the final SHA-256 together, so both suffix
	// arrays have to be live at once. Taken only when the pairing is available,
	// because it is ~350 KB a thread and there is no point holding it otherwise.
	paired := PairedSHAAvailable()
	var scratchV3B *astrobwtv3.ScratchData
	if paired {
		scratchV3B = astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
		defer astrobwtv3.Pool.Put(scratchV3B)
	}

	// The pre-HF2 hash needs its own scratch, and HF2 was at height 481,600 on
	// mainnet -- millions of blocks ago -- so on any live chain this is never
	// touched. Taken on first use instead of up front, which keeps a buffer per
	// thread out of the miner's footprint for the case that will not happen.
	var scratchFast *astrobwt_fast.ScratchData
	defer func() {
		if scratchFast != nil {
			astrobwt_fast.Pool.Put(scratchFast)
		}
	}()

	hashes := &e.counters[tid].n

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pinToCPU(tid)

	// done is checked in the innermost loop, so it must be as cheap as a
	// channel receive can be: a non-blocking select on a never-written channel.
	done := func() bool {
		select {
		case <-stop:
			return true
		case <-e.quit:
			return true
		default:
			return false
		}
	}

	nonceBuf := work[block.MINIBLOCK_SIZE-5:]
	nonceBufB := workB[block.MINIBLOCK_SIZE-5:]
	i := uint32(0)

	for !done() {
		myjob := e.Job()
		localJobCounter := atomic.LoadInt64(&e.jobCounter)

		if myjob.Blockhashing_blob == "" {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		n, err := hex.Decode(work[:], []byte(myjob.Blockhashing_blob))
		if err != nil || n != block.MINIBLOCK_SIZE {
			if tid == 0 {
				e.post(LogError, "job", "cannot decode blockwork (%d bytes)", n)
			}
			time.Sleep(time.Second)
			continue
		}

		height := binary.BigEndian.Uint64(work[0:]) & 0x000000ffffffffff

		copy(work[block.MINIBLOCK_SIZE-12:], randomBuf[:]) // extra randomisation
		work[block.MINIBLOCK_SIZE-1] = byte(tid)

		if _, ok := diff.SetString(myjob.Difficulty, 10); !ok || diff.Sign() <= 0 {
			time.Sleep(time.Second)
			continue
		}
		// The target is fixed for the life of this job: compute it once here
		// rather than dividing 2^256 by the difficulty on every attempt.
		target := NewTarget(&diff)

		if work[0]&0xf != 1 { // version check
			if tid == 0 {
				e.post(LogError, "job", "unknown blockwork version %d — check for updates", work[0]&0x1f)
			}
			time.Sleep(time.Second)
			continue
		}

		if int64(height) < globals.Config.MAJOR_HF2_HEIGHT {
			if scratchFast == nil {
				scratchFast = astrobwt_fast.Pool.Get().(*astrobwt_fast.ScratchData)
			}
			for localJobCounter == atomic.LoadInt64(&e.jobCounter) && !done() {
				i++
				binary.BigEndian.PutUint32(nonceBuf, i)
				powhash := astrobwt_fast.POW_optimized(work[:], scratchFast)
				*hashes++
				if target.Meets(&powhash) {
					e.submit(myjob, work[:])
				}
			}
		} else if paired {
			// Two nonces at a time, so their final SHA-256s can be interleaved.
			// That is worth 44% on the SHA-256 and ~10% on the hash; see
			// astrobwtv3/sha_hook.go. The second work buffer differs from the
			// first only in the nonce.
			copy(workB[:], work[:])
			for localJobCounter == atomic.LoadInt64(&e.jobCounter) && !done() {
				i++
				binary.BigEndian.PutUint32(nonceBuf, i)
				i++
				binary.BigEndian.PutUint32(nonceBufB, i)
				hashA, hashB := astrobwtv3.AstroBWTv3_pair(work[:], workB[:], scratchV3, scratchV3B)
				*hashes += 2
				if target.Meets(&hashA) {
					e.submit(myjob, work[:])
				}
				if target.Meets(&hashB) {
					e.submit(myjob, workB[:])
				}
			}
		} else {
			for localJobCounter == atomic.LoadInt64(&e.jobCounter) && !done() {
				i++
				binary.BigEndian.PutUint32(nonceBuf, i)
				powhash := astrobwtv3.AstroBWTv3_scratch(work[:], scratchV3)
				*hashes++
				if target.Meets(&powhash) {
					e.submit(myjob, work[:])
				}
			}
		}
	}
}

func (e *Engine) submit(job rpc.GetBlockTemplate_Result, work []byte) {
	defer globals.Recover(1)
	e.connMu.Lock()
	defer e.connMu.Unlock()
	if e.conn == nil {
		return
	}
	e.post(LogInfo, "submit", "share at height %d", job.Height)
	e.conn.WriteJSON(rpc.SubmitBlock_Params{
		JobID:                 job.JobID,
		MiniBlockhashing_blob: fmt.Sprintf("%x", work),
	})
}
