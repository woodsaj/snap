package ws_client

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/rpc"
	"net/rpc/jsonrpc"
	"time"

	"github.com/intelsdi-x/snap/mgmt/rest"
	"golang.org/x/net/websocket"
)

type fakeWriter struct {
	status int
	Body   bytes.Buffer
	header http.Header
}

func (w *fakeWriter) Write(b []byte) (int, error) {
	log.Printf("writeHeader: %d", w.status)
	log.Printf("writeHeader: %v", w.header)
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

func New(remoteAddr string, server *rest.Server) *SnapClientOverWebsocket {
	return &SnapClientOverWebsocket{
		Server:     server,
		remoteAddr: remoteAddr,
	}
}

type SnapClientOverWebsocket struct {
	remoteAddr string
	Server     *rest.Server
	Socket     *websocket.Conn
}

type WsClientPayload struct {
	Data []byte
}

func (s *SnapClientOverWebsocket) Start() error {
	log.Printf("connecting to ws_server at %s", s.remoteAddr)
	ws, err := websocket.Dial(s.remoteAddr, "", "http://localhost/")
	if err != nil {
		return err
	}
	s.Socket = ws
	rpc.Register(s)
	go func() {
		log.Println("starting jsonrpc server")
		jsonrpc.ServeConn(ws)
		log.Println("jsonrpc server started.")
	}()
	return nil
}

func (s *SnapClientOverWebsocket) Handle(r WsClientPayload, resp *WsClientPayload) error {
	log.Println("handling jsonrpc request")
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

func (s *SnapClientOverWebsocket) Stop() {
	s.Socket.Close()
}

func (s *SnapClientOverWebsocket) Name() string {
	return "WsClient"
}
