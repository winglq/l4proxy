package handler

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

type PairedConn struct {
	SRC, DEST net.Conn
	SpeedIn   float64
	SpeedOut  float64
	OnClose   func()
	chIn      chan int
	chOut     chan int
	wg        sync.WaitGroup
	done      chan struct{}
	once      sync.Once
}

type Token int

var pool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 4096)
		return buf
	},
}

// copyBuffer is copied from golang's std lib.
func copyBuffer(src, dst net.Conn, buf []byte, speedCH chan<- int) (written int64, err error) {
	if buf != nil && len(buf) == 0 {
		panic("empty buffer in io.CopyBuffer")
	}
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
		speedCH <- nr
	}
	return written, err
}

func NewPairedConn(src, dst net.Conn) *PairedConn {
	p := &PairedConn{
		SRC:   src,
		DEST:  dst,
		chIn:  make(chan int),
		chOut: make(chan int),
		done:  make(chan struct{}),
	}
	p.wg.Add(1)
	go p.statistic()
	return p
}

func (pc *PairedConn) statistic() {
	defer pc.wg.Done()
	seconds := 5.0
	t := time.NewTicker(time.Duration(seconds) * time.Second)
	type SpeedPair struct {
		speed float64
		t     time.Time
	}
	speedsIn := []SpeedPair{}
	speedsOut := []SpeedPair{}
	for {
		select {
		case nr, ok := <-pc.chIn:
			if !ok {
				return
			}
			speedsIn = append(speedsIn, SpeedPair{
				speed: float64(nr),
				t:     time.Now(),
			})
		case nr, ok := <-pc.chOut:
			if !ok {
				return
			}
			speedsOut = append(speedsOut, SpeedPair{
				speed: float64(nr),
				t:     time.Now(),
			})

		case <-t.C:
			fn := func(speeds []SpeedPair) float64 {
				total := 0.0
				for _, sp := range speeds {
					total += sp.speed
				}
				return total / seconds

			}
			pc.SpeedIn = fn(speedsIn)
			pc.SpeedOut = fn(speedsOut)
			speedsIn = []SpeedPair{}
			speedsOut = []SpeedPair{}
		case <-pc.done:
			return
		}
	}
}

func (pc *PairedConn) copy(src, dst net.Conn, ch chan<- int) (int64, error) {
	buf := pool.Get().([]byte)
	defer pool.Put(buf)

	return copyBuffer(src, dst, buf, ch)
}

func (pc *PairedConn) Copy() {
	gracefulClosed := false
	pc.wg.Add(1)
	go func() {
		defer pc.wg.Done()
		_, err := pc.copy(pc.SRC, pc.DEST, pc.chIn)
		if err != nil && !gracefulClosed {
			log.Printf("copy %s -> %s failed: %v", pc.SRC.RemoteAddr(), pc.DEST.LocalAddr(), err)
		} else {
			gracefulClosed = true
		}
		pc.close()
	}()
	pc.wg.Add(1)
	go func() {
		defer pc.wg.Done()
		_, err := pc.copy(pc.DEST, pc.SRC, pc.chOut)
		if err != nil && !gracefulClosed {
			log.Printf("copy %s -> %s failed: %v", pc.DEST.LocalAddr(), pc.SRC.RemoteAddr(), err)
		} else {
			gracefulClosed = true
		}
		pc.close()
	}()
	pc.wg.Add(1)
	go func() {
		defer pc.wg.Done()
		<-pc.done
		if pc.OnClose != nil {
			pc.OnClose()
		}
		pc.SRC.Close()
		pc.DEST.Close()
	}()
}

func (pc *PairedConn) String() string {
	return fmt.Sprintf("SRC: %p DEST: %p", pc.SRC, pc.DEST)
}

func (pc *PairedConn) Close() {
	pc.close()
	pc.wg.Wait()
	log.Debug("pair closed")
}

func (pc *PairedConn) close() {
	pc.once.Do(func() {
		close(pc.done)
	})
}

func (t Token) String() string {
	return fmt.Sprintf("%04d", int(t))
}

func (t Token) Len() int {
	return 4
}
