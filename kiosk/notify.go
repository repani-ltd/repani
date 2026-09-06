// Readiness: the service-manager notification protocol.
package kiosk

import (
	"net"
	"os"
	"strings"
)

// notify sends one service-manager state line on $NOTIFY_SOCKET, and
// does nothing at all when that variable is unset -- which is every
// context but a systemd unit with Type=notify.
//
// It is what turns a restart from a guess into an answer. Under
// Type=simple the manager calls a unit started the moment it forks,
// before the socket is bound, so a deploy has nothing to wait for but
// a sleep; with READY=1 sent after the listener binds, "systemctl
// restart" returns when the kiosk is actually serving.
//
// A failure here is deliberately silent. The socket belongs to the
// service manager, and a kiosk that is serving correctly must not
// fail because nothing was listening on it.
func notify(state string) {
	addr := os.Getenv("NOTIFY_SOCKET")
	if addr == "" {
		return
	}
	// A leading '@' means the abstract namespace, whose names start
	// with a NUL byte. Only the first byte is special: a filesystem
	// path may contain '@' anywhere else.
	name := addr
	if rest, ok := strings.CutPrefix(name, "@"); ok {
		name = "\x00" + rest
	}
	c, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: name, Net: "unixgram"})
	if err != nil {
		return
	}
	defer c.Close()
	c.Write([]byte(state))
}
