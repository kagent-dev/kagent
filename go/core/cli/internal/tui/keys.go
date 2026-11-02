package tui

import (
	"bytes"
	"os"
	"regexp"
	"strconv"
	"sync/atomic"
)

// Kitty keyboard protocol: push "disambiguate" so Shift+Enter has its own
// code, ask whether the terminal took it, and pop it on exit.
const (
	kittyPush  = "\x1b[>1u\x1b[?u"
	kittyPop   = "\x1b[<u"
	kittyEnter = "\x1b\r" // Shift+Enter arrives as alt+enter, which inserts a newline
)

// kittyKeys is set once the terminal answers the protocol query, so Shift+Enter works.
var kittyKeys atomic.Bool

// kittyInput turns the protocol's CSI u keys, which bubbletea v1 cannot parse,
// into the legacy bytes it can. It embeds the terminal so bubbletea still sets raw mode.
type kittyInput struct {
	*os.File
	pending []byte
}

var csiU = regexp.MustCompile(`\x1b\[(\?)?(\d+)(?::\d+)*(?:;(\d+)(?::\d+)*)?u`)

func (k *kittyInput) Read(p []byte) (int, error) {
	buf := make([]byte, max(len(p)-len(k.pending), 1))
	n, err := k.File.Read(buf)
	data := append(k.pending, buf[:n]...)
	k.pending = nil
	// A sequence split across reads waits for the rest.
	if i := bytes.LastIndex(data, []byte("\x1b[")); i >= 0 && unfinishedCSI(data[i+2:]) {
		k.pending, data = append([]byte(nil), data[i:]...), data[:i]
	}
	return copy(p, csiU.ReplaceAllFunc(data, legacyKey)), err
}

// unfinishedCSI reports whether the bytes after "ESC [" are parameters still waiting for a final byte.
func unfinishedCSI(params []byte) bool {
	return len(bytes.Trim(params, "0123456789;:?")) == 0
}

// legacyKey is the legacy encoding of one CSI u key; the query's answer is dropped.
func legacyKey(seq []byte) []byte {
	m := csiU.FindSubmatch(seq)
	if len(m[1]) > 0 {
		kittyKeys.Store(true)
		return nil
	}
	code, _ := strconv.Atoi(string(m[2]))
	mods := 1
	if len(m[3]) > 0 {
		mods, _ = strconv.Atoi(string(m[3]))
	}
	shift, alt, ctrl := (mods-1)&1 != 0, (mods-1)&2 != 0, (mods-1)&4 != 0
	var out []byte
	switch {
	case code == 13 && (shift || alt):
		return []byte(kittyEnter)
	case code == 9 && shift:
		return []byte("\x1b[Z")
	case code >= 0xe000: // keypad and other functional keys bubbletea has no name for
		return seq
	case ctrl && (code >= 'a' && code <= 'z' || code >= '@' && code <= '_'):
		out = []byte{byte(code) & 0x1f}
	case ctrl && code == ' ':
		out = []byte{0}
	default:
		out = []byte(string(rune(code)))
	}
	if alt {
		out = append([]byte{0x1b}, out...)
	}
	return out
}
