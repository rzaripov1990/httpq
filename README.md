# HTTPQ v3

HTTPQ is a lightweight, chainable HTTP client for Go that focuses on:

- **Typed responses** via generics and `ResponseModel[T]`
- **Convenient builder API** (`Get().SetUrl(...).Json().SetBody(...)`)
- **Structured logging** with `log/slog`
- **Safe handling of JSON, XML, HTML, media and arbitrary binary content**

## Installation

```bash
go get github.com/rzaripov1990/httpq/v3
```

Import:

```go
import "github.com/rzaripov1990/httpq/v3"
```

## Quick Start

### Basic GET returning JSON

```go
package main

import (
    "context"
    "fmt"

    httpq "github.com/rzaripov1990/httpq/v3"
)

type User struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

func main() {
    rpc := httpq.NewRpc().
        Get().
        SetUrl("https://api.example.com/user/1").
        Json().
        SetLogging(true)

    resp, err := httpq.Do[User](context.Background(), rpc)
    if err != nil {
        panic(err)
    }

    fmt.Println("status:", resp.StatusCode)
    fmt.Println("user:", resp.Data)
}
```

### POST with JSON body

```go
type CreateUserRequest struct {
    Name string `json:"name"`
    Age  int    `json:"age"`
}

type CreateUserResponse struct {
    ID string `json:"id"`
}

func createUser(ctx context.Context) (*httpq.ResponseModel[CreateUserResponse], error) {
    reqBody := CreateUserRequest{
        Name: "John",
        Age:  30,
    }

    rpc := httpq.NewRpc().
        Post().
        SetUrl("https://api.example.com/users").
        Json().
        SetBody(reqBody).
        SetLogging(true)

    return httpq.Do[CreateUserResponse](ctx, rpc)
}
```

### Request with headers

```go
rpc := httpq.NewRpc().
    Get().
    SetUrl("https://api.example.com/data").
    SetHeader(map[string]string{
        "Authorization": "Bearer token",
        "Custom-Header": "value",
    }).
    Json()
```

### Multipart request (file upload)

```go
type UploadForm struct {
    File    []byte
    Name    string
    Comment string
}

func upload(ctx context.Context, fileBytes []byte) (*httpq.ResponseModel[struct{}], error) {
    rpc := httpq.NewRpc().
        Post().
        SetUrl("https://api.example.com/upload").
        MultiPart().
        SetBody(UploadForm{
            File:    fileBytes,
            Name:    "document.pdf",
            Comment: "Important document",
        })

    return httpq.Do[struct{}](ctx, rpc)
}
```

## Core Concepts

### `Rpc` builder

`Rpc` describes a single HTTP call and is configured with chainable methods:

- **HTTP method**
  - `Get()`, `Post()`, `Put()`, `Delete()`, `Patch()`, `Head()`, `Options()`
  - or generic `Method(string)`
- **URL & body**
  - `SetUrl(string)`
  - `SetBody(any)`
  - `SetHeader(map[string]string)`
- **Content type**
  - `Json()` – `application/json`
  - `Xml()` – `application/xml`
  - `Bytes()` – arbitrary bytes
  - `MultiPart()` – `multipart/form-data`
  - `None()` – no body
  - or explicit `SetContentType(httpq.ContentType)`
- **HTTP client configuration**
  - `SetTransport(http.RoundTripper)`
  - `SetRedirectFunc(func(req *http.Request, via []*http.Request) error)`
  - `SetTimeout(time.Duration)`
- **Cookies**
  - `SetCookies(map[string]string)` – formats and sets the `Cookie` header
- **Logging**
  - `SetLogging(bool)`
  - `SetLogger(*slog.Logger)`
  - `Clone()` – copy all settings except the body (useful for templated requests)

### `Do` and `ResponseModel[T]`

`Do` executes the request and returns a typed response wrapper:

```go
resp, err := httpq.Do[MyType](ctx, rpc, /* optional traceID ...string */)
```

`ResponseModel[T]` contains:

- `Data T` – deserialized response (for JSON/XML)
- `StatusCode int` – HTTP status code
- `Headers map[string]string` – flattened response headers
- `ContentType string` – value of `Content-Type`
- `RawBody []byte` – raw response bytes (HTML, media, binary, etc.)

Example:

```go
type User struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

resp, err := httpq.Do[User](ctx, rpc)
if err != nil {
    // handle error
}

user := resp.Data
code := resp.StatusCode
raw := resp.RawBody
```

## Content Handling

Behavior depends on the `Content-Type` header and payload:

- **JSON** (`*json*`)
  - Parsed into `Data` using `json.Unmarshal` into `T`
  - `RawBody` always contains the original bytes
- **XML** (`*xml*` or body starting with `<`)  
  - Parsed into `Data` using `xml.Unmarshal` into `T`
- **HTML / text** (`text/*`, `*html*`)
  - Body is treated as text:
    - Logged as string (if logging is enabled)
    - Available via `RawBody`
  - `Data` is filled only if `T` is compatible with XML/JSON payload
- **Media / binary** (`image/*`, `application/pdf`, `application/octet-stream`, etc.)
  - No attempt is made to deserialize into `Data`
  - `RawBody` contains the full content

This lets you use the same API both for JSON/XML APIs and for downloading arbitrary binary resources.

## Structured Logging and Trace ID

HTTPQ v3 uses `log/slog` for structured logging.

### Injecting a custom logger

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

rpc := httpq.NewRpc().
    SetLogger(logger).
    SetLogging(true)
```

When logging is enabled, the library logs:

- **Request**: method, URL, headers, content type, body (for text types)
- **Response**: method, URL, status, status code, duration, content type, content length
  - full body for text types (JSON, XML, HTML, `text/*`)
  - only metadata (with `binary=true`) for binary/media content
- **Errors**: request creation, network errors, JSON/XML deserialization errors

### Optional trace ID

You can pass an optional trace ID to `Do`:

```go
resp, err := httpq.Do[User](ctx, rpc, "trace-12345")
```

The trace ID is:

- included in all log records related to this request (`trace_id` field)
- automatically propagated in the `X-Trace-Id` header if it is not already set

## Retry Policy

You can configure automatic retries for transient failures via `RetryPolicy`:

```go
rpc := httpq.NewRpc().
    Get().
    SetUrl("https://api.example.com/data").
    Json().
    SetRetryPolicy(&httpq.RetryPolicy{
        MaxRetries:  3,
        Backoff:     200 * time.Millisecond,
        Exponential: true, // use exponential backoff: 200ms, 400ms, 800ms...
        // Retry only on specific status codes:
        RetryStatusCodes: httpq.DefaultRecommendedRetryStatusCodes,
    })
```

`Do` will:

- retry connection errors (no HTTP response, code 0)
- retry only on statuses listed in `RetryStatusCodes`
- stop retrying when:
  - max attempts are exhausted,
  - a non-retryable status code (not in `RetryStatusCodes`) is returned,
  - the context is cancelled or times out

You can use the built-in presets:

```go
// A recommended set of 5xx codes that are typically safe to retry.
httpq.DefaultRetryable5xx

// A broader set including 5xx and common transient 4xx such as 408 and 429.
httpq.DefaultRecommendedRetryStatusCodes
```

For example:

```go
rpc := httpq.NewRpc().
    Get().
    SetUrl("https://api.example.com/data").
    Json().
    SetRetryPolicy(&httpq.RetryPolicy{
        MaxRetries:       5,
        Backoff:          100 * time.Millisecond,
        Exponential:      true,
        RetryStatusCodes: httpq.DefaultRetryable5xx,
    })
```

## Testing

The project includes:

- unit and integration-style tests built on `httptest.Server`, which:
  - emulate JSON success responses
  - verify cookie handling
  - validate retry behavior with `RetryPolicy`

You can run all tests with:

```bash
go test ./...
```
