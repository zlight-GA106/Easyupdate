package server

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHeartbeatRejectsInvalidInputs(t *testing.T) {
	s := testServer(t)
	for _, body := range []string{`{"device_id":"wrong"}`, strings.Repeat("a", 5000), `{} {}`, `{"device_id":"a94187b3-cc2f-4ef0-93fb-04e7e3444342","package_name":"com.example.app","version_name":"v1","version_code":-1}`} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/heartbeat", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("validation: %d", w.Code)
		}
	}
}
