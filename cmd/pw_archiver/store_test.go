package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type putRecord struct {
	method, path, ifNoneMatch, contentType string
	body                                   []byte
}

// fakeS3 answers PutObject with a scripted status and records what it was sent.
type fakeS3 struct {
	mu     sync.Mutex
	status []int // one per request, the last repeats
	puts   []putRecord
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.puts = append(f.puts, putRecord{
		method:      r.Method,
		path:        r.URL.Path,
		ifNoneMatch: r.Header.Get("If-None-Match"),
		contentType: r.Header.Get("Content-Type"),
		body:        body,
	})
	status := f.status[min(len(f.puts)-1, len(f.status)-1)]
	f.mu.Unlock()
	switch status {
	case http.StatusOK:
		w.Header().Set("ETag", `"abc"`)
		w.WriteHeader(http.StatusOK)
	case http.StatusPreconditionFailed:
		w.WriteHeader(http.StatusPreconditionFailed)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>PreconditionFailed</Code><Message>At least one of the pre-conditions you specified did not hold</Message></Error>`))
	default:
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>InternalError</Code><Message>boom</Message></Error>`))
	}
}

func newTestStore(t *testing.T, f *fakeS3) *S3Store {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	s, err := NewS3Store(context.Background(), S3Config{
		Endpoint:  srv.URL,
		Region:    "auto",
		Bucket:    "pw-raw-archive",
		AccessKey: "key",
		SecretKey: "secret",
		PathStyle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestS3StorePut(t *testing.T) {
	f := &fakeS3{status: []int{http.StatusOK}}
	s := newTestStore(t, f)
	created, err := s.Put(context.Background(), "raw/v1/20261024/0630.zst", []byte("object"), "application/zstd")
	if err != nil || !created {
		t.Fatalf("Put: %v %v", created, err)
	}
	if len(f.puts) != 1 {
		t.Fatalf("%d requests", len(f.puts))
	}
	p := f.puts[0]
	if p.method != http.MethodPut || p.path != "/pw-raw-archive/raw/v1/20261024/0630.zst" {
		t.Errorf("request %s %s", p.method, p.path)
	}
	if p.ifNoneMatch != "*" || p.contentType != "application/zstd" || string(p.body) != "object" {
		t.Errorf("request %+v", p)
	}
}

func TestS3StoreExisting(t *testing.T) {
	f := &fakeS3{status: []int{http.StatusPreconditionFailed}}
	s := newTestStore(t, f)
	created, err := s.Put(context.Background(), "raw/v1/20261024/0630.zst", []byte("object"), "application/zstd")
	if err != nil || created {
		t.Fatalf("Put on an existing key: created %v, err %v", created, err)
	}
	if len(f.puts) != 1 {
		t.Errorf("a 412 was retried: %d requests", len(f.puts))
	}
}

func TestS3StoreError(t *testing.T) {
	f := &fakeS3{status: []int{http.StatusInternalServerError}}
	s := newTestStore(t, f)
	created, err := s.Put(context.Background(), "raw/v1/20261024/0630.zst", []byte("object"), "application/zstd")
	if err == nil || created {
		t.Fatalf("Put on a failing store: created %v, err %v", created, err)
	}
}

func TestS3StoreConfig(t *testing.T) {
	bad := []S3Config{
		{Bucket: "b", AccessKey: "k", SecretKey: "s"},
		{Endpoint: "http://x", AccessKey: "k", SecretKey: "s"},
		{Endpoint: "http://x", Bucket: "b"},
	}
	for i, c := range bad {
		if _, err := NewS3Store(context.Background(), c); err == nil {
			t.Errorf("config %d accepted: %+v", i, c)
		}
	}
}
