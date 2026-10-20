package main

import (
	"errors"
	"os"
	"sync"
	"sync/atomic"

	"golang.zx2c4.com/wireguard/tun"
)

// usTun is a userspace tun.Device backed by two bounded packet queues instead
// of a kernel interface:
//
//	in  -> packets the attached WireGuard device reads (injected by the router)
//	out <- packets the WireGuard device writes (decrypted from a peer)
//
// The relay wires two WireGuard devices together by pumping packets from one
// side's `out` queue, through the router, into the other side's `in` queue.
// This gives true userspace IP forwarding while the WireGuard devices run
// their full, unmodified crypto + cryptokey-routing machinery.
//
// Queue bounds are advisory: on overflow packets are dropped (counted), never
// blocking the device routines — WireGuard retransmits at the transport layer
// (TCP-in-tunnel) or via ICMP.
type usTun struct {
	name string
	mtu  int

	in      chan []byte
	out     chan []byte
	dead    chan struct{}
	closeOnce sync.Once
	events  chan tun.Event

	inDropped  atomic.Uint64
	outDropped atomic.Uint64
}

const (
	usTunQueueLen = 1024
	usTunMaxPkt   = 65535
)

func newUSTun(name string, mtu int) *usTun {
	return &usTun{
		name:   name,
		mtu:    mtu,
		in:     make(chan []byte, usTunQueueLen),
		out:    make(chan []byte, usTunQueueLen),
		dead:   make(chan struct{}),
		events: make(chan tun.Event, 4),
	}
}

// injectFromRouter is used by the router to enqueue a packet for the WireGuard
// device's Read side (this is what a kernel tun Write would do).
func (t *usTun) injectFromRouter(pkt []byte) {
	cp := make([]byte, len(pkt))
	copy(cp, pkt)
	select {
	case t.in <- cp:
	default:
		t.inDropped.Add(1)
	}
}

// consumeFromDevice pops a packet that the WireGuard device wrote (this is
// what reading a kernel tun would do). Blocks until a packet is available,
// the tun is closed, or stop is closed.
func (t *usTun) consumeFromDevice(stop <-chan struct{}) ([]byte, bool) {
	select {
	case pkt := <-t.out:
		return pkt, true
	case <-t.dead:
		return nil, false
	case <-stop:
		return nil, false
	}
}

// Read implements tun.Device: hands queued packets to the WireGuard device at
// the requested offset.
func (t *usTun) Read(bufs [][]byte, sizes []int, offset int) (n int, err error) {
	var pkt []byte
	select {
	case pkt = <-t.in:
	case <-t.dead:
		return 0, os.ErrClosed
	}
	if len(bufs) == 0 || len(bufs[0]) < offset+len(pkt) {
		return 0, errors.New("aegis-usTun: read buffer too small")
	}
	copy(bufs[0][offset:], pkt)
	sizes[0] = len(pkt)
	return 1, nil
}

// Write implements tun.Device: receives packets the WireGuard device decrypted
// from a peer and queues them for the router.
func (t *usTun) Write(bufs [][]byte, offset int) (int, error) {
	written := 0
	for _, buf := range bufs {
		if len(buf) <= offset {
			continue
		}
		pkt := make([]byte, len(buf)-offset)
		copy(pkt, buf[offset:])
		select {
		case t.out <- pkt:
			written++
		default:
			t.outDropped.Add(1)
		}
	}
	return written, nil
}

func (t *usTun) MTU() (int, error)        { return t.mtu, nil }
func (t *usTun) Name() (string, error)    { return t.name, nil }
func (t *usTun) BatchSize() int           { return 1 }
func (t *usTun) Events() <-chan tun.Event { return t.events }
func (t *usTun) File() *os.File           { return nil }

func (t *usTun) Close() error {
	t.closeOnce.Do(func() {
		close(t.dead)
		close(t.events)
	})
	return nil
}
