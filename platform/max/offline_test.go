package max

import (
	"net/http"
	"os"
	"testing"

	"github.com/chenhg5/cc-connect/internal/testutil"
	"github.com/gorilla/websocket"
)

func TestMain(m *testing.M) {
	// Keep the guard installed until process exit: background workers may
	// outlive a test's immediate Stop call.
	http.DefaultTransport = testutil.NewLocalHTTPTransport()
	websocket.DefaultDialer = testutil.NewLocalWebSocketDialer()
	os.Exit(m.Run())
}
