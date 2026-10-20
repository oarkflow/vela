package main

import (
	"bytes"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/oarkflow/vela"
)

func TestCurlPayload(t *testing.T) {
	request, err := hex.DecodeString("c20569641f2a09746578748568656c6c6f")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/echo", bytes.NewReader(request))
	req.Header.Set("Content-Type", vela.HTTPContentType)
	recorder := httptest.NewRecorder()
	handler(vela.Limits{MaxBytes: 1 << 20})(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != vela.HTTPContentType {
		t.Fatal(got)
	}
	want, err := hex.DecodeString("c20569641f2a09746578748f72656365697665643a2068656c6c6f")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(recorder.Body.Bytes(), want) {
		t.Fatalf("response %x, want %x", recorder.Body.Bytes(), want)
	}
}

func TestCurlTextPayload(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/echo", bytes.NewBufferString(`{id=u42;text="hello"}`))
	req.Header.Set("Content-Type", vela.HTTPTextContentType)
	req.Header.Set("Accept", vela.HTTPTextContentType)
	recorder := httptest.NewRecorder()
	handler(vela.Limits{MaxBytes: 1 << 20})(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != vela.HTTPTextContentType {
		t.Fatal(got)
	}
	if got := recorder.Body.String(); got != `{id=u42;text="received: hello"}` {
		t.Fatal(got)
	}
}
