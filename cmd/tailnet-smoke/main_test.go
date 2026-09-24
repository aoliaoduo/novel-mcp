package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeHonorsHTTPClientTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	client := &http.Client{Timeout: 20 * time.Millisecond}
	started := time.Now()
	if code := probe(client, ts.URL, "/slow"); code != 0 {
		t.Fatalf("timed out probe status = %d", code)
	}
	if time.Since(started) > time.Second {
		t.Fatal("probe ignored HTTP timeout")
	}
}

func TestProbeCanObserveRedirectWithoutFollowing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/from" {
			http.Redirect(w, r, "/to", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	client := &http.Client{
		Timeout:       time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	if code := probe(client, ts.URL, "/from"); code != http.StatusFound {
		t.Fatalf("redirect status = %d, want %d", code, http.StatusFound)
	}
}
