package core

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kthom1/switchyard/config"
)

func TestBoardInitializedRequiresExplicitSetupStatus(t *testing.T) {
	for _, test := range []struct {
		name, body             string
		status                 int
		initialized, wantError bool
	}{
		{"fresh", `{"instance":{"is_setup_done":false}}`, 200, false, false},
		{"existing", `{"instance":{"is_setup_done":true}}`, 200, true, false},
		{"missing status", `{"instance":{}}`, 200, false, true},
		{"invalid JSON", `invalid`, 200, false, true},
		{"redirect", `{"instance":{"is_setup_done":true}}`, 302, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.URL.Path != "/api/instances/" || r.Header.Get("X-API-Key") != "" {
					t.Error("setup status must use the public read-only instance endpoint")
				}
				w.Header().Set("Location", "/redirected")
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			a := Installation{Settings: config.Settings{Port: server.Listener.Addr().(*net.TCPAddr).Port}}
			initialized, err := a.boardInitialized()
			if initialized != test.initialized || (err != nil) != test.wantError || requests != 1 {
				t.Fatalf("got initialized=%t, error=%v, requests=%d", initialized, err, requests)
			}
		})
	}
}
