package handler

import (
	"context"
	"fmt"
	"strings"
	"sync"

	uuid "github.com/satori/go.uuid"
	"github.com/winglq/l4proxy/src/api"
)

type Handler struct {
	host    string
	clients map[string]*Client
	mu      sync.Mutex
}

func New(host string) *Handler {
	h := &Handler{
		host:    host,
		clients: map[string]*Client{},
	}
	return h
}

// CreateClient creates a new internal listener for clients.
// Whether public listener is unique depends on request parameter.
func (h *Handler) CreateClient(req *api.CreateClientRequest, svr api.ControlService_CreateClientServer) error {
	ctx := svr.Context()
	uid := strings.Replace(uuid.NewV1().String(), "-", "", -1)
	c, err := NewClient(uid, req.DisplayName, h.host, fmt.Sprintf("%d", req.PublicPort), fmt.Sprintf("%d", req.InternalPort), req.SharePublicAddr)
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
		SharePublicAddr: req.SharePublicAddr,
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
			h.mu.Lock()
			defer h.mu.Unlock()
			delete(h.clients, c.name)
			return nil
		}
	}
}

func (h *Handler) ListClients(ctx context.Context, req *api.ListClientsRequest) (*api.ListClientsResponse, error) {
	cs := []*api.Client{}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range h.clients {
		c := &api.Client{
			Name:            c.name,
			DisplayName:     c.displayName,
			InternalAddress: c.IntAddr(),
			PublicAddress:   c.PubAddr(),
			SharePublicAddr: c.sharePub,
		}
		cs = append(cs, c)
	}
	length := int32(len(h.clients))
	return &api.ListClientsResponse{
		TotalCount: length,
		Clients:    cs,
	}, nil
}

func (h *Handler) ListBackendServiceUsers(ctx context.Context, req *api.ListBackendServiceUsersRequest) (*api.ListBackendServiceUsersResponse, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
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
