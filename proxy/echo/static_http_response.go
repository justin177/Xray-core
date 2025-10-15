package echo

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"google.golang.org/protobuf/proto"
)

func (x *StaticHttpResponse) Build() (proto.Message, error) { return x, nil }

func (x *StaticHttpResponse) toBuf() []byte {
	b := &bytes.Buffer{}
	common.Must2(b.WriteString("HTTP/"))
	common.Must2(b.WriteString(x.GetVersion()))
	common.Must(b.WriteByte(' '))
	common.Must2(b.WriteString(x.GetStatus()))
	common.Must(b.WriteByte(' '))
	common.Must2(b.WriteString(x.GetReason()))
	common.Must2(b.WriteString("\r\n"))
	for k, v := range x.Headers {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		common.Must2(b.WriteString(k))
		common.Must2(b.WriteString(": "))
		common.Must2(b.WriteString(v))
		common.Must2(b.WriteString("\r\n"))
	}
	{
		common.Must2(b.WriteString("Content-Length"))
		common.Must2(b.WriteString(": "))
		common.Must2(b.WriteString(fmt.Sprint(len(x.GetBody()))))
		common.Must2(b.WriteString("\r\n"))
	}
	common.Must2(b.WriteString("\r\n"))
	common.Must2(b.WriteString(x.GetBody()))
	return b.Bytes()
}

func (x *StaticHttpResponse) WriteToClient(request *http.Request, writer io.Writer) (int, error) {
	b := x.toBuf()
	return writer.Write(b)
}

func (x *StaticHttpResponse) WriteToServer(request *http.Request, writer buf.Writer) (int, error) {
	b := x.toBuf()
	return len(b), writer.WriteMultiBuffer(buf.MultiBuffer{buf.FromBytes(b)})
}
