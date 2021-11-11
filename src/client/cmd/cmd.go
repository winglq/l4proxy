package cmd

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/inhies/go-bytesize"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"github.com/winglq/l4proxy/src/api"
	"github.com/winglq/l4proxy/src/client"
	"github.com/winglq/l4proxy/src/client/forwarder"
	"github.com/winglq/l4proxy/src/handler"
	"google.golang.org/grpc"
)

var opt client.Options
var done = make(chan struct{})

func NewClientCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:  "client [host] [port]",
		Args: cobra.MaximumNArgs(2),
		Run:  client.CreateRunFunc(done, &opt, dialToBackend),
	}
	cmd.PersistentFlags().StringVar(&opt.SvrAddr, "svr_addr", "127.0.0.1:2222", "server address.")
	cmd.Flags().Int32Var(&opt.PubPort, "pub_port", 0, "public port for this client.")
	cmd.Flags().Int32Var(&opt.IntPort, "int_port", 0, "internal port used to listen client connection.")
	cmd.Flags().StringVar(&opt.Name, "client_name", "unknown", "client name")
	cmd.Flags().BoolVar(&opt.SharePub, "share_public_port", false, "share public port for different clients")
	cmd.Flags().Int32Var(&opt.BackendPort, "backend_port", 0, "stun port used to be connected by service users")
	list := newListClientsCmd()
	fwd := forwarder.NewForwarderBackendCmd(&opt)
	cmd.AddCommand(list, fwd)
	return &cmd
}

func Close() {
	close(done)
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
			c, err := grpc.Dial(opt.SvrAddr, grpc.WithInsecure())
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

func dialToBackend(resp *api.Client, host, port string) (*handler.PairedConn, error) {
	c, err := net.Dial("tcp", resp.InternalAddress)
	if err != nil {
		return nil, err
	}
	_, err = c.Write([]byte(resp.Token))
	if err != nil {
		return nil, err
	}
	sconn, err := net.Dial("tcp", fmt.Sprintf("%s:%s", host, port))
	if err != nil {
		//TODO: return error code, and close c
		c.Close()
		return nil, err
	}
	log.Printf("connected to backend service: %s -> %s", sconn.LocalAddr(), sconn.RemoteAddr())
	pair := handler.NewPairedConn(c, sconn)
	pair.Copy()
	return pair, nil
}

func newListClientsCmd() *cobra.Command {
	cmd := cobra.Command{
		Use: "list",
		Run: func(cmd *cobra.Command, args []string) {
			c, err := grpc.Dial(opt.SvrAddr, grpc.WithInsecure())
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
