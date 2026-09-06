package main

// Console state that is neither terminal handling nor drawing: which history
// window is showing, which card the GPU panel is on, what the log view is
// doing, and what the window title says.
//
// It lives apart from tui.go because none of it is about owning a terminal and
// none of it is about painting cells. It is the small amount of state a user's
// key presses accumulate, and keeping it here means the input handler and the
// renderer can both read it without either of them owning it.

import (
	"io"
	"strings"
	"time"

	"github.com/notoriousjoshyb/derostorm/internal/ui"
)

// ---------------------------------------------------------------- history ranges

// histRange is one of the windows the hashrate chart can show.
//
// Each one names the series the sampler already keeps at that resolution, so
// switching range is a slice selection and not a resampling job: the render
// thread never averages a day of samples, because the sampler averaged them
// as they arrived.
type histRange struct {
	label string
	xaxis []string
	pick  func(s Snapshot) []float64
	// span is how long the window covers, used for the "no data yet" message
	// and nothing else.
	span time.Duration
}

// The label of each window is the span of the series behind it, and it has to
// stay that way. The sampler keeps five series at five resolutions -- one
// second for three hundred samples, five for a hundred and eighty, twenty for
// a hundred and eighty, ninety for two hundred and forty, six minutes for two
// hundred and forty -- and those multiply out to exactly five minutes, fifteen
// minutes, one hour, six hours and one day. A window named for a span its
// series does not cover is a chart axis that lies, which is worse than no
// chart at all.
var histRanges = []histRange{
	{"5M", []string{"5m", "4m", "3m", "2m", "1m", "Now"},
		func(s Snapshot) []float64 { return s.History }, 5 * time.Minute},
	{"15M", []string{"15m", "10m", "5m", "Now"},
		func(s Snapshot) []float64 { return s.Hist15m }, 15 * time.Minute},
	{"1H", []string{"1h", "45m", "30m", "15m", "Now"},
		func(s Snapshot) []float64 { return s.Hist1h }, time.Hour},
	{"6H", []string{"6h", "4h", "2h", "Now"},
		func(s Snapshot) []float64 { return s.Hist6h }, 6 * time.Hour},
	{"24H", []string{"24h", "18h", "12h", "6h", "Now"},
		func(s Snapshot) []float64 { return s.Hist24h }, 24 * time.Hour},
}

// rangeLabels is the tab strip, built once.
var rangeLabels = func() []string {
	out := make([]string, len(histRanges))
	for i, r := range histRanges {
		out[i] = r.label
	}
	return out
}()

func clampRange(i int) int {
	if i < 0 {
		return len(histRanges) - 1
	}
	if i >= len(histRanges) {
		return 0
	}
	return i
}

// ---------------------------------------------------------------- log view

// logView is everything the event log's own controls remember.
type logView struct {
	// scroll is how many lines back from the newest the view is. Zero means
	// pinned to the bottom.
	scroll int
	// paused freezes the view where it is even as new lines arrive. It is a
	// display decision only: the ring keeps filling, nothing is dropped that
	// would not have been dropped anyway, and no mining thread waits on it.
	paused bool
	// min is the lowest level shown.
	min LogLevel
	// text is a case-insensitive substring every shown line must contain.
	text string
	// clearedAt hides everything older than it. The ring belongs to the run
	// loop and is shared with the stats writer and the log file, so "clear"
	// here means "stop showing", never "throw away".
	clearedAt time.Time
}

// filterName is what the footer calls the current filter.
func (v logView) filterName() string {
	switch {
	case v.text != "":
		return "\"" + v.text + "\""
	case v.min == LogError:
		return "ERRORS"
	case v.min == LogWarn:
		return "WARN+"
	case v.min == LogGood:
		return "SHARES+"
	}
	return "ALL"
}

func (v logView) active() bool { return v.text != "" || v.min != LogInfo || !v.clearedAt.IsZero() }

// apply is the log the view should show: a read-only projection of the slice
// the run loop owns, which it never mutates and whose lock it never takes.
//
// dst is an optional scratch buffer for the filtered case; a caller that has
// one gets no allocation at all. Callers that do not pass nil, because a
// filter is something a person turns on deliberately and a few hundred entries
// copied while one is active is not a cost worth threading a buffer through
// four call sites for.
func (v logView) apply(dst []LogEntry, src []LogEntry) []LogEntry {
	// The common case is no filter at all, and it must not cost a copy. The
	// ring is append-only and is never rewritten in place, so handing the
	// caller the same slice is safe -- and it is the whole reason the run loop
	// stopped copying four hundred entries eight times a second.
	if !v.active() {
		return src
	}
	dst = dst[:0]
	want := strings.ToLower(v.text)
	for _, e := range src {
		if e.Level < v.min {
			continue
		}
		if !v.clearedAt.IsZero() && e.At.Before(v.clearedAt) {
			continue
		}
		if want != "" &&
			!strings.Contains(strings.ToLower(e.Text), want) &&
			!strings.Contains(strings.ToLower(e.Tag), want) {
			continue
		}
		dst = append(dst, e)
	}
	return dst
}

// ---------------------------------------------------------------- window title

// titleInterval is how often the terminal's title bar is rewritten.
//
// Once a second, not once a frame. A title update is an escape sequence the
// terminal has to parse and then hand to the window manager, and several
// terminals redraw their whole tab strip for it; at eight frames a second that
// is visible as a flickering tab on Windows Terminal and as measurable CPU in
// the compositor on GNOME. A hashrate that updates once a second in a title
// bar is not less useful than one that updates eight times.
const titleInterval = time.Second

// setTitle writes the OSC sequence that renames the window and the tab.
//
// Both OSC 0 and OSC 2 are not sent -- only OSC 2. OSC 0 sets the icon name as
// well, which on some window managers is what appears in the task switcher,
// and a task switcher entry that changes eight hundred times an hour is a
// nuisance rather than a feature.
//
// Terminals that do not understand it ignore it silently, which is why there
// is no capability check: the sequence is a no-op on a terminal without a
// title, and there is no reliable way to ask.
func setTitle(w io.Writer, s string) {
	// A control character in a title is the one way this can go wrong: the
	// string is terminated by BEL, so a BEL inside it would end the sequence
	// early and leave the rest as text on the screen.
	if strings.ContainsAny(s, "\x07\x1b\r\n") {
		return
	}
	io.WriteString(w, "\x1b]2;"+s+"\x07")
}

// titleFor is the window title for a snapshot: the name, the headline rate and
// the state, which is everything someone can read off a taskbar button.
func titleFor(s Snapshot) string {
	state := "MINING"
	switch s.State {
	case StateConnecting:
		state = "CONNECTING"
	case StateReconnecting:
		state = "RECONNECTING"
	case StateStopping:
		state = "STOPPING"
	}
	if s.Hashrate <= 0 {
		return "DEROSTORM | " + state
	}
	return "DEROSTORM | " + ui.HumanRate(s.Hashrate) + " | " + state
}

// updateTitle rewrites the window title, at most once a second and only when
// the text has actually changed.
func (t *TUI) updateTitle(s Snapshot) {
	if !t.title {
		return
	}
	now := time.Now()
	if now.Sub(t.titleAt) < titleInterval {
		return
	}
	t.titleAt = now
	if txt := titleFor(s); txt != t.lastTitle {
		t.lastTitle = txt
		setTitle(t.out, txt)
	}
}
