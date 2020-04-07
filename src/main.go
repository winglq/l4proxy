package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_logrus "github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus"
	grpc_validator "github.com/grpc-ecosystem/go-grpc-middleware/validator"
	"github.com/inhies/go-bytesize"
	"github.com/olekukonko/tablewriter"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/winglq/l4proxy/src/api"
	"github.com/winglq/l4proxy/src/handler"
	"github.com/winglq/l4proxy/src/port_map"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

type ClientOptions struct {
	svrAddr     string
	pubPort     int32
	intPort     int32
	name        string
	sharePub    bool
	backendPort int32
}

type ServerOptions struct {
	ctlAddr string
	host    string
}

type Options struct {
	s ServerOptions
	c ClientOptions
}

var opt Options
var cSig = make(chan os.Signal)

func init() {
	signal.Notify(cSig, os.Interrupt)
	log.SetLevel(log.DebugLevel)
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
			ctlLtn, err := net.Listen("tcp", opt.s.ctlAddr)
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
				<-cSig
				grpcServer.Stop()
			}()
			h := handler.New(opt.s.host)
			defer h.Close()
			api.RegisterControlServiceServer(grpcServer, h)
			err = grpcServer.Serve(ctlLtn)
			if err != nil {
				panic(err)
			}

		},
	}
	cmd.Flags().StringVar(&opt.s.ctlAddr, "ctl_addr", ":2222", "server address")
	cmd.Flags().StringVar(&opt.s.host, "host", "127.0.0.1", "public host ip address or hostname")
	return cmd
}

func newClientCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:  "client [host] [port]",
		Args: cobra.MaximumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			port := "22"
			host := "127.0.0.1"
			if len(args) > 0 {
				host = args[0]
			}
			if len(args) > 1 {
				port = args[1]
			}
			c, err := grpc.Dial(opt.c.svrAddr, grpc.WithInsecure())
			if err != nil {
				panic(err)
			}
			go func() {
				<-cSig
				c.Close()
			}()
			client := api.NewControlServiceClient(c)
			backendPort := opt.c.backendPort
			var pt int64
			if backendPort == 0 {
				pt, err = strconv.ParseInt(port, 10, 32)
				if err != nil {
					log.Fatalf("backend port format error")
				}
				backendPort = int32(pt)
			}
			mapper := port_map.NewDummyPortMapper()
			mapper.MapPort("", int32(pt), "tcp", backendPort)
			createClientFunc := func() (api.ControlService_CreateClientClient, error) {
				for {
					c, err := client.CreateClient(context.TODO(), &api.CreateClientRequest{
						DisplayName:     opt.c.name,
						PublicPort:      opt.c.pubPort,
						InternalPort:    opt.c.intPort,
						SharePublicAddr: opt.c.sharePub,
						Protocol:        "tcp",
						BackendPort:     backendPort,
					})
					if err == nil {
						return c, nil
					}
					if grpc.Code(err) == codes.Unavailable {
						log.Printf("reconnecting after 5 second due to err: %v", err)
						time.Sleep(5 * time.Second)
						continue
					} else if grpc.Code(err) == codes.Canceled {
						return nil, err
					} else if err != nil {
						panic(err)
					}
				}

			}
			conClient, err := createClientFunc()
			if err != nil && grpc.Code(err) == codes.Canceled {
				return
			} else if err != nil {
				panic(err)
			}
			for {
				resp, err := conClient.Recv()
				if err != nil {
					if grpc.Code(err) == codes.Canceled {
						break
					} else {
						log.Errorf("recv messsage failed: %v", err)
						conClient, err = createClientFunc()
						if err != nil && grpc.Code(err) == codes.Canceled {
							mapper.UnmapPort("tcp", backendPort)
							return
						}
						continue
					}
				}
				if resp.InternalAddress != "" {
					c, err := net.Dial("tcp", resp.InternalAddress)
					if err != nil {
						panic(err)
					}
					_, err = c.Write([]byte(resp.Token))
					if err != nil {
						panic(err)
					}
					sconn, err := net.Dial("tcp", fmt.Sprintf("%s:%s", host, port))
					if err != nil {
						panic(err)
					}
					log.Printf("connected to backend service: %s -> %s", sconn.LocalAddr(), sconn.RemoteAddr())
					pair := handler.NewPairedConn(c, sconn)
					pair.Copy()
					defer pair.Close()
				} else {
					fmt.Printf("PUBLIC ADDRESS: %s\n", resp.PublicAddress)
				}
			}
		},
	}
	cmd.PersistentFlags().StringVar(&opt.c.svrAddr, "svr_addr", "127.0.0.1:2222", "server address.")
	cmd.Flags().Int32Var(&opt.c.pubPort, "pub_port", 0, "public port for this client.")
	cmd.Flags().Int32Var(&opt.c.intPort, "int_port", 0, "internal port used to listen client connection.")
	cmd.Flags().StringVar(&opt.c.name, "client_name", "unknown", "client name")
	cmd.Flags().BoolVar(&opt.c.sharePub, "share_public_port", false, "share public port for different clients")
	cmd.Flags().Int32Var(&opt.c.backendPort, "backend_port", 0, "stun port used to be connected by service users")
	list := newListClientsCmd()
	cmd.AddCommand(list)
	cmd.AddCommand(newForwarderCmd())
	return &cmd
}

func newListClientsCmd() *cobra.Command {
	cmd := cobra.Command{
		Use: "list",
		Run: func(cmd *cobra.Command, args []string) {
			c, err := grpc.Dial(opt.c.svrAddr, grpc.WithInsecure())
			if err != nil {
				panic(err)
			}
			client := api.NewControlServiceClient(c)
			resp, err := client.ListClients(context.TODO(), &api.ListClientsRequest{})
			if err != nil {
				fmt.Printf("list clients failed: %v", err)
				os.Exit(1)
			}
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Name", "Display Name", "Public Address", "Internal Address"})
			for _, cl := range resp.Clients {
				table.Append([]string{cl.Name, cl.DisplayName, cl.PublicAddress, cl.InternalAddress})

			}
			table.Render()
		},
	}
	list := newListClientUsersCmd()
	cmd.AddCommand(list)
	return &cmd
}

func newListClientUsersCmd() *cobra.Command {
	parent := ""
	cmd := cobra.Command{
		Use: "user",
		Run: func(cmd *cobra.Command, args []string) {
			if parent == "" {
				fmt.Println("client_name is reqired")
				return
			}
			c, err := grpc.Dial(opt.c.svrAddr, grpc.WithInsecure())
			if err != nil {
				log.Fatalf("failed to dial to grpc server: %v", err)
			}
			client := api.NewControlServiceClient(c)
			resp, err := client.ListBackendServiceUsers(context.TODO(), &api.ListBackendServiceUsersRequest{
				Parent: parent,
			})
			if err != nil {
				log.Fatalf("list clients users failed: %v", err)
			}
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"User Address", "Speed In", "Speed Out"})
			for _, u := range resp.Users {
				table.Append([]string{u.UserAddr, bytesize.New(u.SpeedIn).String(), bytesize.New(u.SpeedOut).String()})

			}
			table.Render()
		},
	}
	cmd.Flags().StringVar(&parent, "client_name", "", "client unique name.")
	return &cmd
}

func newForwarderCmd() *cobra.Command {
	var pubPort int32
	cmd := &cobra.Command{
		Use: "forwarder",
		Run: func(cmd *cobra.Command, args []string) {
			c, err := grpc.Dial(opt.c.svrAddr, grpc.WithInsecure())
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
	cmd.Flags().Int32Var(&pubPort, "pub_port", 0, "public port for the forwarder")
	return cmd
}

func newCmd() *cobra.Command {
	client := newClientCmd()
	svr := newServerCmd()
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
		<-cSig
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
			<-cSig
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
