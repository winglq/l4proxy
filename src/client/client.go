package client

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/winglq/l4proxy/src/api"
	"github.com/winglq/l4proxy/src/handler"
	"github.com/winglq/l4proxy/src/port_map"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

type Options struct {
	SvrAddr     string
	PubPort     int32
	IntPort     int32
	Name        string
	SharePub    bool
	BackendPort int32
}

func createClient(client api.ControlServiceClient, opt *Options, backendPort int32) (api.ControlService_CreateClientClient, error) {
	for {
		c, err := client.CreateClient(context.TODO(), &api.CreateClientRequest{
			DisplayName:     opt.Name,
			PublicPort:      opt.PubPort,
			InternalPort:    opt.IntPort,
			SharePublicAddr: opt.SharePub,
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

func CreateRunFunc(done chan struct{}, opt *Options, onNewConn func(resp *api.Client, host, port string) *handler.PairedConn) func(cmd *cobra.Command, args []string) {
	ret := func(cmd *cobra.Command, args []string) {
		port := "22"
		host := "127.0.0.1"
		if len(args) > 0 {
			host = args[0]
		}
		if len(args) > 1 {
			port = args[1]
		}
		c, err := grpc.Dial(opt.SvrAddr, grpc.WithInsecure())
		if err != nil {
			panic(err)
		}
		go func() {
			<-done
			c.Close()
		}()
		client := api.NewControlServiceClient(c)
		backendPort := opt.BackendPort
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
		conClient, err := createClient(client, opt, backendPort)
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
					logrus.Errorf("recv messsage failed: %v", err)
					conClient, err = createClient(client, opt, backendPort)
					if err != nil && grpc.Code(err) == codes.Canceled {
						mapper.UnmapPort("tcp", backendPort)
						return
					}
					continue
				}
			}
			if resp.InternalAddress != "" {
				pair := onNewConn(resp, host, port)
				if pair != nil {
					defer pair.Close()
				}
			} else {
				fmt.Printf("PUBLIC ADDRESS: %s\n", resp.PublicAddress)
				_, port, err := net.SplitHostPort(resp.PublicAddress)
				if err != nil {
					panic(err)
				}
				p, _ := strconv.ParseInt(port, 10, 64)
				opt.PubPort = int32(p)
			}
		}

	}
	return ret
}
