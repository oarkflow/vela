package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"github.com/oarkflow/vela"
)

const jsonContentType = "application/json"

var benchmarkMessage = Message{ID: 42, Text: "hello", Tags: []string{"demo", "http"}}
var benchmarkReply = Message{ID: 42, Text: "received: hello", Tags: []string{"demo", "http"}}
var benchmarkLimits = vela.Limits{MaxBytes: 1 << 20}

// jsonHandler performs the same echo operation and applies the same body limit
// as the VELA example. Both handlers read, decode, encode, and write one value.
func jsonHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/echo" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, int64(benchmarkLimits.MaxBytes)+1))
	if err != nil || len(data) > benchmarkLimits.MaxBytes {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	var request Message
	if err := json.Unmarshal(data, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	response := Message{ID: request.ID, Text: "received: " + request.Text, Tags: request.Tags}
	encoded, err := json.Marshal(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", jsonContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(encoded)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(encoded)
}

type httpFormat struct {
	name        string
	contentType string
	handler     http.Handler
	encode      func(Message) ([]byte, error)
	decode      func(*http.Response, *Message) error
}

func benchmarkFormats() []httpFormat {
	return []httpFormat{
		{
			name: "JSON", contentType: jsonContentType, handler: http.HandlerFunc(jsonHandler),
			encode: func(m Message) ([]byte, error) { return json.Marshal(m) },
			decode: func(r *http.Response, m *Message) error {
				data, err := io.ReadAll(r.Body)
				if err != nil {
					return err
				}
				return json.Unmarshal(data, m)
			},
		},
		{
			name: "VELABinary", contentType: vela.HTTPContentType, handler: handler(benchmarkLimits),
			encode: func(m Message) ([]byte, error) { return vela.MarshalLimited(m, nil, benchmarkLimits) },
			decode: func(r *http.Response, m *Message) error { return vela.DecodeHTTPResponse(r, m, benchmarkLimits) },
		},
		{
			name: "VELAText", contentType: vela.HTTPTextContentType, handler: handler(benchmarkLimits),
			encode: func(m Message) ([]byte, error) { return vela.MarshalTextWithLimits(m, benchmarkLimits) },
			decode: func(r *http.Response, m *Message) error { return vela.DecodeHTTPResponse(r, m, benchmarkLimits) },
		},
	}
}

func makeBenchmarkRequest(url string, f httpFormat) (*http.Request, error) {
	body, err := f.encode(benchmarkMessage)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", f.contentType)
	req.Header.Set("Accept", f.contentType)
	return req, nil
}

func checkBenchmarkResponse(r *http.Response, f httpFormat) error {
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP status %d", r.StatusCode)
	}
	if got := r.Header.Get("Content-Type"); got != f.contentType {
		return fmt.Errorf("content type %q", got)
	}
	var reply Message
	if err := f.decode(r, &reply); err != nil {
		return err
	}
	if !reflect.DeepEqual(reply, benchmarkReply) {
		return fmt.Errorf("reply %+v", reply)
	}
	return nil
}

func TestHTTPComparisonPayloads(t *testing.T) {
	for _, f := range benchmarkFormats() {
		req, err := f.encode(benchmarkMessage)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := f.encode(benchmarkReply)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: request=%d bytes response=%d bytes", f.name, len(req), len(resp))
		if f.name == "JSON" {
			if got, want := string(req), `{"id":42,"text":"hello","tags":["demo","http"]}`; got != want {
				t.Fatalf("JSON request %q, want %q", got, want)
			}
			if got, want := string(resp), `{"id":42,"text":"received: hello","tags":["demo","http"]}`; got != want {
				t.Fatalf("JSON response %q, want %q", got, want)
			}
		}
		if f.name == "VELAText" {
			if got, want := string(req), `{id=u42;tags=["demo";"http"];text="hello"}`; got != want {
				t.Fatalf("VELA text request %q, want %q", got, want)
			}
			if got, want := string(resp), `{id=u42;tags=["demo";"http"];text="received: hello"}`; got != want {
				t.Fatalf("VELA text response %q, want %q", got, want)
			}
		}
		r := httptest.NewRecorder()
		httpReq, err := makeBenchmarkRequest("http://example.com/echo", f)
		if err != nil {
			t.Fatal(err)
		}
		f.handler.ServeHTTP(r, httpReq)
		if err := checkBenchmarkResponse(r.Result(), f); err != nil {
			t.Fatalf("%s: %v", f.name, err)
		}
	}
}

// BenchmarkHTTPInProcess includes client encoding, HTTP request construction,
// handler decoding and encoding, and client decoding. It excludes transport.
func BenchmarkHTTPInProcess(b *testing.B) {
	for _, f := range benchmarkFormats() {
		b.Run(f.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				req, err := makeBenchmarkRequest("http://example.com/echo", f)
				if err != nil {
					b.Fatal(err)
				}
				recorder := httptest.NewRecorder()
				f.handler.ServeHTTP(recorder, req)
				if err := checkBenchmarkResponse(recorder.Result(), f); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkHTTPLoopback measures the same flow over a localhost HTTP socket.
// It may be unavailable in sandboxes that forbid binding a loopback port.
func BenchmarkHTTPLoopback(b *testing.B) {
	for _, f := range benchmarkFormats() {
		b.Run(f.name, func(b *testing.B) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				b.Skipf("localhost socket unavailable: %v", err)
			}
			server := httptest.NewUnstartedServer(f.handler)
			server.Listener = listener
			server.Start()
			defer server.Close()
			client := server.Client()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				req, err := makeBenchmarkRequest(server.URL+"/echo", f)
				if err != nil {
					b.Fatal(err)
				}
				resp, err := client.Do(req)
				if err != nil {
					b.Fatal(err)
				}
				if err := checkBenchmarkResponse(resp, f); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
