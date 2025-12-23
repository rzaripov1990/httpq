package httpq

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

type (
	// ResponseModel is a generic structure representing the result of an HTTP request.
	// Fields:
	//   Data        - Parsed response body as type T (unmarshaled JSON/XML/etc. if possible)
	//   StatusCode  - HTTP response status code (e.g. 200, 404, 500)
	//   Headers     - Response headers as a map (each key is a header name and value is the first header value)
	//   ContentType - The Content-Type of the HTTP response
	//   RawBody     - Raw response body as bytes (useful for binary content or debugging)
	ResponseModel[T any] struct {
		Data        T
		StatusCode  int
		Headers     map[string]string
		ContentType string
		RawBody     []byte
	}
	ContentType int
	RetryPolicy struct {
		// MaxRetries defines how many times a request can be retried on failure.
		MaxRetries int
		// Backoff is the base delay between retries.
		Backoff time.Duration
		// Exponential, if true, multiplies Backoff exponentially by attempt number (1,2,4,...).
		Exponential bool
		// RetryStatusCodes is an explicit allow-list of status codes that should be retried.
		// If non-empty, these codes take precedence over the default rules.
		RetryStatusCodes []int
	}
)

// DefaultRetryable5xx contains a recommended set of 5xx status codes that are typically safe to retry.
var DefaultRetryable5xx = []int{
	http.StatusInternalServerError,     // 500
	http.StatusBadGateway,              // 502
	http.StatusServiceUnavailable,      // 503
	http.StatusGatewayTimeout,          // 504
	http.StatusHTTPVersionNotSupported, // 505
}

// DefaultRecommendedRetryStatusCodes is a broader recommended set of retryable status codes,
// including 5xx and common transient 4xx like 408 and 429.
var DefaultRecommendedRetryStatusCodes = []int{
	http.StatusRequestTimeout,  // 408
	http.StatusTooManyRequests, // 429
	http.StatusInternalServerError,
	http.StatusBadGateway,
	http.StatusServiceUnavailable,
	http.StatusGatewayTimeout,
}

func nextRetryDelay(policy *RetryPolicy, attempt int) time.Duration {
	if policy == nil || attempt <= 0 {
		return 0
	}
	if policy.Backoff <= 0 {
		return 0
	}
	if policy.Exponential {
		return policy.Backoff * time.Duration(1<<(attempt-1))
	}
	return policy.Backoff
}

func isRetryableStatus(policy *RetryPolicy, code int) bool {
	// Connection-level error (we treat 0 as "no HTTP response").
	if code == 0 {
		return true
	}

	// If policy is not set or has no explicit codes, do not retry based on status.
	if policy == nil || len(policy.RetryStatusCodes) == 0 {
		return false
	}

	for _, c := range policy.RetryStatusCodes {
		if c == code {
			return true
		}
	}
	return false
}

const (
	ContentNone ContentType = iota
	ContentJson
	ContentXml
	ContentBytes
	ContentMultiPart
)

type Rpc struct {
	client            *http.Client
	method            string
	url               string
	body              any
	header            map[string]string
	logging           bool
	logger            *slog.Logger
	contentType       ContentType
	contentTypeString string
	retryPolicy       *RetryPolicy
}

func NewRpc() *Rpc {
	return &Rpc{
		contentType:       ContentJson,
		contentTypeString: "application/json",
		client:            http.DefaultClient,
	}
}

// Clone returns a shallow copy of Rpc without copying the body.
func (r *Rpc) Clone() *Rpc {
	if r == nil {
		return nil
	}

	clone := &Rpc{
		client:            r.client,
		method:            r.method,
		url:               r.url,
		logging:           r.logging,
		logger:            r.logger,
		contentType:       r.contentType,
		contentTypeString: r.contentTypeString,
		retryPolicy:       r.retryPolicy,
	}

	if len(r.header) > 0 {
		clone.header = make(map[string]string, len(r.header))
		for k, v := range r.header {
			clone.header[k] = v
		}
	}

	return clone
}

func (r *Rpc) SetTimeout(timeout time.Duration) *Rpc {
	r.client.Timeout = timeout
	return r
}

func (r *Rpc) GetTimeout() time.Duration {
	return r.client.Timeout
}

func (r *Rpc) SetRetryPolicy(policy *RetryPolicy) *Rpc {
	r.retryPolicy = policy
	return r
}

func (r *Rpc) GetRetryPolicy() *RetryPolicy {
	return r.retryPolicy
}

func (r *Rpc) SetCookies(cookies map[string]string) *Rpc {
	var cookieHeader string
	for name, value := range cookies {
		if len(cookieHeader) > 0 {
			cookieHeader += "; "
		}
		cookieHeader += fmt.Sprintf("%s=%s", name, value)
	}
	if r.header == nil {
		r.header = make(map[string]string)
	}
	r.header["Cookie"] = cookieHeader
	return r
}

func (r *Rpc) getLogger() *slog.Logger {
	if r.logger != nil {
		return r.logger
	}
	return slog.Default()
}

func (r *Rpc) SetTransport(val http.RoundTripper) *Rpc {
	r.client.Transport = val
	return r
}

// SetRedirectFunc allows you to set a custom redirect policy for the underlying HTTP client.
// For example:
//
//	rpc := NewRpc("https://example.com").
//		SetRedirectFunc(func(req *http.Request, via []*http.Request) error {
//			// Prevent following redirects:
//			return http.ErrUseLastResponse
//		})
//
// By supplying your own function, you can control how redirect responses
// (like 301, 302, etc.) are handled.
//
// See: https://pkg.go.dev/net/http#Client.CheckRedirect
func (r *Rpc) SetRedirectFunc(val func(req *http.Request, via []*http.Request) error) *Rpc {
	r.client.CheckRedirect = val
	return r
}

func (r *Rpc) SetLogger(logger *slog.Logger) *Rpc {
	r.logger = logger
	return r
}

func (r *Rpc) Method(val string) *Rpc {
	r.method = val
	return r
}

func (r *Rpc) Get() *Rpc {
	r.method = http.MethodGet
	return r
}

func (r *Rpc) Post() *Rpc {
	r.method = http.MethodPost
	return r
}

func (r *Rpc) Put() *Rpc {
	r.method = http.MethodPut
	return r
}

func (r *Rpc) Delete() *Rpc {
	r.method = http.MethodDelete
	return r
}

func (r *Rpc) Patch() *Rpc {
	r.method = http.MethodPatch
	return r
}

func (r *Rpc) Head() *Rpc {
	r.method = http.MethodHead
	return r
}

func (r *Rpc) Options() *Rpc {
	r.method = http.MethodOptions
	return r
}

func (r *Rpc) SetUrl(val string) *Rpc {
	r.url = val
	return r
}

func (r *Rpc) SetBody(val any) *Rpc {
	r.body = val
	return r
}

func (r *Rpc) SetHeader(val map[string]string) *Rpc {
	r.header = val
	return r
}

func (r *Rpc) SetContentType(val ContentType) *Rpc {
	r.contentType = val
	return r
}

func (r *Rpc) None() *Rpc {
	r.contentType = ContentNone
	return r
}

func (r *Rpc) Json() *Rpc {
	r.contentType = ContentJson
	return r
}

func (r *Rpc) Xml() *Rpc {
	r.contentType = ContentXml
	return r
}

func (r *Rpc) Bytes() *Rpc {
	r.contentType = ContentBytes
	return r
}

func (r *Rpc) MultiPart() *Rpc {
	r.contentType = ContentMultiPart
	return r
}

func (r *Rpc) SetLogging(val bool) *Rpc {
	r.logging = val
	return r
}

// Do executes the configured Rpc, optionally with retries and trace ID, and
// returns a typed ResponseModel[T] with both parsed data and raw response.
func Do[T any](ctx context.Context, r *Rpc, traceID ...string) (result *ResponseModel[T], err error) {
	var tID string
	if len(traceID) > 0 {
		tID = traceID[0]
	}

	maxAttempts := 1
	if r.retryPolicy != nil && r.retryPolicy.MaxRetries > 0 {
		maxAttempts = r.retryPolicy.MaxRetries + 1
	}

	var lastStatus int

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if ctx.Err() != nil {
			if err == nil {
				err = ctx.Err()
			}
			return
		}

		start := time.Now()
		result, lastStatus, err = doOnce[T](ctx, r, tID, start)

		if err == nil && !isRetryableStatus(r.retryPolicy, lastStatus) {
			return
		}
		if err != nil && (ctx.Err() != nil) {
			return
		}

		if attempt == maxAttempts || r.retryPolicy == nil || !isRetryableStatus(r.retryPolicy, lastStatus) {
			return
		}

		if r.logging {
			r.getLogger().InfoContext(
				ctx,
				"httpq: retry",
				"method", r.method,
				"url", r.url,
				"trace_id", tID,
				"attempt", attempt,
				"max_attempts", maxAttempts,
				"status_code", lastStatus,
				"error", err,
			)
		}

		delay := nextRetryDelay(r.retryPolicy, attempt)
		if delay > 0 {
			select {
			case <-ctx.Done():
				if err == nil {
					err = ctx.Err()
				}
				return
			case <-time.After(delay):
			}
		}
	}

	return
}

func doOnce[T any](ctx context.Context, r *Rpc, tID string, start time.Time) (result *ResponseModel[T], statusCode int, err error) {
	body := new(bytes.Buffer)

	// (Re)build body and content type for each attempt.
	if r.body != nil {
		switch r.contentType {
		case ContentJson:
			_ = json.NewEncoder(body).Encode(r.body)
			r.contentTypeString = "application/json"
		case ContentXml:
			_ = xml.NewEncoder(body).Encode(r.body)
			r.contentTypeString = "application/xml"
		case ContentBytes:
			if b, ok := r.body.([]byte); ok {
				_, _ = body.Write(b)
			}
		case ContentMultiPart:
			values := map[string]any{}
			bts, errMarshal := json.Marshal(r.body)
			if errMarshal != nil {
				panic(errMarshal)
			}
			_ = json.Unmarshal(bts, &values)
			w := multipart.NewWriter(body)
			for k, v := range values {
				wfield, _ := w.CreateFormField(k)
				_, _ = wfield.Write([]byte(fmt.Sprintf("%v", v)))
			}

			r.contentTypeString = w.FormDataContentType()
			_ = w.Close()
		}
	}

	if r.logging {
		r.getLogger().InfoContext(
			ctx,
			"httpq: request",
			"method", r.method,
			"url", r.url,
			"trace_id", tID,
			"content_type", r.contentTypeString,
			"headers", r.header,
			"body", body.String(),
		)
	}

	req, err := http.NewRequestWithContext(ctx, r.method, r.url, body)
	if err != nil {
		if r.logging {
			r.getLogger().ErrorContext(
				ctx,
				"httpq: new request error",
				"method", r.method,
				"url", r.url,
				"trace_id", tID,
				"error", err,
			)
		}
		return
	}
	for k, v := range r.header {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", r.contentTypeString)
	if tID != "" {
		if req.Header.Get("X-Trace-Id") == "" {
			req.Header.Set("X-Trace-Id", tID)
		}
	}

	resp, err := r.client.Do(req)
	if err != nil {
		if r.logging {
			r.getLogger().ErrorContext(
				ctx,
				"httpq: do request error",
				"method", r.method,
				"url", r.url,
				"trace_id", tID,
				"error", err,
				"duration", time.Since(start),
			)
		}
		return
	}
	defer resp.Body.Close()

	statusCode = resp.StatusCode

	var bodyBytes []byte
	if resp.Body != http.NoBody {
		bodyBytes, _ = io.ReadAll(resp.Body)
		ct := resp.Header.Get("Content-Type")

		if r.logging {
			isText := strings.HasPrefix(ct, "text/") ||
				strings.Contains(ct, "json") ||
				strings.Contains(ct, "xml") ||
				strings.Contains(ct, "html")

			args := []any{
				"method", r.method,
				"url", r.url,
				"trace_id", tID,
				"status_code", resp.StatusCode,
				"status", resp.Status,
				"duration", time.Since(start),
				"content_type", ct,
				"content_length", len(bodyBytes),
			}

			if isText {
				args = append(args, "body", string(bodyBytes))
			} else {
				args = append(args,
					"binary", true,
				)
			}

			r.getLogger().InfoContext(
				ctx,
				"httpq: response",
				args...,
			)
		}

		result = &ResponseModel[T]{
			StatusCode:  resp.StatusCode,
			Headers:     map[string]string{},
			ContentType: ct,
			RawBody:     bodyBytes,
		}

		// Копируем заголовки
		for k, v := range resp.Header {
			if len(v) > 0 {
				result.Headers[k] = v[0]
			}
		}

		// Разбираем тело в Data, если 	это JSON или XML
		var data T
		switch {
		case strings.Contains(ct, "json"):
			err = json.Unmarshal(bodyBytes, &data)
			if err == nil {
				result.Data = data
			} else if r.logging {
				r.getLogger().ErrorContext(
					ctx,
					"httpq: json unmarshal error",
					"method", r.method,
					"url", r.url,
					"trace_id", tID,
					"status_code", resp.StatusCode,
					"error", err,
				)
			}
		case strings.Contains(ct, "xml") || bytes.HasPrefix(bodyBytes, []byte("<")):
			err = xml.Unmarshal(bodyBytes, &data)
			if err == nil {
				result.Data = data
			} else if r.logging {
				r.getLogger().ErrorContext(
					ctx,
					"httpq: xml unmarshal error",
					"method", r.method,
					"url", r.url,
					"trace_id", tID,
					"status_code", resp.StatusCode,
					"error", err,
				)
			}
		default:
		}
	}

	return
}
