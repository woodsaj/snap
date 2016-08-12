package ws_client

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"net/rpc"
	"net/rpc/jsonrpc"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/intelsdi-x/snap/mgmt/rest"
	"golang.org/x/net/websocket"
)

var (
	wsClientLogger = log.WithField("_module", "_mgmt-wsClient")
)

type fakeWriter struct {
	status int
	Body   bytes.Buffer
	header http.Header
}

func (w *fakeWriter) Write(b []byte) (int, error) {
	w.Body.WriteString("HTTP/1.1 200 OK\n")
	w.Body.WriteString(fmt.Sprintf("Content-Length: %d\n", len(b)))
	w.Body.WriteString("\n")
	return w.Body.Write(b)
}

func (w *fakeWriter) WriteHeader(code int) {
	w.status = code
}

func (w *fakeWriter) Header() http.Header {
	return w.header
}

func New(remoteAddr, name string, server *rest.Server) *SnapClientOverWebsocket {
	return &SnapClientOverWebsocket{
		Server:     server,
		name:       name,
		remoteAddr: remoteAddr,
	}
}

type SnapClientOverWebsocket struct {
	remoteAddr string
	Server     *rest.Server
	Socket     *websocket.Conn
	name       string
}

type WsClientPayload struct {
	Data []byte
}

func (s *SnapClientOverWebsocket) Start() error {
	wsClientLogger.Info(fmt.Sprintf("connecting to ws_server at %s", s.remoteAddr))
	ws, err := websocket.Dial(s.remoteAddr, "", "http://localhost/")
	if err != nil {
		return err
	}
	s.Socket = ws
	rpc.Register(s)
	go func() {
		wsClientLogger.Info("starting jsonrpc server")
		jsonrpc.ServeConn(ws)
	}()
	return nil
}

func (s *SnapClientOverWebsocket) Handle(r WsClientPayload, resp *WsClientPayload) error {
	wsClientLogger.Debugln("handling jsonrpc request")
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewBuffer(r.Data)))
	if err != nil {
		return err
	}
	writer := &fakeWriter{header: make(http.Header)}
	s.Server.ServeHTTP(writer, req)
	resp.Data = writer.Body.Bytes()
	return nil
}

func (s *SnapClientOverWebsocket) Heartbeat(t time.Time, r *time.Time) error {
	*r = time.Now()
	return nil
}
func (s *SnapClientOverWebsocket) Metadata(id string, meta *map[string]string) error {
	*meta = map[string]string{"name": s.name}

	return nil
}

func (s *SnapClientOverWebsocket) Stop() {
	s.Socket.Close()
}

func (s *SnapClientOverWebsocket) Name() string {
	return "WsClient"
}
