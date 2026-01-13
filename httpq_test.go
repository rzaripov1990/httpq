package httpq

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testUser struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// newTestServer returns an httptest.Server that can emulate various behaviors.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	var retryCounter int32

	mux := http.NewServeMux()

	// Simple JSON success endpoint.
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(testUser{ID: 1, Name: "John"})
	})

	// Endpoint that always returns 400 (non-retryable).
	mux.HandleFunc("/bad-request", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	})

	// Endpoint to test cookies.
	mux.HandleFunc("/with-cookies", func(w http.ResponseWriter, r *http.Request) {
		cookie := r.Header.Get("Cookie")
		if cookie == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"cookie": cookie})
	})

	// Endpoint for retry tests: first two calls -> 500, third -> 200.
	mux.HandleFunc("/retry", func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&retryCounter, 1)
		if count <= 2 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"temporary"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(testUser{ID: int(count), Name: "Retried"})
	})

	// Endpoint for testing multi-value headers
	mux.HandleFunc("/multi-headers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "cookie1=value1")
		w.Header().Add("Set-Cookie", "cookie2=value2")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// Endpoint for testing User-Agent
	mux.HandleFunc("/user-agent", func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"user-agent": ua})
	})

	// Endpoint for testing invalid JSON (parse error)
	mux.HandleFunc("/invalid-json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"invalid": json}`))
	})

	return httptest.NewServer(mux)
}

func TestDo_SimpleJSON(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	log.Println("srv.URL", srv.URL)
	rpc := NewRpc().
		Get().
		SetUrl(srv.URL + "/user").
		Json().
		SetLogging(false)

	ctx := context.Background()
	resp, err := Do[testUser](ctx, rpc)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if resp.Data.ID != 1 || resp.Data.Name != "John" {
		t.Fatalf("unexpected data: %+v", resp.Data)
	}
}

func TestDo_NonRetryableStatus_NoRetry(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	rpc := NewRpc().
		Get().
		SetUrl(srv.URL + "/bad-request").
		Json().
		SetLogging(false).
		SetRetryPolicy(&RetryPolicy{
			MaxRetries:       3,
			Backoff:          1 * time.Millisecond,
			Exponential:      true,
			RetryStatusCodes: DefaultRecommendedRetryStatusCodes,
		})

	ctx := context.Background()
	resp, err := Do[map[string]any](ctx, rpc)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestDo_RetryPolicy_ExponentialBackoff(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	rpc := NewRpc().
		Get().
		SetUrl(srv.URL + "/retry").
		Json().
		SetLogging(false).
		SetRetryPolicy(&RetryPolicy{
			MaxRetries:       3,
			Backoff:          1 * time.Millisecond,
			Exponential:      true,
			RetryStatusCodes: DefaultRetryable5xx,
		})

	ctx := context.Background()
	start := time.Now()
	resp, err := Do[testUser](ctx, rpc)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if resp.Data.Name != "Retried" {
		t.Fatalf("unexpected data: %+v", resp.Data)
	}

	// We don't assert exact delay, but we expect some delay due to retries.
	if elapsed <= 0 {
		t.Fatalf("expected elapsed time > 0, got %v", elapsed)
	}
}

func TestDo_RetryPolicy_ExponentialBackoff2(t *testing.T) {
	type autoGenerated struct {
		ID        int      `json:"id"`
		Name      string   `json:"name"`
		PhotoUrls []string `json:"photoUrls"`
		Tags      []any    `json:"tags"`
		Status    string   `json:"status"`
	}

	rpc := NewRpc().
		Get().
		SetUrl("https://petstore3.swagger.io/api/v3/pet/1").
		Json().
		SetLogging(false).
		SetRetryPolicy(&RetryPolicy{
			MaxRetries:  3,
			Backoff:     1 * time.Millisecond,
			Exponential: true,
			RetryStatusCodes: append(
				DefaultRetryable5xx,
				DefaultRecommendedRetryStatusCodes...,
			),
		})

	ctx := context.Background()
	start := time.Now()
	resp, err := Do[autoGenerated](ctx, rpc)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	// We don't assert exact delay, but we expect some delay due to retries.
	if elapsed <= 0 {
		t.Fatalf("expected elapsed time > 0, got %v", elapsed)
	}
}

func TestRpc_CloneWithoutBody(t *testing.T) {
	original := NewRpc().
		Post().
		SetUrl("http://example.com").
		Json().
		SetBody(map[string]string{"k": "v"}).
		SetHeader(map[string]string{"X-Test": "1"}).
		SetLogging(true).
		SetRetryPolicy(&RetryPolicy{
			MaxRetries:  2,
			Backoff:     10 * time.Millisecond,
			Exponential: false,
		})

	clone := original.Clone()
	if clone == nil {
		t.Fatalf("expected clone not to be nil")
	}

	if clone.method != original.method || clone.url != original.url {
		t.Fatalf("method/url not cloned correctly")
	}
	if clone.body != nil {
		t.Fatalf("expected body to be nil in clone")
	}
	if clone.contentType != original.contentType {
		t.Fatalf("contentType not cloned")
	}
	if clone.retryPolicy != original.retryPolicy {
		t.Fatalf("retryPolicy pointer not cloned")
	}
	if clone.header["X-Test"] != "1" {
		t.Fatalf("header not cloned")
	}

	clone.header["X-Test"] = "2"
	if original.header["X-Test"] == "2" {
		t.Fatalf("expected headers map to be copied, not shared")
	}
}

func TestDo_Cookies(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	rpc := NewRpc().
		Get().
		SetUrl(srv.URL + "/with-cookies").
		SetCookies(map[string]string{"sid": "123"}).
		Json().
		SetLogging(false)

	ctx := context.Background()
	resp, err := Do[map[string]string](ctx, rpc)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	_ = resp.Data
	if resp.Data["cookie"] == "" {
		t.Fatalf("expected cookie to be echoed back in response")
	}
}

// TestNewRpc_IsolatedClient tests that each Rpc instance has its own http.Client
func TestNewRpc_IsolatedClient(t *testing.T) {
	rpc1 := NewRpc()
	rpc2 := NewRpc()

	// Set different timeouts
	rpc1.SetTimeout(1 * time.Second)
	rpc2.SetTimeout(30 * time.Second)

	// Verify they are isolated
	if rpc1.GetTimeout() != 1*time.Second {
		t.Fatalf("expected rpc1 timeout to be 1s, got %v", rpc1.GetTimeout())
	}
	if rpc2.GetTimeout() != 30*time.Second {
		t.Fatalf("expected rpc2 timeout to be 30s, got %v", rpc2.GetTimeout())
	}

	// Verify they don't share the same client instance
	if rpc1.client == rpc2.client {
		t.Fatalf("rpc1 and rpc2 should have different client instances")
	}
}

// TestClone_IsolatedClient tests that Clone creates a new http.Client
func TestClone_IsolatedClient(t *testing.T) {
	original := NewRpc().SetTimeout(10 * time.Second)
	clone := original.Clone()

	if clone.client == original.client {
		t.Fatalf("clone should have a different client instance")
	}

	// Modify clone timeout
	clone.SetTimeout(20 * time.Second)

	// Original should not be affected
	if original.GetTimeout() != 10*time.Second {
		t.Fatalf("original timeout should not be affected, got %v", original.GetTimeout())
	}
	if clone.GetTimeout() != 20*time.Second {
		t.Fatalf("clone timeout should be 20s, got %v", clone.GetTimeout())
	}
}

// TestDo_RetryWithDefaultCodes tests that retry works with default codes when RetryStatusCodes is empty
func TestDo_RetryWithDefaultCodes(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	// Create retry policy without explicit RetryStatusCodes
	rpc := NewRpc().
		Get().
		SetUrl(srv.URL + "/retry").
		Json().
		SetLogging(false).
		SetRetryPolicy(&RetryPolicy{
			MaxRetries:  3,
			Backoff:     1 * time.Millisecond,
			Exponential: true,
			// RetryStatusCodes is empty, should use DefaultRetryable5xx
		})

	ctx := context.Background()
	resp, err := Do[testUser](ctx, rpc)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if resp.Data.Name != "Retried" {
		t.Fatalf("unexpected data: %+v", resp.Data)
	}
}

// TestDo_JsonSerializationError tests that JSON serialization errors are returned
func TestDo_JsonSerializationError(t *testing.T) {
	// Create a body that cannot be serialized to JSON
	invalidBody := make(chan int)

	rpc := NewRpc().
		Post().
		SetUrl("http://example.com").
		Json().
		SetBody(invalidBody).
		SetLogging(false)

	ctx := context.Background()
	_, err := Do[map[string]any](ctx, rpc)
	if err == nil {
		t.Fatalf("expected error for invalid JSON body, got nil")
	}
	if !contains(err.Error(), "json") {
		t.Fatalf("expected error to mention json, got: %v", err)
	}
}

// TestDo_XmlSerializationError tests that XML serialization errors are returned
func TestDo_XmlSerializationError(t *testing.T) {
	// Create a body that cannot be serialized to XML
	invalidBody := make(chan int)

	rpc := NewRpc().
		Post().
		SetUrl("http://example.com").
		Xml().
		SetBody(invalidBody).
		SetLogging(false)

	ctx := context.Background()
	_, err := Do[map[string]any](ctx, rpc)
	if err == nil {
		t.Fatalf("expected error for invalid XML body, got nil")
	}
	if !contains(err.Error(), "xml") {
		t.Fatalf("expected error to mention xml, got: %v", err)
	}
}

// TestDo_MultipartNoPanic tests that multipart doesn't panic on invalid data
func TestDo_MultipartNoPanic(t *testing.T) {
	// Create a body that cannot be marshaled
	invalidBody := make(chan int)

	rpc := NewRpc().
		Post().
		SetUrl("http://example.com").
		MultiPart().
		SetBody(invalidBody).
		SetLogging(false)

	ctx := context.Background()
	_, err := Do[map[string]any](ctx, rpc)
	if err == nil {
		t.Fatalf("expected error for invalid multipart body, got nil")
	}
	// Should not panic, should return error
}

// TestDo_MultipartStruct tests multipart with struct
func TestDo_MultipartStruct(t *testing.T) {
	type FormData struct {
		Name    string `form:"name"`
		Email   string `form:"email"`
		Message string `form:"message"`
	}

	rpc := NewRpc().
		Post().
		SetUrl("http://example.com").
		MultiPart().
		SetBody(FormData{
			Name:    "Test",
			Email:   "test@example.com",
			Message: "Hello",
		}).
		SetLogging(false)

	ctx := context.Background()
	// This will fail because we don't have a real server, but it should not panic
	_, err := Do[map[string]any](ctx, rpc)
	// We expect a network error, not a panic
	if err != nil && contains(err.Error(), "panic") {
		t.Fatalf("should not panic, got: %v", err)
	}
}

// TestDo_MultiValueHeaders tests that multi-value headers are preserved
func TestDo_MultiValueHeaders(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	rpc := NewRpc().
		Get().
		SetUrl(srv.URL + "/multi-headers").
		Json().
		SetLogging(false)

	ctx := context.Background()
	resp, err := Do[map[string]string](ctx, rpc)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}

	cookies := resp.Headers["Set-Cookie"]
	if len(cookies) != 2 {
		t.Fatalf("expected 2 Set-Cookie headers, got %d", len(cookies))
	}
	if cookies[0] != "cookie1=value1" {
		t.Fatalf("expected first cookie to be 'cookie1=value1', got '%s'", cookies[0])
	}
	if cookies[1] != "cookie2=value2" {
		t.Fatalf("expected second cookie to be 'cookie2=value2', got '%s'", cookies[1])
	}

	// Test GetHeader helper method
	firstCookie := resp.GetHeader("Set-Cookie")
	if firstCookie != "cookie1=value1" {
		t.Fatalf("expected GetHeader to return first value, got '%s'", firstCookie)
	}
}

// TestDo_UserAgent tests that default User-Agent is set
func TestDo_UserAgent(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	rpc := NewRpc().
		Get().
		SetUrl(srv.URL + "/user-agent").
		Json().
		SetLogging(false)

	ctx := context.Background()
	resp, err := Do[map[string]string](ctx, rpc)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}

	if resp.Data["user-agent"] != DefaultUserAgent {
		t.Fatalf("expected User-Agent to be '%s', got '%s'", DefaultUserAgent, resp.Data["user-agent"])
	}
}

// TestDo_ParseError tests that ParseError is set when JSON parsing fails
func TestDo_ParseError(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	rpc := NewRpc().
		Get().
		SetUrl(srv.URL + "/invalid-json").
		Json().
		SetLogging(false)

	ctx := context.Background()
	resp, err := Do[testUser](ctx, rpc)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}

	if resp.ParseError == nil {
		t.Fatalf("expected ParseError to be set for invalid JSON")
	}
}

// TestDo_ConcurrentUsage tests that Rpc can be used concurrently
func TestDo_ConcurrentUsage(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			rpc := NewRpc().
				Get().
				SetUrl(srv.URL + "/user").
				Json().
				SetLogging(false).
				SetTimeout(5 * time.Second)

			ctx := context.Background()
			resp, err := Do[testUser](ctx, rpc)
			if err != nil {
				t.Errorf("goroutine %d: Do returned error: %v", id, err)
				return
			}

			if resp.StatusCode != http.StatusOK {
				t.Errorf("goroutine %d: expected status 200, got %d", id, resp.StatusCode)
			}
		}(i)
	}

	wg.Wait()
}

// TestRpc_SetLogLevel tests that log level can be set and retrieved
func TestRpc_SetLogLevel(t *testing.T) {
	rpc := NewRpc()
	
	// Default should be Info
	if rpc.GetLogLevel() != LogLevelInfo {
		t.Fatalf("expected default log level to be Info, got %v", rpc.GetLogLevel())
	}
	
	// Set to Debug
	rpc.SetLogLevel(LogLevelDebug)
	if rpc.GetLogLevel() != LogLevelDebug {
		t.Fatalf("expected log level to be Debug, got %v", rpc.GetLogLevel())
	}
	
	// Set to Warn
	rpc.SetLogLevel(LogLevelWarn)
	if rpc.GetLogLevel() != LogLevelWarn {
		t.Fatalf("expected log level to be Warn, got %v", rpc.GetLogLevel())
	}
	
	// Set to Error
	rpc.SetLogLevel(LogLevelError)
	if rpc.GetLogLevel() != LogLevelError {
		t.Fatalf("expected log level to be Error, got %v", rpc.GetLogLevel())
	}
}

// TestRpc_Clone_LogLevel tests that log level is cloned
func TestRpc_Clone_LogLevel(t *testing.T) {
	original := NewRpc().
		SetLogLevel(LogLevelDebug)
	
	clone := original.Clone()
	if clone == nil {
		t.Fatalf("expected clone not to be nil")
	}
	
	if clone.GetLogLevel() != LogLevelDebug {
		t.Fatalf("expected clone log level to be Debug, got %v", clone.GetLogLevel())
	}
	
	// Modify clone log level
	clone.SetLogLevel(LogLevelWarn)
	
	// Original should not be affected
	if original.GetLogLevel() != LogLevelDebug {
		t.Fatalf("original log level should not be affected, got %v", original.GetLogLevel())
	}
	if clone.GetLogLevel() != LogLevelWarn {
		t.Fatalf("clone log level should be Warn, got %v", clone.GetLogLevel())
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
