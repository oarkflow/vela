package vela

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestHTTPRawRoundTrip(t *testing.T) {
	type message struct {
		ID   uint64   `vela:"id"`
		Tags []string `vela:"tags"`
	}
	in := message{ID: 300, Tags: []string{"a", "b"}}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got message
		if err := DecodeHTTPRequest(r, &got, Limits{}); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if !reflect.DeepEqual(got, in) {
			http.Error(w, "wrong body", http.StatusBadRequest)
			return
		}
		if err := WriteHTTPResponse(w, http.StatusCreated, got, Limits{}); err != nil {
			t.Error(err)
		}
	})
	req, err := NewHTTPRequest(context.Background(), http.MethodPost, "http://example.test/records", in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	resp := recorder.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	var out message
	if err := DecodeHTTPResponse(resp, &out, Limits{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("got %#v", out)
	}
}

func TestHTTPRejectsTypeAndOversize(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte{Null}))
	var out any
	if err := DecodeHTTPRequest(req, &out, Limits{}); !errors.Is(err, ErrType) {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", HTTPContentType)
	if err := DecodeHTTPRequest(req, &out, Limits{MaxBytes: 1}); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte{Null, Null}))
	req.Header.Set("Content-Type", HTTPContentType)
	if err := DecodeHTTPRequest(req, &out, Limits{MaxBytes: 1}); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if err := WriteHTTPResponse(recorder, http.StatusOK, "too long", Limits{MaxBytes: 1}); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
		t.Fatal("response was sent after encode failure")
	}
}

func TestHTTPTextRoundTrip(t *testing.T) {
	type message struct {
		ID   uint64 `vela:"id"`
		Text string `vela:"text"`
	}
	in := message{ID: 42, Text: "hello"}
	req, err := NewHTTPTextRequest(context.Background(), http.MethodPost, "http://example.test/echo", in, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Content-Type"); got != HTTPTextContentType {
		t.Fatal(got)
	}
	var decoded message
	if err := DecodeHTTPRequest(req, &decoded, Limits{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, decoded) {
		t.Fatal(decoded)
	}
	recorder := httptest.NewRecorder()
	if err := WriteHTTPResponseForRequest(recorder, req, http.StatusOK, decoded, Limits{}); err != nil {
		t.Fatal(err)
	}
	if got := recorder.Body.String(); got != `{id=u42;text="hello"}` {
		t.Fatal(got)
	}
	resp := recorder.Result()
	defer resp.Body.Close()
	var out message
	if err := DecodeHTTPResponse(resp, &out, Limits{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatal(out)
	}
}
