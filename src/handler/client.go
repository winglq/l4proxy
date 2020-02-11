package handler

import (
	"net"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
)

type Client struct {
	name               string
	displayName        string
	sharePub           bool
	pubPort            string
	intPort            string
	host               string
	connPairs          Pairs
	intConnCH          chan net.Conn
	pubConnCH          chan net.Conn
	done               chan struct{}
	NewPubConnNotifyCH chan Token
	wg                 sync.WaitGroup
}

func NewClient(name, displayName, host, pubPort, intPort string, sharePub bool) (*Client, error) {
	c := &Client{
		name:               name,
		displayName:        displayName,
		connPairs:          Pairs{},
		pubPort:            pubPort,
		intPort:            intPort,
		host:               host,
		done:               make(chan struct{}),
		NewPubConnNotifyCH: make(chan Token),
		sharePub:           sharePub,
	}
	return c, c.init()
}

func (c *Client) PubAddr() string {
	return net.JoinHostPort(c.host, c.pubPort)
}

func (c *Client) IntAddr() string {
	return net.JoinHostPort(c.host, c.intPort)
}

var cs = map[string]chan net.Conn{}
var mu sync.Mutex

func key(network, address string) string {
	return strings.Join([]string{network, address}, "_")
}

func (c *Client) listenAndAccept(addr string, share bool) (string, chan net.Conn, error) {
	mu.Lock()
	defer mu.Unlock()
	var ltn net.Listener
	var err error
	if share {
		ch, ok := cs[key("tcp", addr)]
		if ok {
			return addr, ch, nil
		}
		ltn, err = Listen("tcp", addr, func() {
			mu.Lock()
			defer mu.Unlock()
			delete(cs, key("tcp", addr))
		})

	} else {
		ltn, err = net.Listen("tcp", addr)
	}
	if err != nil {
		return "", nil, err
	}

	go func() {
		select {
		case <-c.done:
		}
		ltn.Close()
	}()
	ch := make(chan net.Conn)
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
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
	if share {
		cs[key("tcp", addr)] = ch
	}
	return port, ch, nil
}

func (c *Client) init() (err error) {
	c.intPort, c.intConnCH, err = c.listenAndAccept(c.IntAddr(), false)
	if err != nil {
		return
	}
	c.pubPort, c.pubConnCH, err = c.listenAndAccept(c.PubAddr(), c.sharePub)
	if err != nil {
		return
	}
	return
}

func (c *Client) Start() {
	c.wg.Add(1)
	go func() {
		c.run()
	}()
}

func (c *Client) run() {
	var token Token
	for {
		select {
		case <-c.done:
			for _, p := range c.connPairs {
				p.Close()
			}
			return
		case conn := <-c.pubConnCH:
			if conn == nil {
				c.Close()
				break
			}
			token = token + 1
			c.connPairs[token.String()] = &PairedConn{
				SRC: conn,
			}
			c.NewPubConnNotifyCH <- token
			log.Debugf("new backend service user from %s", conn.RemoteAddr())
		case conn := <-c.intConnCH:
			if conn == nil {
				c.Close()
				break
			}
			buf := make([]byte, token.Len())
			_, err := conn.Read(buf)
			if err != nil {
				panic(err)
			}
			pair := c.connPairs[string(buf)]
			pair.DEST = conn
			pair.OnClose = func() {
				delete(c.connPairs, string(buf))
				log.Debugf("backend service user %s disconnected", pair.SRC.RemoteAddr())
			}
			pair.Copy(&c.wg)

		}
	}

}

func (c *Client) Close() {
	close(c.done)
	c.wg.Wait()
	log.Infof("client %s closed.", c.name)
}
