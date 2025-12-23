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
	ResponseModel[T any] struct {
		Data        T
		StatusCode  int
		Headers     map[string]string
		ContentType string
		RawBody     []byte
	}
	ContentType int
)

const (
	ContentNone ContentType = iota
	ContentJson
	ContentXml
	ContentBytes
	ContentMultiPart
)

type Rpc struct {
	client             *http.Client
	method             string
	url                string
	body               any
	header             map[string]string
	logging            bool
	logger             *slog.Logger
	contentType        ContentType
	contentTypeString  string
	insecureSkipVerify bool
}

func NewRpc() *Rpc {
	dc := &http.Client{
		CheckRedirect: nil,
	}
	r := &Rpc{
		contentType:       ContentJson,
		contentTypeString: "application/json",
		client:            dc,
	}
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

func Do[T any](ctx context.Context, r *Rpc, traceID ...string) (result *ResponseModel[T], err error) {
	body := new(bytes.Buffer)
	req := new(http.Request)
	resp := new(http.Response)
	start := time.Now()
	var tID string
	if len(traceID) > 0 {
		tID = traceID[0]
	}

	if r.body != nil {
		switch r.contentType {
		case ContentJson:
			_ = json.NewEncoder(body).Encode(r.body)
			r.contentTypeString = "application/json"
		case ContentXml:
			_ = xml.NewEncoder(body).Encode(r.body)
			r.contentTypeString = "application/xml"
		case ContentBytes:
			_, _ = body.Write(r.body.([]byte))
		case ContentMultiPart:
			values := map[string]any{}
			bts, err := json.Marshal(r.body)
			if err != nil {
				panic(err)
			}
			_ = json.Unmarshal(bts, &values)
			w := multipart.NewWriter(body)
			for k, v := range values {
				wfield, _ := w.CreateFormField(k)
				wfield.Write([]byte(fmt.Sprintf("%v", v)))
			}

			r.contentTypeString = w.FormDataContentType()
			w.Close()
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

	req, err = http.NewRequestWithContext(ctx, r.method, r.url, body)
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
		// Добавляем trace_id в заголовок, если ещё не установлен пользователем
		if req.Header.Get("X-Trace-Id") == "" {
			req.Header.Set("X-Trace-Id", tID)
		}
	}

	resp, err = r.client.Do(req)
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

	var bodyBytes []byte
	if resp.Body != http.NoBody {
		bodyBytes, _ = io.ReadAll(resp.Body)
		ct := resp.Header.Get("Content-Type")

		if r.logging {
			// Определяем, текстовый ли это ответ (json/xml/html/любой text/*)
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
				// Бинарный/медиа контент — не логируем тело целиком
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

		// Инициализируем модель ответа
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

		// Разбираем тело в Data, если это JSON или XML
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
			// бинарные/медиа типы — в Data не парсим, оставляем только RawBody
		}
	}

	return
}
