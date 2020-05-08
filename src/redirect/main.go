package main

import (
	"context"
	"log"
	"net/http"

	"github.com/labstack/echo"
	"github.com/spf13/cobra"
	"github.com/winglq/l4proxy/src/api"
	"google.golang.org/grpc"
)

var l4svrAddr = "127.0.0.1:2222"
var displayName = "mstream"
var addr = ":3000"

func redirect(c echo.Context) error {
	addr := getRedirectToIP(l4svrAddr, displayName)
	if addr == "" {
		c.String(http.StatusNotFound, "not found redirect url")
		return nil
	}
	c.Redirect(http.StatusSeeOther, "http://"+addr)
	return nil
}

func main() {
	cmd := cobra.Command{
		Use: "redirect",
		Run: func(cmd *cobra.Command, args []string) {
			e := echo.New()
			e.GET("/", redirect)
			e.Logger.Fatal(e.Start(addr))

		},
	}
	cmd.Flags().StringVar(&l4svrAddr, "svr_addr", l4svrAddr, "l4proxy server address")
	cmd.Flags().StringVar(&displayName, "display", displayName, "display name of the public ip this service will direct to")
	cmd.Flags().StringVar(&addr, "addr", addr, "http server listen address")
	if err := cmd.Execute(); err != nil {
		panic(err)
	}
}

func getRedirectToIP(l4svrAddr, displayName string) string {
	c, err := grpc.Dial(l4svrAddr, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("failed to dial to grpc server: %v", err)
		return ""
	}
	client := api.NewControlServiceClient(c)
	resp, err := client.ListClients(context.TODO(), &api.ListClientsRequest{})
	if err != nil {
		log.Fatalf("get response failed: %v", err)
		return ""
	}
	for _, cl := range resp.Clients {
		if cl.DisplayName == displayName {
			return cl.PublicAddress
		}
	}
	return ""
}
