package relay

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const socksUDPMax = 65507

// ValidateSOCKS5Proxy validates a UDP-capable proxy configuration without exposing credentials.
func ValidateSOCKS5Proxy(raw string) error {
	// Source of truth: https://datatracker.ietf.org/doc/html/rfc1928#section-3
	_, err := parseSOCKSProxy(raw)
	return err
}

func parseSOCKSProxy(raw string) (*url.URL, error) {
	// Source of truth: https://datatracker.ietf.org/doc/html/rfc1929#section-2
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "socks5" && u.Scheme != "socks5h") || u.Hostname() == "" || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("SOCKS5 UDP requires a valid socks5://host:port proxy")
	}
	port, e := strconv.Atoi(u.Port())
	if e != nil || port < 1 || port > 65535 {
		return nil, errors.New("SOCKS5 UDP requires a valid proxy port")
	}
	if u.User != nil {
		user := u.User.Username()
		pass, ok := u.User.Password()
		if !ok || len(user) < 1 || len(user) > 255 || len(pass) < 1 || len(pass) > 255 {
			return nil, errors.New("SOCKS5 UDP requires valid username/password credentials")
		}
	}
	return u, nil
}

// DialSOCKS5UDP opens RFC1928 UDP ASSOCIATE and retains its TCP control connection.
func DialSOCKS5UDP(ctx context.Context, raw string) (net.PacketConn, error) {
	// Source of truth: https://datatracker.ietf.org/doc/html/rfc1928#section-7
	u, err := parseSOCKSProxy(raw)
	if err != nil {
		return nil, err
	}
	control, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, fmt.Errorf("SOCKS5 UDP connect: %w", err)
	}
	success := false
	defer func() {
		if !success {
			control.Close()
		}
	}()
	stopSetup := context.AfterFunc(ctx, func() { control.Close() })
	defer stopSetup()
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = control.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("SOCKS5 UDP deadline: %w", err)
	}
	method := byte(0)
	if u.User != nil {
		method = 2
	}
	if err = writeSOCKS(control, []byte{5, 1, method}); err != nil {
		return nil, err
	}
	var answer [2]byte
	if _, err = io.ReadFull(control, answer[:]); err != nil {
		return nil, fmt.Errorf("SOCKS5 UDP greeting: %w", err)
	}
	if answer[0] != 5 || answer[1] != method {
		return nil, errors.New("SOCKS5 UDP authentication method rejected")
	}
	if method == 2 {
		user := u.User.Username()
		pass, _ := u.User.Password()
		request := append([]byte{1, byte(len(user))}, user...)
		request = append(request, byte(len(pass)))
		request = append(request, pass...)
		err = writeSOCKS(control, request)
		clear(request)
		if err != nil {
			return nil, err
		}
		if _, err = io.ReadFull(control, answer[:]); err != nil {
			return nil, fmt.Errorf("SOCKS5 UDP authentication: %w", err)
		}
		if answer[0] != 1 || answer[1] != 0 {
			return nil, errors.New("SOCKS5 UDP authentication rejected")
		}
	}
	if err = writeSOCKS(control, []byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return nil, err
	}
	var header [4]byte
	if _, err = io.ReadFull(control, header[:]); err != nil {
		return nil, fmt.Errorf("SOCKS5 UDP association: %w", err)
	}
	if header[0] != 5 || header[2] != 0 {
		return nil, errors.New("SOCKS5 UDP invalid association response")
	}
	if header[1] != 0 {
		return nil, fmt.Errorf("SOCKS5 UDP association rejected (reply %d)", header[1])
	}
	host, err := readSOCKSHost(control, header[3])
	if err != nil {
		return nil, err
	}
	var portBytes [2]byte
	if _, err = io.ReadFull(control, portBytes[:]); err != nil {
		return nil, fmt.Errorf("SOCKS5 UDP association port: %w", err)
	}
	port := int(binary.BigEndian.Uint16(portBytes[:]))
	if port == 0 {
		return nil, errors.New("SOCKS5 UDP invalid relay port")
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsUnspecified() {
		ip = control.RemoteAddr().(*net.TCPAddr).IP
	}
	if ip == nil {
		addresses, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil || len(addresses) == 0 {
			return nil, errors.New("SOCKS5 UDP relay name resolution failed")
		}
		ip = addresses[0].IP
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	udp, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: ip, Port: port})
	if err != nil {
		return nil, fmt.Errorf("SOCKS5 UDP relay socket: %w", err)
	}
	if err = control.SetDeadline(time.Time{}); err != nil {
		udp.Close()
		return nil, fmt.Errorf("SOCKS5 UDP clear deadline: %w", err)
	}
	p := &socksUDPPacketConn{udp: udp, control: control, readBuf: make([]byte, 65535), writeBuf: make([]byte, 0, socksUDPMax)}
	p.mu.Lock()
	p.stopCancel = context.AfterFunc(ctx, func() { p.Close() })
	p.mu.Unlock()
	go func() { io.Copy(io.Discard, control); p.Close() }()
	success = true
	return p, nil
}

func writeSOCKS(c net.Conn, b []byte) error {
	// Source of truth: https://datatracker.ietf.org/doc/html/rfc1928#section-3
	for len(b) > 0 {
		n, err := c.Write(b)
		if err != nil {
			return fmt.Errorf("SOCKS5 UDP control write: %w", err)
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}

func readSOCKSHost(r io.Reader, atyp byte) (string, error) {
	// Source of truth: https://datatracker.ietf.org/doc/html/rfc1928#section-4
	n := 0
	switch atyp {
	case 1:
		n = 4
	case 4:
		n = 16
	case 3:
		var length [1]byte
		if _, err := io.ReadFull(r, length[:]); err != nil {
			return "", fmt.Errorf("SOCKS5 UDP relay name: %w", err)
		}
		n = int(length[0])
		if n == 0 {
			return "", errors.New("SOCKS5 UDP empty relay name")
		}
	default:
		return "", errors.New("SOCKS5 UDP invalid relay address type")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", fmt.Errorf("SOCKS5 UDP relay address: %w", err)
	}
	if atyp == 3 {
		return string(b), nil
	}
	return net.IP(b).String(), nil
}

type socksUDPPacketConn struct {
	udp               *net.UDPConn
	control           net.Conn
	mu                sync.Mutex
	stopCancel        func() bool
	closeOnce         sync.Once
	closeErr          error
	readMu, writeMu   sync.Mutex
	readBuf, writeBuf []byte
}

func (p *socksUDPPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	// Source of truth: https://datatracker.ietf.org/doc/html/rfc1928#section-7
	p.readMu.Lock()
	defer p.readMu.Unlock()
	for {
		n, err := p.udp.Read(p.readBuf)
		if err != nil {
			return 0, nil, err
		}
		from, payload, err := decodeSOCKSUDP(p.readBuf[:n])
		if err != nil {
			continue
		}
		return copy(b, payload), from, nil
	}
}

func decodeSOCKSUDP(b []byte) (*net.UDPAddr, []byte, error) {
	// Source of truth: https://datatracker.ietf.org/doc/html/rfc1928#section-7
	invalid := errors.New("invalid SOCKS5 UDP datagram")
	if len(b) < 4 || b[0] != 0 || b[1] != 0 || b[2] != 0 {
		return nil, nil, invalid
	}
	pos := 4
	var ip net.IP
	switch b[3] {
	case 1:
		if len(b) < pos+4+2 {
			return nil, nil, invalid
		}
		ip = append(net.IP(nil), b[pos:pos+4]...)
		pos += 4
	case 4:
		if len(b) < pos+16+2 {
			return nil, nil, invalid
		}
		ip = append(net.IP(nil), b[pos:pos+16]...)
		pos += 16
	case 3:
		if len(b) < 5 {
			return nil, nil, invalid
		}
		n := int(b[4])
		pos = 5
		if n == 0 || len(b) < pos+n+2 {
			return nil, nil, invalid
		}
		ip = net.ParseIP(string(b[pos : pos+n]))
		pos += n
		if ip == nil {
			return nil, nil, invalid
		}
	default:
		return nil, nil, invalid
	}
	port := int(binary.BigEndian.Uint16(b[pos : pos+2]))
	if port == 0 {
		return nil, nil, invalid
	}
	return &net.UDPAddr{IP: ip, Port: port}, b[pos+2:], nil
}

func (p *socksUDPPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	// Source of truth: https://datatracker.ietf.org/doc/html/rfc1928#section-7
	target, ok := addr.(*net.UDPAddr)
	if !ok || target == nil || target.Port < 1 || target.Port > 65535 || target.Zone != "" {
		return 0, errors.New("SOCKS5 UDP requires an IP relay endpoint")
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	p.writeBuf = p.writeBuf[:0]
	if ip := target.IP.To4(); ip != nil {
		p.writeBuf = append(p.writeBuf, 0, 0, 0, 1)
		p.writeBuf = append(p.writeBuf, ip...)
	} else if ip := target.IP.To16(); ip != nil {
		p.writeBuf = append(p.writeBuf, 0, 0, 0, 4)
		p.writeBuf = append(p.writeBuf, ip...)
	} else {
		return 0, errors.New("SOCKS5 UDP requires a valid relay IP")
	}
	p.writeBuf = append(p.writeBuf, byte(target.Port>>8), byte(target.Port))
	if len(b) > socksUDPMax-len(p.writeBuf) {
		return 0, errors.New("SOCKS5 UDP datagram too large")
	}
	p.writeBuf = append(p.writeBuf, b...)
	n, err := p.udp.Write(p.writeBuf)
	if err != nil {
		return 0, err
	}
	if n != len(p.writeBuf) {
		return 0, io.ErrShortWrite
	}
	return len(b), nil
}

func (p *socksUDPPacketConn) Close() error {
	// Source of truth: https://datatracker.ietf.org/doc/html/rfc1928#section-6
	p.closeOnce.Do(func() {
		p.mu.Lock()
		stop := p.stopCancel
		p.mu.Unlock()
		if stop != nil {
			stop()
		}
		p.control.Close()
		p.closeErr = p.udp.Close()
	})
	return p.closeErr
}
func (p *socksUDPPacketConn) LocalAddr() net.Addr                { return p.udp.LocalAddr() }
func (p *socksUDPPacketConn) SetDeadline(t time.Time) error      { return p.udp.SetDeadline(t) }
func (p *socksUDPPacketConn) SetReadDeadline(t time.Time) error  { return p.udp.SetReadDeadline(t) }
func (p *socksUDPPacketConn) SetWriteDeadline(t time.Time) error { return p.udp.SetWriteDeadline(t) }
