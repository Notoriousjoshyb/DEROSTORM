package main

// Tests for share accounting: a share that meets the target must always be
// counted, a share the node cannot receive must be counted as a failure and
// never vanish silently, and the audit must complain only on a real gap.

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/deroproject/derohe/rpc"
	"github.com/gorilla/websocket"
)

func auditJob(minis, blocks, rejected uint64) rpc.GetBlockTemplate_Result {
	return rpc.GetBlockTemplate_Result{
		Blockhashing_blob: "01", // non-empty: miners skip empty jobs
		MiniBlocks:        minis,
		Blocks:            blocks,
		Rejected:          rejected,
	}
}

// A share submitted with no connection at all is still counted as sent, is
// counted as a send failure, and must not panic.
func TestSubmitWithoutConnectionCountsAsFailure(t *testing.T) {
	e := NewEngine("node", "wallet")
	job := auditJob(0, 0, 0)
	var h [32]byte

	e.submit(job, []byte{1}, &h)

	if got := e.Submitted(); got != 1 {
		t.Fatalf("submitted = %d, want 1", got)
	}
	if got := e.SendFailures(); got != 1 {
		t.Fatalf("send failures = %d, want 1", got)
	}
}

// The audit stays quiet on a healthy connection: sent minus confirmed within
// the in-flight slack means nothing to say.
func TestShareAuditQuietWhenHealthy(t *testing.T) {
	e := NewEngine("node", "wallet")

	e.shareAudit(auditJob(0, 0, 0))  // first job rebases
	e.submitted = 25                 // 25 sent this connection
	e.shareAudit(auditJob(20, 3, 0)) // 23 confirmed — in flight, not a gap

	if e.auditLastNs != 0 {
		t.Fatalf("audit logged on a healthy connection")
	}
}

// A sustained gap — shares the node never heard about — must be flagged.
func TestShareAuditFlagsGap(t *testing.T) {
	e := NewEngine("node", "wallet")

	e.shareAudit(auditJob(0, 0, 0))
	e.submitted = 100 // sent 100, node confirmed 3, failures 0 → 97 vanished
	e.shareAudit(auditJob(3, 0, 0))

	if e.auditLastNs == 0 {
		t.Fatalf("audit did not flag a %d-share gap", 97)
	}
}

// Send failures are subtracted from the gap: a share that failed locally is
// accounted for, not mysterious.
func TestShareAuditCountsSendFailures(t *testing.T) {
	e := NewEngine("node", "wallet")

	e.shareAudit(auditJob(0, 0, 0))
	e.submitted = 100
	e.sendFailures = 97 // all 97 accounted for as local failures
	e.shareAudit(auditJob(3, 0, 0))

	if e.auditLastNs != 0 {
		t.Fatalf("audit flagged a gap that send failures already explained")
	}
}

// The audit is throttled: a second gap within the window does not log again.
func TestShareAuditThrottled(t *testing.T) {
	e := NewEngine("node", "wallet")

	e.shareAudit(auditJob(0, 0, 0))
	e.submitted = 100
	e.shareAudit(auditJob(3, 0, 0))
	first := e.auditLastNs
	if first == 0 {
		t.Fatalf("audit did not flag the first gap")
	}

	recent := time.Now().UnixNano() // pretend it logged again just now
	e.auditLastNs = recent
	e.submitted = 300
	e.shareAudit(auditJob(3, 0, 0))
	if e.auditLastNs != recent {
		t.Fatalf("audit logged twice inside the throttle window")
	}
}

// A dead-but-open socket must be detected by submit and counted as a failure,
// not silently dropped. This is the regression test for a 12-hour session that
// ended with 232 shares sent and 3 confirmed: writes to a socket nobody reads
// kept succeeding until the buffer filled, and every lost share was invisible.
func TestSubmitDetectsDeadSocket(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Reset the connection at the TCP level: the client's writes must fail
		// immediately rather than sit in a buffer, which is what the original
		// bug relied on.
		if tcp, ok := c.UnderlyingConn().(*net.TCPConn); ok {
			tcp.SetLinger(0) // Close sends RST instead of FIN
		}
		c.Close()
	}))
	defer srv.Close()

	e := NewEngine("node", "wallet")
	url := "ws" + srv.URL[len("http"):] // http→ws, https→wss
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	e.connMu.Lock()
	e.conn = conn
	e.connMu.Unlock()

	job := auditJob(0, 0, 0)
	var h [32]byte
	var blob [48]byte
	for i := 0; i < 100 && e.SendFailures() == 0; i++ {
		e.submit(job, blob[:], &h)
	}

	if got := e.SendFailures(); got == 0 {
		t.Fatalf("wrote shares into a reset socket and no failure was recorded")
	}
	if got := e.Submitted(); got == 0 {
		t.Fatalf("submitted was not counted at all")
	}
	if e.conn != nil {
		t.Fatalf("a failed share must tear the socket down so the miner redials")
	}
}
