package ui

// Number and duration formatting for panel cells.
//
// The rule everywhere here is that a cell is narrow and a reader is glancing,
// so a value gets one unit and two or three significant figures. Precision
// beyond that is not information: a hashrate quoted to six figures changes
// every one of them between frames and becomes harder to read, not more exact.

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// HumanRate is a hashrate with the unit that keeps it to four digits.
func HumanRate(h float64) string {
	switch {
	case h >= 1e12:
		return fmt.Sprintf("%.2f TH/s", h/1e12)
	case h >= 1e9:
		return fmt.Sprintf("%.2f GH/s", h/1e9)
	case h >= 1e6:
		return fmt.Sprintf("%.2f MH/s", h/1e6)
	case h >= 1e3:
		return fmt.Sprintf("%.2f KH/s", h/1e3)
	case h > 0:
		return fmt.Sprintf("%.0f H/s", h)
	}
	return "--"
}

// SplitRate is HumanRate with the number and the unit separately, for the
// headline where they are drawn at two different sizes.
func SplitRate(h float64) (num, unit string) {
	s := HumanRate(h)
	if i := strings.IndexByte(s, ' '); i > 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// ShortNum is a count with a magnitude suffix, for an axis label or a cell too
// narrow for the digits.
func ShortNum(v float64) string {
	switch {
	case v >= 1e12:
		return trimZero(fmt.Sprintf("%.1f", v/1e12)) + "T"
	case v >= 1e9:
		return trimZero(fmt.Sprintf("%.1f", v/1e9)) + "G"
	case v >= 1e6:
		return trimZero(fmt.Sprintf("%.1f", v/1e6)) + "M"
	case v >= 1e3:
		return trimZero(fmt.Sprintf("%.1f", v/1e3)) + "K"
	case v <= 0:
		return "0"
	default:
		return strconv.Itoa(int(v + 0.5))
	}
}

func trimZero(s string) string { return strings.TrimSuffix(s, ".0") }

// Commas groups an integer in threes.
func Commas(v uint64) string {
	s := strconv.FormatUint(v, 10)
	if len(s) <= 3 {
		return s
	}
	out := make([]byte, 0, len(s)+len(s)/3)
	for i := 0; i < len(s); i++ {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, s[i])
	}
	return string(out)
}

// HMS is a duration as hh:mm:ss, which is what an uptime wants: it stays the
// same width all day and is directly comparable between two machines.
func HMS(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	t := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", t/3600, (t%3600)/60, t%60)
}

// ShortDur is a duration at one significant unit: long enough to compare,
// short enough for a table cell.
func ShortDur(d time.Duration) string {
	switch {
	case d <= 0:
		return "--"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%.1fd", d.Hours()/24)
	}
}

// Ago is "12s ago" / "never", for a last-seen field. A relative time answers
// the question the reader actually has, which is whether it just happened.
func Ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return ShortDur(time.Since(t)) + " ago"
}

// Pct formats a fraction as a whole-number percentage, or "--" when the
// denominator was zero. A percentage of nothing is not 0%, and drawing it as
// 0% is the difference between "no shares yet" and "every share rejected".
func Pct(part, whole float64) string {
	if whole <= 0 {
		return "--"
	}
	return fmt.Sprintf("%.0f%%", part/whole*100)
}

// Pct1 is Pct to one decimal, for an acceptance rate where the difference
// between 99.8% and 100% is the whole point of the number.
func Pct1(part, whole float64) string {
	if whole <= 0 {
		return "--"
	}
	return fmt.Sprintf("%.1f%%", part/whole*100)
}

// NA is the single spelling of a value this machine cannot report. One
// spelling, used everywhere, so a reader learns it once and never has to
// wonder whether "--" and "n/a" mean different things.
const NA = "--"
