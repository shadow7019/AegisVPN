// Package obfs implements the AegisVPN UDP obfuscation layer.
//
// It wraps any conn.Bind (typically conn.DefaultBind) so that every WireGuard
// UDP datagram sent through it is XOR-masked on the wire, and every datagram
// received is unmasked before the WireGuard device sees it. This defeats
// deep-packet-inspection classifiers that fingerprint raw WireGuard
// handshake/message types (WireGuard message types live in the first 4 bytes,
// so masking the first 16 bytes — as practised by common wg obfuscation
// patches like udp-obfuscate — is sufficient).
//
// Keystream derivation: ks = BLAKE2s-256("aegisvpn-obfs-v1|" + key); the
// first 16 bytes of ks are XORed cyclically over the 16-byte datagram prefix.
// The server side applies the identical transform (XOR is an involution).
package obfs

import (
	"sync"

	"golang.org/x/crypto/blake2s"
	"golang.zx2c4.com/wireguard/conn"
)

// maskedLen is the number of leading bytes of each datagram that are masked.
const maskedLen = 16

// keystream derives the 32-byte mask keystream for a passphrase.
func keystream(key string) []byte {
	sum := blake2s.Sum256([]byte("aegisvpn-obfs-v1|" + key))
	return sum[:]
}

type obfsBind struct {
	inner conn.Bind
	mask  [maskedLen]byte

	mu   sync.Mutex
	fns  []conn.ReceiveFunc
}

// NewBind wraps inner with XOR obfuscation. Every outbound datagram's first
// maskedLen bytes are XORed with the keystream before being handed to the
// inner bind; every inbound datagram is unmasked in the receive path before
// the WireGuard device processes it.
func NewBind(key string, inner conn.Bind) conn.Bind {
	b := &obfsBind{inner: inner}
	ks := keystream(key)
	copy(b.mask[:], ks)
	return b
}

// Open delegates to the inner bind and wraps each of its receive functions
// with the unmasking step.
func (b *obfsBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	fns, actualPort, err := b.inner.Open(port)
	if err != nil {
		return nil, 0, err
	}
	b.mu.Lock()
	wrapped := make([]conn.ReceiveFunc, 0, len(fns))
	for _, fn := range fns {
		f := fn
		wrapped = append(wrapped, b.receive(f))
	}
	b.fns = wrapped
	b.mu.Unlock()
	return wrapped, actualPort, nil
}

// receive returns a conn.ReceiveFunc that unmasks packets before delivery.
func (b *obfsBind) receive(fn conn.ReceiveFunc) conn.ReceiveFunc {
	return func(packets [][]byte, sizes []int, eps []conn.Endpoint) (n int, err error) {
		n, err = fn(packets, sizes, eps)
		if err != nil {
			return n, err
		}
		for i := 0; i < n; i++ {
			applyMask(packets[i], b.mask)
		}
		return n, nil
	}
}

// applyMask XORs the first maskedLen bytes of buf with the keystream.
// XOR is an involution, so the same function masks and unmasks.
func applyMask(buf []byte, mask [maskedLen]byte) {
	if len(buf) < maskedLen {
		return
	}
	for i := 0; i < maskedLen; i++ {
		buf[i] ^= mask[i]
	}
}

// Send masks each packet in place and forwards to the inner bind. The
// WireGuard device recycles its buffers after Send returns, so mutating them
// is safe.
func (b *obfsBind) Send(bufs [][]byte, ep conn.Endpoint) error {
	for _, buf := range bufs {
		applyMask(buf, b.mask)
	}
	return b.inner.Send(bufs, ep)
}

// ParseEndpoint delegates to the inner bind (obfuscation never changes
// addressing, only payload bytes).
func (b *obfsBind) ParseEndpoint(s string) (conn.Endpoint, error) {
	return b.inner.ParseEndpoint(s)
}

// SetMark delegates to the inner bind.
func (b *obfsBind) SetMark(mark uint32) error {
	return b.inner.SetMark(mark)
}

// BatchSize delegates to the inner bind.
func (b *obfsBind) BatchSize() int {
	return b.inner.BatchSize()
}

// Close delegates to the inner bind.
func (b *obfsBind) Close() error {
	return b.inner.Close()
}
