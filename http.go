package vela

import (
	"bytes"
	"context"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

// HTTPContentType identifies a raw VELA value in an HTTP body. .vlf files use
// the separate VLF1 envelope with a checksum and optional dictionary.
const HTTPContentType = "application/vnd.vela"
const HTTPTextContentType = "application/vnd.vela+text"

func httpContentType(header http.Header) (string, error) {
	value := header.Get("Content-Type")
	if value == HTTPContentType || value == HTTPTextContentType {
		return value, nil
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil || (mediaType != HTTPContentType && mediaType != HTTPTextContentType) {
		return "", ErrType
	}
	return mediaType, nil
}

// NewHTTPRequest creates a client request with a raw VELA body.
func NewHTTPRequest(ctx context.Context, method, url string, value any, limits Limits) (*http.Request, error) {
	data, err := MarshalLimited(value, nil, limits)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", HTTPContentType)
	req.Header.Set("Accept", HTTPContentType)
	return req, nil
}

// NewHTTPTextRequest creates a client request with a human-readable VELA body.
func NewHTTPTextRequest(ctx context.Context, method, url string, value any, limits Limits) (*http.Request, error) {
	data, err := MarshalTextWithLimits(value, limits)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", HTTPTextContentType)
	req.Header.Set("Accept", HTTPTextContentType)
	return req, nil
}

// DecodeHTTPRequest reads one raw VELA request body into dst.
func DecodeHTTPRequest(req *http.Request, dst any, limits Limits) error {
	if req == nil || req.Body == nil {
		return ErrInvalid
	}
	mediaType, err := httpContentType(req.Header)
	if err != nil {
		return err
	}
	if req.ContentLength > int64(limits.normalized().MaxBytes) {
		return ErrLimit
	}
	return decodeHTTPBody(req.Body, dst, limits, mediaType)
}

// DecodeHTTPResponse reads one raw VELA response body into dst.
func DecodeHTTPResponse(resp *http.Response, dst any, limits Limits) error {
	if resp == nil || resp.Body == nil {
		return ErrInvalid
	}
	mediaType, err := httpContentType(resp.Header)
	if err != nil {
		return err
	}
	if resp.ContentLength > int64(limits.normalized().MaxBytes) {
		return ErrLimit
	}
	return decodeHTTPBody(resp.Body, dst, limits, mediaType)
}

func decodeHTTPBody(body io.Reader, dst any, limits Limits, mediaType string) error {
	limits = limits.normalized()
	data, err := io.ReadAll(io.LimitReader(body, int64(limits.MaxBytes)+1))
	if err != nil {
		return err
	}
	if len(data) > limits.MaxBytes {
		return ErrLimit
	}
	if mediaType == HTTPTextContentType {
		return UnmarshalTextInto(data, dst, limits)
	}
	return UnmarshalIntoWithLimits(data, nil, limits, dst)
}

// WriteHTTPResponse writes a raw VELA value with its content type and length.
// Serialization completes before headers are sent, so encode errors leave the
// response untouched.
func WriteHTTPResponse(w http.ResponseWriter, status int, value any, limits Limits) error {
	data, err := MarshalLimited(value, nil, limits)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", HTTPContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(status)
	n, err := w.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

// WriteHTTPTextResponse sends a human-readable VELA text body.
func WriteHTTPTextResponse(w http.ResponseWriter, status int, value any, limits Limits) error {
	data, err := MarshalTextWithLimits(value, limits)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", HTTPTextContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(status)
	n, err := w.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

// WriteHTTPResponseForRequest selects text when the request accepts it;
// otherwise it sends the compact binary representation.
func WriteHTTPResponseForRequest(w http.ResponseWriter, req *http.Request, status int, value any, limits Limits) error {
	if req != nil {
		accept := req.Header.Get("Accept")
		if accept == HTTPTextContentType {
			return WriteHTTPTextResponse(w, status, value, limits)
		}
		for _, candidate := range strings.Split(accept, ",") {
			mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(candidate))
			if err == nil && mediaType == HTTPTextContentType {
				return WriteHTTPTextResponse(w, status, value, limits)
			}
		}
	}
	return WriteHTTPResponse(w, status, value, limits)
}
