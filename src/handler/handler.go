package handler

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/winglq/l4proxy/src/api"
)

type Handler struct {
	done chan struct{}
	host string
}

func New(host string) *Handler {
	h := &Handler{
		host: host,
		done: make(chan struct{}),
	}
	return h
}

func (h *Handler) listen(addr string, ctx context.Context) (string, chan net.Conn, error) {
	ltn, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, err
	}
	ch := make(chan net.Conn)
	go func() {
		select {
		case <-ctx.Done():
		}
		ltn.Close()
	}()
	go func() {
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

// CreateConnection creates a new internal listener for clients.
// Whether public listener is unique depends on request parameter.
func (h *Handler) CreateConnection(req *api.CreateConnectionRequest, svr api.ControlService_CreateConnectionServer) error {
	ctx := svr.Context()
	pairs := Pairs{}
	var token Token = 0
	pubAddr, pubCH, err := h.listen(fmt.Sprintf(":%d", req.PublicPort), ctx)
	intAddr, intCH, err := h.listen(fmt.Sprintf(":%d", req.InternalPort), ctx)
	if err != nil {
		panic(err)
	}
	resp := &api.CreateConnectionResponse{
		Name:            "",
		InternalAddress: "",
		PublicAddress:   pubAddr,
	}
	if err := svr.Send(resp); err != nil {
		panic(err)
	}
	for {
		select {
		case <-ctx.Done():
			for _, p := range pairs {
				p.Close()
			}
			log.Printf("client %s closed.", req.DisplayName)
			return nil
		case c := <-pubCH:
			token = token + 1
			resp := &api.CreateConnectionResponse{
				Name:            token.String(),
				InternalAddress: intAddr,
				PublicAddress:   pubAddr,
			}
			pairs[token.String()] = &PairedConn{
				SRC: c,
			}
			if err := svr.Send(resp); err != nil {
				panic(err)
			}
		case c := <-intCH:
			buf := make([]byte, token.Len())
			_, err := c.Read(buf)
			if err != nil {
				panic(err)
			}
			pair := pairs[string(buf)]
			pair.DEST = c
			pair.OnClose = func() {
				delete(pairs, string(buf))
			}
			log.Printf("new connection pair created. token %s", string(buf))
			pair.Copy()

		}
	}
}
