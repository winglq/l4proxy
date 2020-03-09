package handler

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus/ctxlogrus"
	uuid "github.com/satori/go.uuid"
	"github.com/winglq/l4proxy/src/api"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
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
	log := ctxlogrus.Extract(ctx)
	uid := strings.Replace(uuid.NewV1().String(), "-", "", -1)
	c, err := NewClient(uid, req.DisplayName, h.host, fmt.Sprintf("%d", req.PublicPort), fmt.Sprintf("%d", req.InternalPort), req.SharePublicAddr, log)
	if err != nil {
		return err
	}

	pr, ok := peer.FromContext(ctx)
	if !ok {
		panic("no peer info in context")
	}
	host, _, err := net.SplitHostPort(pr.Addr.String())
	if err != nil {
		panic(err)
	}

	stun := func() bool {
		if req.Protocol != "tcp" {
			return false
		}
		addr := net.JoinHostPort(host, fmt.Sprintf("%d", req.BackendPort))
		stunConn, err := net.DialTimeout("tcp", addr, time.Second*5)
		if err != nil {
			return false
		}
		stunConn.Close()
		resp := &api.Client{
			Name:            "",
			InternalAddress: "",
			PublicAddress:   addr,
			DisplayName:     "",
		}
		if err := svr.Send(resp); err != nil {
			panic(err)
		}
		c.SetSTUNInfo(host, fmt.Sprintf("%d", req.BackendPort), req.Protocol)
		h.clients.Store(uid, c)
		return true
	}
	if !stun() {
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
	iClient, ok := h.clients.Load(req.Parent)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "%s does not found", req.Parent)
	}
	iClient.(*Client).connPairs.Range(func(k, v interface{}) bool {
		p := v.(*PairedConn)
		u := &api.BackendServiceUser{
			UserAddr: p.SRC.RemoteAddr().String(),
			SpeedIn:  p.SpeedIn,
			SpeedOut: p.SpeedOut,
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

type InternalService struct {
	PublicPort  int32
	Name        string
	Close       func()
	ServiceName string
}

var services sync.Map

func init() {
	services = sync.Map{}
}
func (h *Handler) StartInternalService(ctx context.Context, req *api.StartInternalServiceRequest) (*api.InternalService, error) {
	if req.ServiceName == "l7forwarder" {
		proxy := goproxy.NewProxyHttpServer()
		var srv *http.Server
		go func() {
			l, err := net.Listen("tcp", fmt.Sprintf(":%d", req.PubPort))
			if err != nil {
				panic(err)
			}
			srv = &http.Server{Handler: proxy}
			srv.Serve(l)
		}()
		uid := strings.Replace(uuid.NewV1().String(), "-", "", -1)
		services.Store(uid, &InternalService{
			PublicPort:  req.PubPort,
			Name:        uid,
			ServiceName: req.ServiceName,
			Close: func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
				defer cancel()
				err := srv.Shutdown(ctx)
				if err != nil {
					panic(err)
				}
			},
		})
		return &api.InternalService{
			Name: uid,
		}, nil
	}
	return nil, status.Errorf(codes.NotFound, "service %s does not found", req.ServiceName)
}
