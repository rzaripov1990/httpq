package httpq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
)

var (
	GlobalLoggingFlag = false
)

type Httpq struct {
	client                  *http.Client
	method                  string
	url                     string
	body                    any
	header                  map[string]string
	logging                 bool
	loggingNetworkError     bool
	returnErrorGt200        bool
	ignoreStatusCodeAsError []int
}

func New() *Httpq {
	return &Httpq{
		client: http.DefaultClient,
	}
}

func (r *Httpq) Method(val string) *Httpq {
	r.method = val
	return r
}

func (r *Httpq) Url(val string) *Httpq {
	r.url = val
	return r
}

func (r *Httpq) Body(val any) *Httpq {
	r.body = val
	return r
}

func (r *Httpq) Header(val map[string]string) *Httpq {
	r.header = val
	return r
}

func (r *Httpq) Logging(val bool, onlyNetworkError ...bool) *Httpq {
	r.logging = val
	r.loggingNetworkError = func() bool {
		if len(onlyNetworkError) > 0 {
			return onlyNetworkError[0]
		}
		return false
	}()
	return r
}

func (r *Httpq) ReturnErrorGt200(val bool, ignoreStatusCodeAsError ...int) *Httpq {
	r.returnErrorGt200 = val
	r.ignoreStatusCodeAsError = ignoreStatusCodeAsError
	return r
}

func Do[T any](ctx context.Context, r *Httpq) (result *T, err error) {
	body := new(bytes.Buffer)
	req := new(http.Request)
	resp := new(http.Response)

	if r.body != nil {
		_ = json.NewEncoder(body).Encode(r.body)
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
