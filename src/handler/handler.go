package handler

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	uuid "github.com/satori/go.uuid"
	"github.com/winglq/l4proxy/src/api"
)

type Handler struct {
	done    chan struct{}
	host    string
	clients map[string]*Client
	mu      sync.Mutex
}

func New(host string) *Handler {
	h := &Handler{
		host:    host,
		clients: map[string]*Client{},
		done:    make(chan struct{}),
	}
	return h
}

func (h *Handler) listen(addr string, ctx context.Context, wg *sync.WaitGroup) (string, chan net.Conn, error) {
	ltn, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, err
	}
	ch := make(chan net.Conn)
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
		}
		ltn.Close()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(ch)
		for {
			c, err := ltn.Accept()
			if err != nil && strings.Contains(err.Error(), "use of closed network connection") {
				return
			} else if err != nil {
				panic(err)
			}
			ch <- c
		}
	}()
	_, port, _ := net.SplitHostPort(ltn.Addr().String())
	return fmt.Sprintf("%s:%s", h.host, port), ch, nil
}

// CreateClient creates a new internal listener for clients.
// Whether public listener is unique depends on request parameter.
func (h *Handler) CreateClient(req *api.CreateClientRequest, svr api.ControlService_CreateClientServer) error {
	ctx := svr.Context()
	uid := strings.Replace(uuid.NewV1().String(), "-", "", -1)
	c, err := NewClient(uid, req.DisplayName, h.host, fmt.Sprintf("%d", req.PublicPort), fmt.Sprintf("%d", req.InternalPort))
	if err != nil {
		return err
	}

	h.mu.Lock()
	h.clients[uid] = c
	h.mu.Unlock()
	c.Start()
	resp := &api.Client{
		Name:            "",
		InternalAddress: "",
		DisplayName:     "",
		PublicAddress:   c.PubAddr(),
	}
	if err := svr.Send(resp); err != nil {
		panic(err)
	}

	for {
		select {
		case token := <-c.NewPubConnNotifyCH:
			resp := &api.Client{
				Name:            uid,
				Token:           token.String(),
				InternalAddress: c.IntAddr(),
				PublicAddress:   c.PubAddr(),
				DisplayName:     req.DisplayName,
			}
			if err := svr.Send(resp); err != nil {
				panic(err)
			}
		case <-ctx.Done():
			c.Close()
			delete(h.clients, c.name)
			return nil

		}
	}
}

func (h *Handler) ListClients(ctx context.Context, req *api.ListClientsRequest) (*api.ListClientsResponse, error) {
	cs := []*api.Client{}
	h.mu.Lock()
	for _, c := range h.clients {
		c := &api.Client{
			Name:            c.name,
			DisplayName:     c.displayName,
			InternalAddress: c.IntAddr(),
			PublicAddress:   c.PubAddr(),
		}
		cs = append(cs, c)
	}
	length := int32(len(h.clients))
	h.mu.Unlock()
	return &api.ListClientsResponse{
		TotalCount: length,
		Clients:    cs,
	}, nil
}

func (h *Handler) ListBackendServiceUsers(ctx context.Context, req *api.ListBackendServiceUsersRequest) (*api.ListBackendServiceUsersResponse, error) {
	us := []*api.BackendServiceUser{}
	for _, p := range h.clients[req.Parent].connPairs {
		u := &api.BackendServiceUser{
			UserAddr: p.SRC.RemoteAddr().String(),
		}
		us = append(us, u)
	}
	return &api.ListBackendServiceUsersResponse{
		TotalCount: int32(len(h.clients[req.Parent].connPairs)),
		Users:      us,
	}, nil
}
