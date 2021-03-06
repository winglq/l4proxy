package handler

import (
	"net"
	"sync"
	"sync/atomic"
)

// SharedListener uses reference count to ensure
// listening on the same address will success.
// SO_REUSEPORT ?
type SharedListener struct {
	refCount int64
	l        net.Listener
	onClose  func()
}

func (sl *SharedListener) Accept() (net.Conn, error) {
	return sl.l.Accept()
}

func (sl *SharedListener) Close() error {
	refCount := sl.decRef()
	if refCount == 0 {
		sl.onClose()
		return sl.l.Close()
	}
	return nil
}

func (sl *SharedListener) Addr() net.Addr {
	return sl.l.Addr()
}

func (sl *SharedListener) incRef() int64 {
	return atomic.AddInt64(&sl.refCount, 1)
}

func (sl *SharedListener) decRef() int64 {
	return atomic.AddInt64(&sl.refCount, -1)
}

var ls = sync.Map{}

func Listen(network, address string, onClose func()) (net.Listener, error) {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if port == "0" {
		return net.Listen(network, address)
	}
	k := key(network, address)
	il, ok := ls.Load(k)
	var l *SharedListener
	if ok {
		l = il.(*SharedListener)
		l.incRef()
		return l, nil
	}
	ltner, err := net.Listen(network, address)
	if err != nil {
		return ltner, err
	}
	sl := &SharedListener{
		l:        ltner,
		refCount: 1,
		onClose: func() {
			onClose()
			ls.Delete(k)
		},
	}
	ls.Store(k, sl)
	return sl, nil
}
