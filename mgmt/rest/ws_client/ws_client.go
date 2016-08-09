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

type WsClient struct {
	Server *rest.Server
	ws     *websocket.Conn
}

func New(remoteAddr string, server *rest.Server) {
	origin := "http://localhost/"
	ws, err := websocket.Dial(remoteAddr, "", origin)
	if err != nil {
		log.Fatal(err)
	}
	scow := &SnapClientOverWebsocket{Server: server}
	rpc.Register(scow)
	log.Println("starting jsonrcp server")
	go jsonrpc.ServeConn(ws)
	log.Println("jsonrpc server started.")
}

type SnapClientOverWebsocket struct {
	Server *rest.Server
}

type WsClientPayload struct {
	Data []byte
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
	log.Printf("recieved heartbeat.  Delay %s", time.Since(t))
	*r = time.Now()
	return nil
}
