package bridge

import (
	"testing"

	"github.com/Nickcandy/eth-fund-trace/internal/chainrpc"
)

func TestClassifyBridgeError(t *testing.T) {
	for err, want := range map[error]string{
		ErrMalformedEvent:       "bridge_malformed_event",
		chainrpc.ErrRateLimited: "bridge_upstream_rate_limited",
		chainrpc.ErrUnavailable: "bridge_upstream_unavailable",
	} {
		if got := classifyBridgeError(err); got != want {
			t.Fatalf("err=%v got=%s want=%s", err, got, want)
		}
	}
}
