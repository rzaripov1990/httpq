package httpq

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"slices"
)

var (
	GlobalLoggingFlag = true
)

type ContentType int

const (
	ContentNone ContentType = iota
	ContentJson
	ContentXml
	ContentBytes
	ContentMultiPart
)

type Rpc struct {
	client                  *http.Client
	method                  string
	url                     string
	body                    any
	header                  map[string]string
	logging                 bool
	loggingNetworkError     bool
	returnErrorGt200        bool
	ignoreStatusCodeAsError []int
	conentType              ContentType
	contentTypeString       string
}

func NewRpc() *Rpc {
	dc := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// return http.ErrUseLastResponse
			return nil
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	return &Rpc{
		conentType:        ContentJson,
		contentTypeString: "application/json",
		client:            dc,
	}
}

func (r *Rpc) Method(val string) *Rpc {
	r.method = val
	return r
}

func (r *Rpc) Url(val string) *Rpc {
	r.url = val
	return r
}

func (r *Rpc) Body(val any) *Rpc {
	r.body = val
	return r
}

func (r *Rpc) Header(val map[string]string) *Rpc {
	r.header = val
	return r
}

func (r *Rpc) ContentType(val ContentType) *Rpc {
	r.conentType = val
	return r
}

func (r *Rpc) Logging(val bool, onlyNetworkError ...bool) *Rpc {
	r.logging = val
	r.loggingNetworkError = func() bool {
		if len(onlyNetworkError) > 0 {
			return onlyNetworkError[0]
		}
		return false
	}()
	return r
}

func (r *Rpc) ReturnErrorGt200(val bool, ignoreStatusCodeAsError ...int) *Rpc {
	r.returnErrorGt200 = val
	r.ignoreStatusCodeAsError = ignoreStatusCodeAsError
	return r
}

func Do[T any](ctx context.Context, r *Rpc) (result *T, err error) {
	body := new(bytes.Buffer)
	req := new(http.Request)
	resp := new(http.Response)

	if r.body != nil {
		switch r.conentType {
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
		fmt.Println("request", body.String())
	}

	req, err = http.NewRequestWithContext(ctx, r.method, r.url, body)
	if err != nil {
		return
	}
	for k, v := range r.header {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", r.contentTypeString)

	resp, err = r.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var bodyBytes []byte
	if resp.Body != http.NoBody {
		bodyBytes, _ = io.ReadAll(resp.Body)
		if r.logging {
			fmt.Println("status code", resp.Status)
			fmt.Println("response", string(bodyBytes))
		}

		result = new(T)
		if !bytes.HasPrefix(bodyBytes, []byte("<")) {
			err = json.Unmarshal(bodyBytes, result)
		} else {
			if r.logging {
				fmt.Println("xml?")
			}
		}
	}
	if r.returnErrorGt200 && resp.StatusCode != 200 && !slices.Contains(r.ignoreStatusCodeAsError, resp.StatusCode) {
		err = fmt.Errorf("%d %v", resp.StatusCode, string(bodyBytes))
	}

	return
}
