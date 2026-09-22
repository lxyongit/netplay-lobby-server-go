// Package traversal exchanges TCP endpoints for simultaneous open. It never relays game data.
package traversal

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

const addressBytes = 19

type address struct {
	ip   net.IP
	port uint16
}

type peer struct {
	role       byte
	room       string
	observed   address
	candidates []address
	matched    chan match
}

type match struct {
	other *peer
	nonce [16]byte
}

type server struct {
	mu      sync.Mutex
	waiting map[string]*peer
	perIP   map[string]int
	slots   chan struct{}
}

// Serve owns accepted connections. Each registration and pairing has a deadline;
// per-IP and global limits also bound idle connections and waiting goroutines.
func Serve(listener net.Listener) error {
	s := &server{waiting: make(map[string]*peer), perIP: make(map[string]int), slots: make(chan struct{}, 512)}
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		remote, ok := conn.RemoteAddr().(*net.TCPAddr)
		if !ok {
			conn.Close()
			continue
		}
		key := remote.IP.String()
		s.mu.Lock()
		allowed := s.perIP[key] < 32
		if allowed {
			select {
			case s.slots <- struct{}{}:
				s.perIP[key]++
			default:
				allowed = false
			}
		}
		s.mu.Unlock()
		if !allowed {
			conn.Close()
			continue
		}
		go func() {
			defer conn.Close()
			defer func() {
				s.mu.Lock()
				s.perIP[key]--
				if s.perIP[key] == 0 {
					delete(s.perIP, key)
				}
				s.mu.Unlock()
				<-s.slots
			}()
			s.handle(conn, remote)
		}()
	}
}

func (s *server) handle(conn net.Conn, remote *net.TCPAddr) {
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	p, err := readPeer(conn)
	if err != nil {
		return
	}
	p.observed = address{ip: remote.IP, port: uint16(remote.Port)}
	p.matched = make(chan match, 1)
	defer func() {
		s.mu.Lock()
		if s.waiting[p.room] == p {
			delete(s.waiting, p.room)
		}
		s.mu.Unlock()
	}()
	wait := 60 * time.Second
	if p.role == 2 {
		wait = 4 * time.Second
	}
	conn.SetDeadline(time.Now().Add(wait + 6 * time.Second))
	// EOF promptly removes hosts that leave a room, instead of retaining stale slots.
	disconnected := make(chan struct{})
	go func() {
		var b [1]byte
		conn.Read(b[:])
		close(disconnected)
	}()
	s.mu.Lock()
	other := s.waiting[p.room]
	if other == nil {
		s.waiting[p.room] = p
	} else if other.role != p.role {
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			s.mu.Unlock()
			return
		}
		delete(s.waiting, p.room)
		other.matched <- match{other: p, nonce: nonce}
		p.matched <- match{other: other, nonce: nonce}
	} else {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case paired := <-p.matched:
		candidates := candidatesFor(paired.other, p)
		body := append([]byte("PAIR"), paired.nonce[:]...)
		body = append(body, byte(len(candidates)))
		for _, a := range candidates {
			body = appendAddress(body, a)
		}
		packet := make([]byte, 2, 2+len(body))
		binary.BigEndian.PutUint16(packet, uint16(len(body)))
		packet = append(packet, body...)
		conn.SetWriteDeadline(time.Now().Add(time.Second))
		if _, err := io.Copy(conn, bytes.NewReader(packet)); err != nil {
			return
		}
		log.Printf("NETPLAY-DEBUG traversal paired role=%d candidates=%d", p.role, len(candidates))
		// Preserve the observed NAT mapping until the phones select a direct socket.
		conn.SetReadDeadline(time.Now().Add(6 * time.Second))
		<-disconnected
	case <-disconnected:
	case <-timer.C:
	}
}

// Small bounded protocol: u16 length, LNP1, role, room length, room,
// candidate count, then family/u16 port/16-byte address records.
func readPeer(r io.Reader) (*peer, error) {
	var prefix [2]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint16(prefix[:]))
	if size < 8 || size > 7+64+7*addressBytes {
		return nil, fmt.Errorf("invalid size")
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	roomSize := int(body[5])
	if string(body[:4]) != "LNP1" || (body[4] != 1 && body[4] != 2) || roomSize < 1 || roomSize > 64 || 7+roomSize > size {
		return nil, fmt.Errorf("invalid registration")
	}
	for _, c := range body[6:6+roomSize] {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' || c == '-' || c == '_') {
			return nil, fmt.Errorf("invalid room")
		}
	}
	count := int(body[6+roomSize])
	if count > 7 || size != 7+roomSize+count*addressBytes {
		return nil, fmt.Errorf("invalid candidates")
	}
	p := &peer{role: body[4], room: string(body[6 : 6+roomSize])}
	for i := 7 + roomSize; i < size; i += addressBytes {
		a := address{port: binary.BigEndian.Uint16(body[i+1 : i+3])}
		if body[i] == 4 {
			a.ip = net.IP(body[i+3 : i+7])
		} else if body[i] == 6 {
			a.ip = net.IP(body[i+3 : i+19])
		}
		if usable(a) {
			p.candidates = append(p.candidates, a)
		}
	}
	return p, nil
}

func usable(a address) bool {
	if a.port == 0 || a.ip == nil {
		return false
	}
	if v4 := a.ip.To4(); v4 != nil {
		return v4[0] != 0 && v4[0] != 127 && v4[0] < 224 && !(v4[0] == 169 && v4[1] == 254) && !a.ip.Equal(net.IPv4bcast)
	}
	return len(a.ip) == 16 && a.ip[0]&0xe0 == 0x20
}

func privateIPv4(ip net.IP) bool {
	v := ip.To4()
	return v != nil && (v[0] == 10 || (v[0] == 172 && v[1] >= 16 && v[1] <= 31) || (v[0] == 192 && v[1] == 168) || (v[0] == 100 && v[1] >= 64 && v[1] <= 127))
}

func candidatesFor(from, to *peer) []address {
	result := []address{}
	for _, a := range append([]address{from.observed}, from.candidates...) {
		if !usable(a) || (privateIPv4(a.ip) && !from.observed.ip.Equal(to.observed.ip)) {
			continue
		}
		duplicate := false
		for _, b := range result {
			if a.port == b.port && a.ip.Equal(b.ip) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, a)
		}
	}
	return result
}

func appendAddress(out []byte, a address) []byte {
	record := make([]byte, addressBytes)
	binary.BigEndian.PutUint16(record[1:3], a.port)
	if v4 := a.ip.To4(); v4 != nil {
		record[0] = 4
		copy(record[3:], v4)
	} else {
		record[0] = 6
		copy(record[3:], a.ip.To16())
	}
	return append(out, record...)
}
