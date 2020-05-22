package forwarder

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/elazarl/goproxy"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/winglq/l4proxy/src/api"
	"github.com/winglq/l4proxy/src/client"
	"github.com/winglq/l4proxy/src/handler"
)

var done chan struct{} = nil

func Run(clientName string, svrAddr string) {
	if done != nil {
		return
	}
	done = make(chan struct{})
	opt := client.Options{
		SvrAddr: svrAddr,
		Name:    clientName,
	}
	l := ForwarderListen()
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = true
	var srv *http.Server
	go func() {
		srv = &http.Server{Handler: proxy}
		srv.Serve(l)
	}()
	go func() {
		<-done
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()
		err := srv.Shutdown(ctx)
		if err != nil {
			panic(err)
		}
		logrus.Info("forwarder server closed")
	}()

	runFn := client.CreateRunFunc(done, &opt, l.(*forwarderListener).httpForwarder)
	runFn(nil, nil)

}

type forwarderListener struct {
	connCH chan net.Conn
	done   chan struct{}
}

func ForwarderListen() net.Listener {
	return &forwarderListener{
		connCH: make(chan net.Conn),
		done:   make(chan struct{}),
	}
}

func (fl *forwarderListener) Accept() (net.Conn, error) {
	select {
	case c := <-fl.connCH:
		return c, nil
	case <-fl.done:
		return nil, fmt.Errorf("closed")

	}
}

func (fl *forwarderListener) Close() error {
	close(fl.done)
	return nil
}

type FakeAddr struct {
}

func (f FakeAddr) String() string {
	return "127.0.0.1:123"
}

func (f FakeAddr) Network() string {
	return "tcp"
}

func (fl *forwarderListener) Addr() net.Addr {
	return FakeAddr{}
}

func (fl *forwarderListener) httpForwarder(resp *api.Client, host, port string) *handler.PairedConn {
	c, err := net.Dial("tcp", resp.InternalAddress)
	if err != nil {
		panic(err)
	}
	_, err = c.Write([]byte(resp.Token))
	if err != nil {
		panic(err)
	}
	fl.connCH <- c
	return nil
}

func Close() {
	if done != nil {
		close(done)
		done = nil
	}
}

func NewForwarderBackendCmd(opt *client.Options) *cobra.Command {
	cmd := &cobra.Command{
		Use: "forwarder",
		Run: func(cmd *cobra.Command, args []string) {
			done = make(chan struct{})
			l := ForwarderListen()
			proxy := goproxy.NewProxyHttpServer()
			proxy.Verbose = true
			var srv *http.Server
			go func() {
				srv = &http.Server{Handler: proxy}
				srv.Serve(l)
			}()
			go func() {
				<-done
				ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
				defer cancel()
				err := srv.Shutdown(ctx)
				if err != nil {
					panic(err)
				}
				logrus.Info("forwarder server closed")
			}()

			run := client.CreateRunFunc(done, opt, l.(*forwarderListener).httpForwarder)
			run(cmd, args)
		},
	}
	cmd.Flags().Int32Var(&opt.PubPort, "pub_port", 0, "public port for this client.")
	cmd.Flags().Int32Var(&opt.IntPort, "int_port", 0, "internal port used to listen client connection.")
	cmd.Flags().StringVar(&opt.Name, "client_name", "unknown", "client name")
	cmd.Flags().BoolVar(&opt.SharePub, "share_public_port", false, "share public port for different clients")
	return cmd
}
