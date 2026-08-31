package main

// The GPU mining worker.
//
// It is one more worker beside the CPU threads, sharing the same job, target
// and submit path. Two things make it different from a CPU thread:
//
//   - It hashes a whole batch per call (thousands of nonces), so it can only
//     notice a new job between batches. The batch size is therefore a latency
//     knob, not just a throughput one.
//   - It keeps a second batch queued behind the running one. A card that is
//     only handed its next batch after the host has woken up, read the last
//     one's results and re-enqueued is idle for the whole of that, and on a
//     machine mining on every core the wake-up alone is a scheduler quantum.
//     That is why the GPU used to speed up when CPU threads were taken away.
//     With one batch queued behind the other, the card starts the next the
//     instant the current one ends and the host has a full batch of slack.
//   - Its nonce space is kept disjoint from the CPU threads'. Byte 47 of the
//     miniblock is the CPU's thread id; GPUs take 0xf0 upwards, so no CPU
//     thread and no other GPU can ever hash the same input.
//
// Every winning nonce is re-hashed on the CPU before it is submitted. A GPU
// that is subtly wrong (bad overclock, failing memory) would otherwise poison
// the share stream, and one CPU hash per share is free at any real difficulty.

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"runtime"
	"sort"
	"sync/atomic"
	"time"

	"github.com/deroproject/derohe/astrobwt/astrobwtv3"
	"github.com/deroproject/derohe/block"
)

// maxGPUs bounds the per-device hash counters. Nonce-space byte 47 gives GPUs
// the range 0xf0..0xff, so this cannot exceed 16, and 16 is what it is: mining
// rigs routinely carry twelve cards and HiveOS will happily boot more. At 8 a
// twelve-card rig started twelve workers, four of which returned immediately on
// the bounds check below while the console went on reporting twelve -- four
// cards idle and nothing saying so.
//
// The ceiling is real and cannot be lifted here: CPU threads tag their nonces
// with their thread id, so the GPU range has to stay above any plausible thread
// count, and 0xf0 is where that line was drawn. Past 16 devices the tagging
// scheme itself has to change.
const maxGPUs = 16

// gpuWorkSize is the miniblock the GPU library expects, and must equal the
// DSG_WORK_SIZE the kernels are compiled with.
const gpuWorkSize = block.MINIBLOCK_SIZE

// Raising this thread's priority was the other half of the same idea and is
// deliberately absent. Measured on a 5080 with every core busy it is not one
// trade but two opposite ones: +4.8% at 1,024 nonces a batch, where the wake-up
// is a real share of the batch, and -4.0% at the default 32,768, where it is
// not and the preemption costs more than it buys. The default is the case that
// matters, so the pipeline does the work and the scheduler is left alone.
//
// gpuPipelineDepth is how many batches are kept in flight. Two is what the
// library allocates slots for (DSG_SLOTS), and two is all that is wanted: one
// running on the card and one queued behind it. A third would buy no more
// slack and would add a batch to how long a new job takes to reach the card.
const gpuPipelineDepth = 2

// gpuNonceTag is byte 47 for GPU device i. CPU threads use their thread id,
// which is far below this in any real configuration.
func gpuNonceTag(device int) byte { return byte(0xf0 + device) }

// verifyGPU hashes a few nonces on both sides and reports the first
// disagreement. Cheap insurance: it runs once per device at start-up and
// catches a broken build, a bad overclock or failing VRAM before a single
// share is submitted.
func verifyGPU(g *GPUContext, work []byte, scratch *astrobwtv3.ScratchData) error {
	var probe [block.MINIBLOCK_SIZE]byte
	copy(probe[:], work)

	for _, nonce := range []uint32{0, 1, 0x5f3759df, 0xffffffff} {
		binary.BigEndian.PutUint32(probe[block.MINIBLOCK_SIZE-5:], nonce)

		got, err := g.HashOne(probe[:], nonce)
		if err != nil {
			return err
		}
		want := astrobwtv3.AstroBWTv3_scratch(probe[:], scratch)
		if got != want {
			return &gpuMismatch{nonce: nonce, got: got, want: want}
		}
	}
	return nil
}

type gpuMismatch struct {
	nonce     uint32
	got, want [32]byte
}

func (e *gpuMismatch) Error() string {
	return "nonce " + hex.EncodeToString([]byte{
		byte(e.nonce >> 24), byte(e.nonce >> 16), byte(e.nonce >> 8), byte(e.nonce),
	}) + ": gpu " + hex.EncodeToString(e.got[:8]) + "... but cpu " + hex.EncodeToString(e.want[:8]) + "..."
}

// RunGPUMiner drives one CUDA device until stop or quit is closed. batch is the
// nonces per launch; 0 lets the library size it from free VRAM. blocks pins the
// suffix kernel's resident block count; 0 measures it (see gpu_tune.go).
func (e *Engine) RunGPUMiner(device, batch, blocks int, stop <-chan struct{}) {
	if device < 0 || device >= maxGPUs {
		e.post(LogError, "gpu", "device %d is out of range", device)
		return
	}

	// CUDA keeps its current device per OS thread, so this goroutine must not
	// be allowed to migrate between them mid-batch.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()


	g, err := NewGPUContext(device, batch, blocks)
	if err != nil {
		e.post(LogError, "gpu", "%v", err)
		return
	}
	defer g.Close()
	e.post(LogGood, "gpu", "device %d: %s, %d hashes per batch", device, g.Name(), g.Batch())

	tuner := newBlockTuner(g, device, blocks, e.post)

	// noteSettled clears the engine's tuning flag the moment the sweep ends, so
	// the console stops saying TUNING GPU without having to poll for it. It
	// stays nil when there is no sweep to wait for.
	var noteSettled func()
	if tuner != nil {
		atomic.AddInt32(&e.gputuning, 1)
		tuning := true
		release := func() {
			if tuning {
				atomic.AddInt32(&e.gputuning, -1)
				tuning = false
			}
		}
		// Also on the way out, so a worker that stops mid-sweep cannot leave
		// the console saying TUNING GPU with no device left to tune.
		defer release()
		noteSettled = func() {
			if !tuner.Tuning() {
				release()
			}
		}
	}

	scratch := astrobwtv3.Pool.Get().(*astrobwtv3.ScratchData)
	defer astrobwtv3.Pool.Put(scratch)

	hashes := &e.gpuCounters[device].n

	var diff big.Int
	var work [block.MINIBLOCK_SIZE]byte
	var randomBuf [12]byte
	rand.Read(randomBuf[:])

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

	nonce := uint32(0)
	verified := false

	for !done() {
		myjob := e.Job()
		localJobCounter := atomic.LoadInt64(&e.jobCounter)

		if myjob.Blockhashing_blob == "" {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		n, err := hex.Decode(work[:], []byte(myjob.Blockhashing_blob))
		if err != nil || n != block.MINIBLOCK_SIZE {
			time.Sleep(time.Second)
			continue
		}

		copy(work[block.MINIBLOCK_SIZE-12:], randomBuf[:])
		work[block.MINIBLOCK_SIZE-1] = gpuNonceTag(device)

		if _, ok := diff.SetString(myjob.Difficulty, 10); !ok || diff.Sign() <= 0 {
			time.Sleep(time.Second)
			continue
		}
		target := NewTarget(&diff)

		if work[0]&0xf != 1 {
			time.Sleep(time.Second)
			continue
		}

		// Prove the device before trusting it, on the first real job so the
		// probe uses the same shape of input mining will.
		if !verified {
			if err := verifyGPU(g, work[:], scratch); err != nil {
				e.post(LogError, "gpu", "device %d disagrees with the CPU (%v) — not mining on it", device, err)
				return
			}
			e.post(LogGood, "gpu", "device %d verified against the CPU", device)
			verified = true
		}

		// take is one collected batch: count it, time it, and turn any winning
		// nonce into a share. Shared by the mining loop and the drain below, so
		// a batch that was already on the card when the job changed is still
		// checked rather than thrown away.
		//
		// Batches are timed collect-to-collect rather than around the call. In
		// a full pipeline the call itself is only the part of a batch that was
		// left when the host got round to waiting, so the gap between two
		// collections is what a batch actually takes. The first collection
		// after priming has no previous one to measure from and is skipped.
		timed := false
		lastDone := time.Now()
		take := func() bool {
			hits, err := g.Collect()
			if err != nil {
				e.post(LogError, "gpu", "device %d: %v — stopping", device, err)
				return false
			}
			atomic.AddUint64(hashes, uint64(g.Batch()))

			now := time.Now()
			if timed {
				tuner.observe(now.Sub(lastDone), g.Batch())
			}
			lastDone, timed = now, true
			if noteSettled != nil {
				noteSettled()
			}

			for _, hit := range hits {
				binary.BigEndian.PutUint32(work[block.MINIBLOCK_SIZE-5:], hit)
				powhash := astrobwtv3.AstroBWTv3_scratch(work[:], scratch)
				if target.Meets(&powhash) {
					e.submit(myjob, work[:], &powhash)
				} else {
					// The GPU claimed a share the CPU does not agree with. That
					// is a hardware fault, not a rounding difference, so say so
					// loudly rather than silently dropping it.
					e.post(LogWarn, "gpu", "device %d produced a share the CPU rejects — check clocks", device)
				}
			}
			return true
		}

		failed := false
		for localJobCounter == atomic.LoadInt64(&e.jobCounter) && !done() {
			// Fill the queue before waiting on it. The submit is what keeps the
			// card fed; the collect below is where this thread sleeps.
			for g.InFlight() < gpuPipelineDepth {
				if err := g.Submit(work[:], nonce, &target); err != nil {
					e.post(LogError, "gpu", "device %d: %v — stopping", device, err)
					failed = true
					break
				}
				nonce += uint32(g.Batch())
			}
			if failed || !take() {
				failed = true
				break
			}
		}

		// Whatever is still on the card was submitted against this job and this
		// work, so it is worth collecting rather than abandoning: a share found
		// in it is still a share. Nothing is lost by waiting either — closing
		// the context waits for the same kernels.
		for !failed && g.InFlight() > 0 {
			if !take() {
				break
			}
		}
		if failed {
			// Drain quietly so Close is not left tidying up after an error.
			for g.InFlight() > 0 {
				if _, err := g.Collect(); err != nil {
					break
				}
			}
			return
		}
	}
}

// SetGPUs starts or stops GPU workers. Devices are addressed by index, and a
// device already running is left alone.
func (e *Engine) SetGPUs(devices []int, batch, blocks int) {
	e.gmu.Lock()
	defer e.gmu.Unlock()

	want := map[int]bool{}
	for _, d := range devices {
		want[d] = true
	}
	for d, stop := range e.gpuWorkers {
		if !want[d] {
			close(stop)
			delete(e.gpuWorkers, d)
		}
	}
	for _, d := range devices {
		if _, running := e.gpuWorkers[d]; running {
			continue
		}
		// Refuse here rather than in the worker. RunGPUMiner also checks, but
		// by then the device is in gpuWorkers and counted, so the console
		// reported a card that had already given up.
		if d < 0 || d >= maxGPUs {
			e.post(LogError, "gpu", "device %d is past the %d this build supports and is not being mined", d, maxGPUs)
			continue
		}
		stop := make(chan struct{})
		e.gpuWorkers[d] = stop
		go e.RunGPUMiner(d, batch, blocks, stop)
	}
	atomic.StoreInt32(&e.ngpu, int32(len(e.gpuWorkers)))
}

// GPUs returns how many GPU workers are running.
func (e *Engine) GPUs() int { return int(atomic.LoadInt32(&e.ngpu)) }

// GPUTuning reports whether any device is still choosing its block count.
func (e *Engine) GPUTuning() bool { return atomic.LoadInt32(&e.gputuning) > 0 }

// GPUHashes sums the per-device counters, so the console can show the GPU
// contribution separately from the CPU one.
func (e *Engine) GPUHashes() (t uint64) {
	for i := 0; i < maxGPUs; i++ {
		t += atomic.LoadUint64(&e.gpuCounters[i].n)
	}
	return
}

// GPUHashesFor is one device's counter. The console keeps a rate window per
// device from this, which is what lets a six-card rig show which card stopped
// rather than only that the total dropped.
func (e *Engine) GPUHashesFor(device int) uint64 {
	if device < 0 || device >= maxGPUs {
		return 0
	}
	return atomic.LoadUint64(&e.gpuCounters[device].n)
}

// GPUDeviceList is the indices of the workers running right now, in order.
// Taken under the same lock SetGPUs writes under, so the console can never see
// a half-applied change.
func (e *Engine) GPUDeviceList() []int {
	e.gmu.Lock()
	defer e.gmu.Unlock()
	out := make([]int, 0, len(e.gpuWorkers))
	for d := range e.gpuWorkers {
		out = append(out, d)
	}
	sort.Ints(out)
	return out
}
