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
go get github.com/rzaripov1990/httpq/v2
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