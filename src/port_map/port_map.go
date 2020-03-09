package port_map

import log "github.com/sirupsen/logrus"

type PortMapper interface {
	MapPort(srcHost string, srcPort int32, protocol string, destPort int32) error
	UnmapPort(protocol string, destPort int32) error
}

type DummyPortMapper struct{}

func NewDummyPortMapper() PortMapper {
	return &DummyPortMapper{}
}

func (d *DummyPortMapper) MapPort(srcHost string, srcPort int32, protocol string, destPort int32) error {
	return nil
}

func (d *DummyPortMapper) UnmapPort(protocol string, destPort int32) error {
	log.Debugf("ummap port")
	return nil
}
