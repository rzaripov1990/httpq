# HTTPQ

HTTPQ is a convenient wrapper for working with HTTP requests in Go, providing a simple and flexible API for making HTTP requests.

## Key Features

- Support for various content types (JSON, XML, Multipart, Bytes)
- Configurable error handling
- Request and response logging
- Context support
- Customizable HTTP client

## Installation

```bash
go get github.com/rzaripov1990/httpq/v3
```

## Usage Examples

### Basic GET Request

```go
package main

import (
    "context"
    "github.com/rzaripov1990/httpq/v2"
)

type Response struct {
    Message string `json:"message"`
}

func main() {
    rpc := httpq.NewRpc().
        Method("GET").
        Url("https://api.example.com/data").
        Logging(true)

    var result Response
    result, err := httpq.Do[Response](context.Background(), rpc)
    if err != nil {
        panic(err)
    }
}
```

### POST Request with JSON

```go
type Request struct {
    Name string `json:"name"`
    Age  int    `json:"age"`
}

type Response struct {
    ID string `json:"id"`
}

func main() {
    request := Request{
        Name: "John",
        Age:  30,
    }

    rpc := httpq.NewRpc().
        Method("POST").
        Url("https://api.example.com/users").
        Body(request).
        ContentType(httpq.ContentJson).
        Logging(true)

    var result Response
    result, err := httpq.Do[Response](context.Background(), rpc)
    if err != nil {
        panic(err)
    }
}
```

### Request with Headers

```go
rpc := httpq.NewRpc().
    Method("GET").
    Url("https://api.example.com/data").
    Header(map[string]string{
        "Authorization": "Bearer token",
        "Custom-Header": "value",
    })
```

### Error Handling

```go
rpc := httpq.NewRpc().
    Method("GET").
    Url("https://api.example.com/data").
    ReturnErrorGt200(true, 404) // Ignore 404 as an error

var result Response
result, err := httpq.Do[Response](context.Background(), rpc)
if err != nil {
    // Handle error
}
```

### Multipart Request

```go
type FormData struct {
    File    []byte
    Name    string
    Comment string
}

rpc := httpq.NewRpc().
    Method("POST").
    Url("https://api.example.com/upload").
    Body(FormData{
        File:    fileBytes,
        Name:    "document.pdf",
        Comment: "Important document",
    }).
    ContentType(httpq.ContentMultiPart)
```

## Client Configuration

By default, the client is configured with:
- Disabled SSL certificate verification
- Allowed redirects
- Enabled logging

You can modify these settings after client creation:

```go
rpc := httpq.NewRpc()
rpc.client.Timeout = time.Second * 30
```

## Logging

Logging can be enabled/disabled for each request:

```go
rpc := httpq.NewRpc().
    Logging(true) // Enable logging
    // or
    Logging(true, true) // Log only network errors
```

## Content Types

The following content types are supported:
- `ContentJson` - application/json
- `ContentXml` - application/xml
- `ContentBytes` - arbitrary bytes
- `ContentMultiPart` - multipart/form-data
- `ContentNone` - no content

## Response Handling

The `Do` function automatically deserializes the response into the specified type:

```go
type User struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

var user User
user, err := httpq.Do[User](ctx, rpc)
```
If the response cannot be deserialized into the specified type, an error will be returned. 

## New Functionality (v2)

### ResponseModel Wrapper

In the new version, `Do` returns a generic `ResponseModel[T]` that contains:

- `Data T` – deserialized response model
- `StatusCode int` – HTTP status code
- `Headers map[string]string` – response headers
- `ContentType string` – value of the `Content-Type` header
- `RawBody []byte` – raw response body (useful for HTML, media, PDF, images, etc.)

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
status := resp.StatusCode
bodyBytes := resp.RawBody
```

### Fluent Helpers for Methods and Content Types

To make the API more convenient, there are chainable helpers:

- HTTP methods: `Get()`, `Post()`, `Put()`, `Delete()`, `Patch()`, `Head()`, `Options()`
- Content types: `Json()`, `Xml()`, `Bytes()`, `MultiPart()`, `None()`

Example:

```go
rpc := httpq.NewRpc().
    Get().
    SetUrl("https://api.example.com/data").
    Json().
    SetLogging(true)
```

### Extended Configuration Methods

Additional configuration helpers:

- `SetUrl(string)` – set request URL
- `SetBody(any)` – set request body
- `SetHeader(map[string]string)` – set headers
- `SetContentType(ContentType)` – set content type explicitly
- `SetTransport(*http.Transport)` – customize HTTP transport
- `SetRedirectFunc(func(req *http.Request, via []*http.Request) error)` – custom redirect policy
- `SetLogging(bool)` – enable/disable logging

### Structured Logging with slog

The library now uses `log/slog` for structured logging.

You can inject your own logger:

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

rpc := httpq.NewRpc().
    SetLogger(logger).
    SetLogging(true)
```

When logging is enabled, the following is logged:

- Request: method, URL, headers, content type, body (for text types)
- Response: method, URL, status, status code, duration, content type, length, and:
  - full body for text types (JSON, XML, HTML, `text/*`)
  - only metadata (with `binary=true`) for binary/media content (PDF, images, etc.)
- Errors: request creation, network errors, JSON/XML deserialization errors

### Optional Trace ID Support

`Do` supports an optional trace ID parameter that is logged and automatically propagated via the `X-Trace-Id` header (if not already set):

```go
resp, err := httpq.Do[User](ctx, rpc, "trace-12345")
```

The `trace_id` field is included in all log records related to this request.

### Handling HTML, Media and Binary Content

Depending on the `Content-Type` header:

- JSON (`*json*`) – deserialized into `Data` via `json.Unmarshal`
- XML (`*xml*` or body starting with `<`) – deserialized into `Data` via `xml.Unmarshal`
- HTML and other `text/*` – body is logged as text; you can access it via `RawBody`
- Media/binary (`image/*`, `application/pdf`, `application/octet-stream`, etc.) – only metadata is logged, content is available as `RawBody` without attempting to deserialize into `Data`

This allows using the same API both for JSON/XML APIs and for downloading arbitrary binary resources.

## Testing

Unit tests are not yet implemented. They are planned to cover:

- JSON/XML deserialization into `ResponseModel[T]`
- logging behavior (including trace ID)
- handling of HTML and binary/media content