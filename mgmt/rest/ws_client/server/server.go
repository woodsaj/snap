package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"golang.org/x/net/websocket"
	"log"
	"net/http"
	"net/rpc"
	"net/rpc/jsonrpc"
	"time"

	"github.com/codeskyblue/go-uuid"
	"github.com/intelsdi-x/snap/mgmt/rest/client"
	"github.com/intelsdi-x/snap/mgmt/rest/rbody"
	"github.com/intelsdi-x/snap/mgmt/rest/ws_client"
)

var sessions map[string]*Session

type wsRoundTripper struct {
	c *rpc.Client
}

func (r *wsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	reply := new(ws_client.WsClientPayload)
	var b bytes.Buffer
	req.Write(&b)
	rpcReq := ws_client.WsClientPayload{Data: b.Bytes()}
	err := r.c.Call("SnapClientOverWebsocket.Handle", rpcReq, reply)
	if err != nil {
		log.Fatal("SnapClientOverWebsocket.Handle error:", err)
	}

	return http.ReadResponse(bufio.NewReader(bytes.NewBuffer(reply.Data)), req)
}

func main() {
	sessions = make(map[string]*Session)
	http.Handle("/ws", websocket.Handler(serveWs))
	http.HandleFunc("/catalog", servePlugins)
	http.ListenAndServe("localhost:7000", nil)

}

func servePlugins(w http.ResponseWriter, r *http.Request) {
	catalog := make([]*rbody.Metric, 0)
	for id, sess := range sessions {
		log.Printf("getting plugins from socket with sessionId: %s", id)
		resp := sess.snapClient.GetMetricCatalog()
		catalog = append(catalog, resp.Catalog...)
	}
	body, err := json.Marshal(catalog)
	if err != nil {
		panic(err)
	}
	w.Write(body)
}

func serveWs(ws *websocket.Conn) {
	log.Printf("Handler starting")
	c := jsonrpc.NewClient(ws)
	hc := &http.Client{
		Transport: &wsRoundTripper{c: c},
	}
	snapClient, err := client.New("http://localhost", "", true, client.HttpClient(hc))
	if err != nil {
		panic(err)
	}
	session := &Session{
		Id:         uuid.NewUUID().String(),
		socket:     ws,
		snapClient: snapClient,
		jsonrpcCli: c,
	}
	sessions[session.Id] = session
	session.Run()
	log.Printf("Handler exiting")
	delete(sessions, session.Id)
}

type Session struct {
	Id         string
	socket     *websocket.Conn
	snapClient *client.Client
	jsonrpcCli *rpc.Client
}

func (s *Session) Run() {

	done := make(chan struct{})
	go func(done chan struct{}) {
		ticker := time.NewTicker(time.Second)
		defer close(done)
		for range ticker.C {
			recv := &time.Time{}
			sent := time.Now()
			err := s.jsonrpcCli.Call("SnapClientOverWebsocket.Heartbeat", sent, recv)
			if err != nil {
				log.Printf("SnapClientOverWebsocket.Heartbeat error:%s", err)
				ticker.Stop()
				return
			}
			log.Printf("Heartbeat took %s", recv.Sub(sent))
		}
		close(done)
	}(done)

	go func(done chan struct{}) {
		ticker := time.NewTicker(time.Second * 5)
		for {
			select {
			case <-ticker.C:
				resp := s.snapClient.GetPlugins(true)
				for _, plugin := range resp.LoadedPlugins {
					log.Printf("found plugin %s", plugin.Name)
				}
			case <-done:
				log.Printf("stopping plugin list")
				ticker.Stop()
				return
			}
		}
	}(done)

	<-done
}
