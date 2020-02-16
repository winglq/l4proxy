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
	clients sync.Map
}

func New(host string) *Handler {
	h := &Handler{
		host: host,
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

	h.clients.Store(uid, c)
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
			h.clients.Delete(c.name)
			return nil
		}
	}
}

func (h *Handler) ListClients(ctx context.Context, req *api.ListClientsRequest) (*api.ListClientsResponse, error) {
	cs := []*api.Client{}
	var count int32 = 0
	h.clients.Range(func(k, v interface{}) bool {
		c := v.(*Client)
		count += 1
		cc := &api.Client{
			Name:            c.name,
			DisplayName:     c.displayName,
			InternalAddress: c.IntAddr(),
			PublicAddress:   c.PubAddr(),
			SharePublicAddr: c.sharePub,
		}
		cs = append(cs, cc)
		return true
	})
	return &api.ListClientsResponse{
		TotalCount: count,
		Clients:    cs,
	}, nil
}

func (h *Handler) ListBackendServiceUsers(ctx context.Context, req *api.ListBackendServiceUsersRequest) (*api.ListBackendServiceUsersResponse, error) {
	us := []*api.BackendServiceUser{}
	var count int32 = 0
	iClient, _ := h.clients.Load(req.Parent)
	iClient.(*Client).connPairs.Range(func(k, v interface{}) bool {
		p := v.(*PairedConn)
		u := &api.BackendServiceUser{
			UserAddr: p.SRC.RemoteAddr().String(),
		}
		us = append(us, u)
		count += 1
		return true
	})
	return &api.ListBackendServiceUsersResponse{
		TotalCount: count,
		Users:      us,
	}, nil
}
