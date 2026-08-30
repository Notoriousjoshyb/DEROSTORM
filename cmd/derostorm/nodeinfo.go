package main

// Everything the dashboard shows about the chain that getwork does not carry.
//
// The getwork socket is deliberately thin: a job, a difficulty, a height and
// the counters for this miner's own shares. It says nothing about how many
// peers the node has, what the network is hashing at, or how long a block is
// taking. Those come from derod's JSON-RPC, which is a different port and may
// not be reachable at all -- a public getwork endpoint usually only exposes
// 10100 and nothing else.
//
// So this whole file is best-effort by design. It probes for an RPC endpoint
// once, uses it if it answers, and reports "not available" forever after if it
// does not. Nothing else in the program waits on it, and every field it fills
// in is drawn as "--" when it has not.

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/deroproject/derohe/rpc"
)

// NodeInfo is the last successful GetInfo, plus how long it took to arrive.
type NodeInfo struct {
	OK bool // false until a poll has succeeded; every field below is then unset
	At time.Time

	// Latency is the round trip of the RPC call. It is a real measurement of a
	// real request to this node, which is the closest thing to a ping that a
	// program speaking only websockets and HTTP can honestly report.
	Latency time.Duration

	Peers      int
	PeersIn    int
	PeersOut   int
	Height     int64
	TopoHeight int64
	Difficulty uint64
	// BlockTime is the chain's target seconds per block, shown next to the
	// measured average so a stalling chain is visible.
	BlockTime      uint64
	AvgBlockTime50 float64
	Version        string
	Network        string
	Miners         int
	MiniInMemory   int
	TxPool         uint64

	// Endpoint is the URL that answered, for the network screen. Worth showing:
	// when it is empty the reason half the panel says "--" is that no RPC
	// endpoint was found, and that is not otherwise guessable.
	Endpoint string
}

// NetHashrate is the network's hashrate in H/s, and whether it could be
// computed.
//
// It is the difficulty, unchanged. That looks like a missing division, so it
// is worth saying why there is none: DERO's difficulty is already denominated
// in hashes per second, not in hashes per block. derod agrees with itself on
// this -- Get_Network_HashRate() in blockchain.go is a one-line return of
// Get_Difficulty(), and config.go labels a bootstrap difficulty of 10,000,000
// as "10 MH/s". Dividing by the eighteen-second block target, as a chain with
// per-block difficulty would need, reported the network eighteen times slower
// than it was.
func (n NodeInfo) NetHashrate() (float64, bool) {
	if !n.OK || n.Difficulty == 0 {
		return 0, false
	}
	return float64(n.Difficulty), true
}

// NodeWatcher polls derod's JSON-RPC in the background.
type NodeWatcher struct {
	mu   sync.RWMutex
	cur  NodeInfo
	note string

	client    *http.Client
	endpoints []string
	closed    chan struct{}
	once      sync.Once
}

// nodeInfoInterval is how often the node is asked. Peer counts and network
// difficulty move on the scale of a block, not a frame.
const nodeInfoInterval = 5 * time.Second

// nodeInfoRetry is how long to wait before probing again once every candidate
// endpoint has failed. Long, because the usual reason for failure is that the
// node simply does not expose RPC, and that will not change while the miner
// runs.
const nodeInfoRetry = 2 * time.Minute

// NewNodeWatcher starts polling. node is the getwork address from the config;
// rpcOverride is an explicit RPC address, used as-is when it is set.
func NewNodeWatcher(node, rpcOverride string) *NodeWatcher {
	w := &NodeWatcher{
		closed:    make(chan struct{}),
		endpoints: rpcCandidates(node, rpcOverride),
		client: &http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				// Same posture as the getwork dialer: a miner's node is often
				// reached over a self-signed certificate, and refusing to talk
				// to it would take away the panel rather than protect anything
				// -- nothing secret is sent, and nothing here is trusted with
				// a decision.
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
				MaxIdleConnsPerHost: 1,
			},
		},
	}
	go w.loop()
	return w
}

func (w *NodeWatcher) loop() {
	// Probe immediately; a first frame with peer counts in it is worth the one
	// request, and if there is no endpoint the answer is cached for two
	// minutes.
	w.poll()
	t := time.NewTicker(nodeInfoInterval)
	defer t.Stop()
	lastFail := time.Time{}
	for {
		select {
		case <-w.closed:
			return
		case <-t.C:
			w.mu.RLock()
			ok := w.cur.OK
			w.mu.RUnlock()
			if !ok && time.Since(lastFail) < nodeInfoRetry {
				continue
			}
			if !w.poll() {
				lastFail = time.Now()
			}
		}
	}
}

func (w *NodeWatcher) Info() NodeInfo {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cur
}

// Note is the one line worth putting in the event log at start-up: which
// endpoint answered, or that none did and what that costs.
func (w *NodeWatcher) Note() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.note
}

func (w *NodeWatcher) Close() { w.once.Do(func() { close(w.closed) }) }

func (w *NodeWatcher) poll() bool {
	// A known-good endpoint is tried alone; only a cold start walks the list.
	w.mu.RLock()
	known := w.cur.Endpoint
	w.mu.RUnlock()

	list := w.endpoints
	if known != "" {
		list = []string{known}
	}

	for _, url := range list {
		info, err := w.getInfo(url)
		if err != nil {
			continue
		}
		info.Endpoint = url
		w.mu.Lock()
		w.cur = info
		if w.note == "" || !strings.HasPrefix(w.note, "chain detail") {
			w.note = "chain detail from " + url
		}
		w.mu.Unlock()
		return true
	}

	w.mu.Lock()
	// Keep the last good sample rather than blanking the panel on one dropped
	// request; only a cold start with nothing to keep reports unavailable.
	if !w.cur.OK && w.note == "" {
		w.note = "no derod RPC endpoint answered — peer count, network hashrate and block interval will show " + noteNA
	}
	w.mu.Unlock()
	return false
}

const noteNA = "--"

func (w *NodeWatcher) getInfo(url string) (NodeInfo, error) {
	body := []byte(`{"jsonrpc":"2.0","id":"1","method":"DERO.GetInfo"}`)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return NodeInfo{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := w.client.Do(req)
	if err != nil {
		return NodeInfo{}, err
	}
	defer resp.Body.Close()
	// A bounded read: this is an untrusted endpoint and an unbounded one would
	// let a misconfigured host hand the miner a gigabyte of anything.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return NodeInfo{}, err
	}
	latency := time.Since(start)

	var env struct {
		Result rpc.GetInfo_Result `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return NodeInfo{}, err
	}
	if env.Error != nil {
		return NodeInfo{}, fmt.Errorf("%s", env.Error.Message)
	}
	r := env.Result
	if r.Height == 0 && r.Version == "" {
		return NodeInfo{}, fmt.Errorf("not a derod RPC endpoint")
	}

	return NodeInfo{
		OK:             true,
		At:             time.Now(),
		Latency:        latency,
		PeersIn:        int(r.Incoming_connections_count),
		PeersOut:       int(r.Outgoing_connections_count),
		Peers:          int(r.Incoming_connections_count + r.Outgoing_connections_count),
		Height:         r.Height,
		TopoHeight:     r.TopoHeight,
		Difficulty:     r.Difficulty,
		BlockTime:      r.Target,
		AvgBlockTime50: float64(r.AverageBlockTime50),
		Version:        r.Version,
		Network:        r.Network,
		Miners:         r.Miners,
		MiniInMemory:   r.Miniblocks_In_Memory,
		TxPool:         r.Tx_pool_size,
	}, nil
}

// rpcCandidates builds the list of URLs to try, best guess first.
//
// derod puts getwork on one port and JSON-RPC two below it -- 10100 and 10102
// on mainnet, 40100 and 40102 on testnet -- so a miner pointed at a getwork
// port on a machine that also serves RPC can usually be found by arithmetic.
// Both schemes are tried because a node behind a reverse proxy is https and a
// node on the same LAN generally is not.
func rpcCandidates(node, override string) []string {
	if override != "" {
		return withSchemes(override)
	}
	host, port, err := net.SplitHostPort(strings.TrimSpace(node))
	if err != nil {
		return withSchemes(node)
	}
	var out []string
	if p, err := strconv.Atoi(port); err == nil {
		out = append(out, withSchemes(net.JoinHostPort(host, strconv.Itoa(p+2)))...)
	}
	// The configured port itself, last: some setups do put both on one port,
	// and it costs one failed request on a cold start to find out.
	return append(out, withSchemes(net.JoinHostPort(host, port))...)
}

func withSchemes(hostPort string) []string {
	hostPort = strings.TrimSpace(hostPort)
	if strings.HasPrefix(hostPort, "http://") || strings.HasPrefix(hostPort, "https://") {
		return []string{strings.TrimRight(hostPort, "/") + "/json_rpc"}
	}
	return []string{
		"http://" + hostPort + "/json_rpc",
		"https://" + hostPort + "/json_rpc",
	}
}
