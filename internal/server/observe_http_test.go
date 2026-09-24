package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestObserverHTTPWiring 走真 HTTP：一通 tools/call 进来，计数与事件都要落到 Observer。
// 它锁的是 NewHTTP→NewMCP→Record 这条接线，不是 Observer 本身（那是 observe_test.go 的事）。
func TestObserverHTTPWiring(t *testing.T) {
	p := newProjects(t)
	route := strings.Repeat("a", 64)
	bearer := strings.Repeat("b", 64)
	obs := NewObserver()
	handler := NewHTTP(p, HTTPOptions{
		Credentials:   func() (Credentials, error) { return Credentials{Route: route, Bearer: bearer}, nil },
		Hosts:         []string{"novel.test"},
		RequireBearer: true,
		Observer:      obs,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	transport := &staysLocal{addr: srv.Listener.Addr().String()}

	call := func(body string) int {
		t.Helper()
		req, err := http.NewRequest("POST", "http://novel.test/mcp/"+route, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+bearer)
		resp, err := transport.roundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	okBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_projects","arguments":{}}}`
	if code := call(okBody); code > 299 {
		t.Fatalf("list_projects HTTP %d", code)
	}
	// 不存在的项目：进到工具里才失败，事件记 ✕。
	badBody := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"project_status","arguments":{"project":"ghost-book"}}}`
	if code := call(badBody); code > 299 {
		t.Fatalf("project_status HTTP %d", code)
	}
	c, s, f := obs.Stats.Snapshot()
	if c != 2 || s != 1 || f != 1 {
		t.Fatalf("counts = %d/%d/%d, want 2/1/1", c, s, f)
	}
	evs := obs.Log.Recent(5)
	if len(evs) != 2 {
		t.Fatalf("events = %d, want 2", len(evs))
	}
	if evs[0].Tool != "project_status" || evs[0].OK || evs[0].Err == "" {
		t.Fatalf("fail event wrong: %+v", evs[0])
	}
	if evs[1].Tool != "list_projects" || !evs[1].OK || evs[1].Project != "" {
		t.Fatalf("ok event wrong: %+v", evs[1])
	}
}
