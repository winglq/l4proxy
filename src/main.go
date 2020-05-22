package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_logrus "github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus"
	grpc_validator "github.com/grpc-ecosystem/go-grpc-middleware/validator"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/winglq/l4proxy/src/api"
	"github.com/winglq/l4proxy/src/client/cmd"
	"github.com/winglq/l4proxy/src/client/forwarder"
	"github.com/winglq/l4proxy/src/handler"
	"google.golang.org/grpc"
)

type ServerOptions struct {
	ctlAddr string
	host    string
}

var opt ServerOptions
var cSig = make(chan os.Signal)
var done = make(chan struct{})

func init() {
	signal.Notify(cSig, os.Interrupt)
	log.SetLevel(log.DebugLevel)
	go func() {
		<-cSig
		close(done)
		cmd.Close()
		forwarder.Close()
	}()
}

func newLANCmd() *cobra.Command {
	pubPort := "22"
	cmd := &cobra.Command{
		Use: "lan [host] [port]",
		Run: func(cmd *cobra.Command, args []string) {
			port := "22"
			host := "127.0.0.1"
			if len(args) > 0 {
				host = args[0]
			}
			if len(args) > 1 {
				port = args[1]
			}
			proxy(":"+pubPort, fmt.Sprintf("%s:%s", host, port))
		},
	}
	cmd.Flags().StringVar(&pubPort, "pub_port", pubPort, "public port")
	return cmd
}

func newServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "server",
		Run: func(cmd *cobra.Command, args []string) {
			ctlLtn, err := net.Listen("tcp", opt.ctlAddr)
			if err != nil {
				panic(err)
			}
			log.Printf("listen on ctl addr %s", ctlLtn.Addr())
			entry := log.WithFields(log.Fields{})
			grpc_logrus.ReplaceGrpcLogger(entry)
			grpcServer := grpc.NewServer(
				grpc_middleware.WithUnaryServerChain(
					grpc_logrus.UnaryServerInterceptor(entry),
					grpc_validator.UnaryServerInterceptor(),
				),
				grpc_middleware.WithStreamServerChain(
					grpc_logrus.StreamServerInterceptor(entry),
					grpc_validator.StreamServerInterceptor(),
				))
			go func() {
				<-done
				grpcServer.Stop()
			}()
			h := handler.New(opt.host)
			defer h.Close()
			api.RegisterControlServiceServer(grpcServer, h)
			err = grpcServer.Serve(ctlLtn)
			if err != nil {
				panic(err)
			}

		},
	}
	cmd.Flags().StringVar(&opt.ctlAddr, "ctl_addr", ":2222", "server address")
	cmd.Flags().StringVar(&opt.host, "host", "127.0.0.1", "public host ip address or hostname")
	return cmd
}

func newForwarderCmd() *cobra.Command {
	var pubPort int32
	var svrAddr string
	cmd := &cobra.Command{
		Use:   "forwarder",
		Short: "create a forwarder service on server side",
		Run: func(cmd *cobra.Command, args []string) {
			c, err := grpc.Dial(svrAddr, grpc.WithInsecure())
			if err != nil {
				log.Fatalf("failed to dial to grpc server: %v", err)
			}
			client := api.NewControlServiceClient(c)
			resp, err := client.StartInternalService(context.TODO(), &api.StartInternalServiceRequest{
				ServiceName: "l7forwarder",
				PubPort:     pubPort,
			})
			if err != nil {
				log.Fatalf("failed to start forwarder service: %v", err)
			}
			fmt.Printf("new forwarder %s(%s) created\n", resp.Name, resp.Addr)
		},
	}
	cmd.Flags().StringVar(&svrAddr, "svr_addr", "127.0.0.1:2222", "server address.")
	cmd.Flags().Int32Var(&pubPort, "pub_port", 0, "public port for the forwarder")
	return cmd
}

func newCmd() *cobra.Command {
	client := cmd.NewClientCmd()
	svr := newServerCmd()
	forward := newForwarderCmd()
	svr.AddCommand(forward)
	lan := newLANCmd()
	cmd := &cobra.Command{
		Use:  "l4proxy",
		Long: "reverse proxy",
	}
	cmd.AddCommand(client, svr, lan)
	return cmd
}

func proxy(localAddr, remoteAddr string) {
	l, err := net.Listen("tcp", localAddr)
	if err != nil {
		panic(err)
	}
	go func() {
		<-done
		l.Close()
	}()
	log.Printf("listen on %s", l.Addr())
	var wg sync.WaitGroup
	for {
		cc, err := l.Accept()
		if err != nil && strings.Contains(err.Error(), "use of closed network connection") {
			break
		} else if err != nil {
			panic(err)
		}
		c, err := net.Dial("tcp", remoteAddr)
		if err != nil {
			panic(err)
		}
		log.Printf("dialed to %s", c.RemoteAddr())
		if err != nil {
			panic(err)
		}
		log.Printf("connected from %s", cc.RemoteAddr())
		p := handler.NewPairedConn(cc, c)
		p.Copy()
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-done
			p.Close()
		}()
	}
	wg.Wait()
}

func main() {
	cmd := newCmd()
	if err := cmd.Execute(); err != nil {
		panic(err)
	}
}
