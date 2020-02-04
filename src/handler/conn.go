package handler

import (
	"fmt"
	"io"
	"log"
	"net"
)

type PairedConn struct {
	SRC, DEST net.Conn
	OnClose   func()
}

func (pc *PairedConn) Copy() {
	closeCH := make([]chan struct{}, 2)
	closeCH[0] = make(chan struct{})
	closeCH[1] = make(chan struct{})
	graceClose := make([]bool, 2)
	go func() {
		_, err := io.Copy(pc.SRC, pc.DEST)
		if err != nil && !graceClose[1] {
			log.Printf("close %s -> %s failed: %v", pc.SRC.RemoteAddr(), pc.DEST.LocalAddr(), err)
		} else {
			graceClose[0] = true
		}
		close(closeCH[0])
	}()
	go func() {
		_, err := io.Copy(pc.DEST, pc.SRC)
		if err != nil && !graceClose[0] {
			log.Printf("close %s -> %s failed: %v", pc.DEST.LocalAddr(), pc.SRC.RemoteAddr(), err)
		} else {
			graceClose[1] = true
		}
		close(closeCH[1])
	}()
	go func() {
		select {
		case <-closeCH[0]:
		case <-closeCH[1]:
		}
		pc.Close()
		if pc.OnClose != nil {
			pc.OnClose()
		}
	}()
}

func (pc *PairedConn) String() string {
	return fmt.Sprintf("SRC: %p DEST: %p", pc.SRC, pc.DEST)
}

func (pc *PairedConn) Close() {
	pc.SRC.Close()
	pc.DEST.Close()
}

type Pairs map[string]*PairedConn

type Token int

func (t Token) String() string {
	return fmt.Sprintf("%04d", int(t))
}

func (t Token) Len() int {
	return 4
}
