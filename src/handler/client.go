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
	c.log().Infof("client connected")
	return c, c.init()
}
func (c *Client) log() *log.Entry {
	return log.NewEntry(log.StandardLogger()).WithField("client", c.name)
}

func (c *Client) PubAddr() string {
	return net.JoinHostPort(c.host, c.pubPort)
}

func (c *Client) IntAddr() string {
	return net.JoinHostPort(c.host, c.intPort)
}

var cs = map[net.Listener]chan net.Conn{}
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
		ltn, err = Listen("tcp", addr, func() {
			mu.Lock()
			defer mu.Unlock()
			delete(cs, ltn)
		})
		if err != nil {
			return "", nil, err
		}
		go func() {
			select {
			case <-c.done:
			}
			ltn.Close()
		}()

		ch, ok := cs[ltn]
		if ok {
			_, port, _ := net.SplitHostPort(addr)
			return port, ch, nil
		}

	} else {
		ltn, err = net.Listen("tcp", addr)
		if err != nil {
			return "", nil, err
		}
		go func() {
			select {
			case <-c.done:
			}
			ltn.Close()
		}()
	}

	ch := make(chan net.Conn)
	// this go routine will be closed when ltn(may be shared) closed.
	// may we should use SharedListener to manage this go routine.
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
	if share {
		cs[ltn] = ch
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
	defer c.wg.Done()
	for {
		select {
		case <-c.done:
			return
		case conn := <-c.pubConnCH:
			if conn == nil {
				return
			}
			token = token + 1
			c.connPairs[token.String()] = &PairedConn{
				SRC: conn,
			}
			c.NewPubConnNotifyCH <- token
			c.log().Debugf("new backend service user from %s", conn.RemoteAddr())
		case conn := <-c.intConnCH:
			if conn == nil {
				return
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
				c.log().Debugf("backend service user %s disconnected", pair.SRC.RemoteAddr())
			}
			pair.Copy(&c.wg)

		}
	}

}

func (c *Client) Close() {
	for _, p := range c.connPairs {
		p.Close()
	}
	close(c.done)
	c.wg.Wait()
	c.log().Infof("client closed.")
}
