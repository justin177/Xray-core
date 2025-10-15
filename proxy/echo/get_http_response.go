package echo

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/errors"
	"google.golang.org/protobuf/proto"
)

func (x *GetHttpResponse) Build() (proto.Message, error) { return x, nil }

func (x *GetHttpResponse) toSr(request *http.Request) (*StaticHttpResponse, error) {
	targetUrl := x.GetTargetUrl()
	if request != nil && request.URL != nil {
		targetUrl += request.URL.String()
	}
	errors.LogInfo(context.Background(), "targetUrl: ", targetUrl)
	httpReq, err := http.NewRequest("GET", targetUrl, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range x.GetHeaders() {
		httpReq.Header.Add(k, v)
	}
	httpResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = httpResp.Body.Close()
	}()
	respData, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}
	sr := &StaticHttpResponse{
		Version: fmt.Sprintf("%v.%v", httpResp.ProtoMajor, httpResp.ProtoMinor),
		Status:  fmt.Sprint(httpResp.StatusCode),
		Reason:  http.StatusText(httpResp.StatusCode),
		Headers: make(map[string]string, len(httpResp.Header)),
		Body:    string(respData),
	}
	for k, vs := range httpResp.Header {
		sr.Headers[k] = strings.Join(vs, ";")
	}
	return sr, nil
}

func (x *GetHttpResponse) WriteToClient(request *http.Request, writer io.Writer) (int, error) {
	sr, err := x.toSr(request)
	if err != nil {
		return 0, err
	}
	return sr.WriteToClient(request, writer)
}

func (x *GetHttpResponse) WriteToServer(request *http.Request, writer buf.Writer) (int, error) {
	sr, err := x.toSr(request)
	if err != nil {
		return 0, err
	}
	return sr.WriteToServer(request, writer)
}
