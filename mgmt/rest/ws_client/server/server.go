package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"golang.org/x/net/websocket"
	"log"
	"net/http"
	"net/http/httputil"
	"net/rpc"
	"net/rpc/jsonrpc"
	"sync"
	"time"

	"github.com/codeskyblue/go-uuid"
	"github.com/gorilla/mux"
	"github.com/intelsdi-x/snap/mgmt/rest/client"
	"github.com/intelsdi-x/snap/mgmt/rest/ws_client"
	"github.com/urfave/negroni"
)

type Session struct {
	Id         string
	Name       string
	socket     *websocket.Conn
	snapClient *client.Client
	jsonrpcCli *rpc.Client
}

type SessionStore struct {
	sessions map[string]*Session
	sync.Mutex
}

func (s *SessionStore) Get(id string) *Session {
	s.Lock()
	defer s.Unlock()
	return s.sessions[id]
}

func (s *SessionStore) Put(id string, sess *Session) {
	s.Lock()
	s.sessions[id] = sess
	s.Unlock()
}

func (s *SessionStore) Delete(id string) {
	s.Lock()
	delete(s.sessions, id)
	s.Unlock()
}

func (s *SessionStore) All() []*Session {
	s.Lock()
	currentSessions := make([]*Session, len(s.sessions))
	i := 0
	for _, sess := range s.sessions {
		currentSessions[i] = sess
		i++
	}
	s.Unlock()
	return currentSessions
}

var sessionsStore *SessionStore

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
	sessionsStore = &SessionStore{sessions: make(map[string]*Session)}
	rtr := mux.NewRouter()
	http.Handle("/", rtr)
	rtr.Handle("/ws", websocket.Handler(websocketHandler))
	rtr.PathPrefix("/v1/").HandlerFunc(proxyHandler)
	rtr.HandleFunc("/nodes", nodesHandler)
	n := negroni.Classic() // Includes some default middlewares
	n.UseHandler(rtr)
	n.Run(":7000")
}

func nodesHandler(w http.ResponseWriter, r *http.Request) {
	nodes := make([]string, 0)
	for _, sess := range sessionsStore.All() {
		nodes = append(nodes, sess.Name)
	}
	body, err := json.Marshal(nodes)
	if err != nil {
		panic(err)
	}
	w.Write(body)
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	node := query.Get("proxy_node")

	if node == "" {
		w.WriteHeader(400)
		w.Write([]byte("no proxy_node url param present."))
		return
	}

	session := sessionsStore.Get(node)
	if session == nil {
		w.WriteHeader(404)
		w.Write([]byte("Not found"))
		return
	}
	// modify the URL PATH to strip out the request to the node.
	director := func(req *http.Request) {
		query := req.URL.Query()
		query.Del("proxy_node")
		req.URL.RawQuery = query.Encode()
	}

	proxy := httputil.ReverseProxy{
		Director:  director,
		Transport: &wsRoundTripper{c: session.jsonrpcCli},
	}
	proxy.ServeHTTP(w, r)
}

func websocketHandler(ws *websocket.Conn) {
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

	meta := make(map[string]string)
	err = c.Call("SnapClientOverWebsocket.Metadata", session.Id, &meta)
	if err != nil {
		log.Printf("SnapClientOverWebsocket.Metadata error:%s", err)
		ws.Close()
		return
	}
	session.Name = meta["name"]
	sessionsStore.Put(session.Name, session)
	defer sessionsStore.Delete(session.Name)
	session.Run()
}

func (s *Session) Run() {
	defer s.socket.Close()
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
		}
		close(done)
	}(done)

	<-done
}
