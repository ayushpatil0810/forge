package credentials

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

var testKeyringArmed bool
var testKeyringMu sync.Mutex

// ArmForTest installs go-keyring's in-memory mock and a transport that refuses
// non-loopback dials. Every test package that can construct a credential
// manager must call it from TestMain. Under go test, NewManager(nil) panics
// until this has run, so a test cannot read the real OS keychain.
func ArmForTest() {
	keyring.MockInit()
	SetDefaultKeyring(osKeyring{})
	testKeyringMu.Lock()
	testKeyringArmed = true
	testKeyringMu.Unlock()
	installNetGuard()
}

func installNetGuard() {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok || base == nil {
		return
	}
	clone := base.Clone()
	clone.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		if !loopbackHost(host) {
			return nil, fmt.Errorf("test network guard: refused non-loopback dial %s", addr)
		}
		var d net.Dialer
		return d.DialContext(ctx, network, addr)
	}
	http.DefaultTransport = clone
}

func loopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func testKeyringReady() bool {
	testKeyringMu.Lock()
	defer testKeyringMu.Unlock()
	return testKeyringArmed
}
