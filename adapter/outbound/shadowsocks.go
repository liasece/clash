package outbound

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/Dreamacro/clash/common/structure"
	"github.com/Dreamacro/clash/component/dialer"
	C "github.com/Dreamacro/clash/constant"
	"github.com/Dreamacro/clash/log"
	obfs "github.com/Dreamacro/clash/transport/simple-obfs"
	"github.com/Dreamacro/clash/transport/socks5"
	v2rayObfs "github.com/Dreamacro/clash/transport/v2ray-plugin"
	shadowsocks "github.com/sagernet/sing-shadowsocks"
	"github.com/sagernet/sing-shadowsocks/shadowimpl"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/uot"
)

type ShadowSocks struct {
	*Base
	method shadowsocks.Method

	option *ShadowSocksOption
	// obfs
	obfsMode    string
	obfsOption  *simpleObfsOption
	v2rayOption *v2rayObfs.Option
}

type ShadowSocksOption struct {
	BasicOption
	Name       string         `proxy:"name"`
	Server     string         `proxy:"server"`
	Port       int            `proxy:"port"`
	Password   string         `proxy:"password"`
	Cipher     string         `proxy:"cipher"`
	UDP        bool           `proxy:"udp,omitempty"`
	Plugin     string         `proxy:"plugin,omitempty"`
	PluginOpts map[string]any `proxy:"plugin-opts,omitempty"`
	UDPOverTCP bool           `proxy:"udp-over-tcp,omitempty"`
}

type simpleObfsOption struct {
	Mode string `obfs:"mode,omitempty"`
	Host string `obfs:"host,omitempty"`
}

type v2rayObfsOption struct {
	Mode           string            `obfs:"mode"`
	Host           string            `obfs:"host,omitempty"`
	Path           string            `obfs:"path,omitempty"`
	TLS            bool              `obfs:"tls,omitempty"`
	Fingerprint    string            `obfs:"fingerprint,omitempty"`
	Headers        map[string]string `obfs:"headers,omitempty"`
	SkipCertVerify bool              `obfs:"skip-cert-verify,omitempty"`
	Mux            bool              `obfs:"mux,omitempty"`
}

func (ss *ShadowSocks) initProto(c net.Conn) (net.Conn, error) {
	switch ss.obfsMode {
	case "tls":
		c = obfs.NewTLSObfs(c, ss.obfsOption.Host)
	case "http":
		_, port, _ := net.SplitHostPort(ss.addr)
		c = obfs.NewHTTPObfs(c, ss.obfsOption.Host, port)
	case "websocket":
		var err error
		c, err = v2rayObfs.NewV2rayObfs(c, ss.v2rayOption)
		if err != nil {
			return nil, fmt.Errorf("%s connect error: %w", ss.addr, err)
		}
	}
	return c, nil
}

// StreamConn implements C.ProxyAdapter
func (ss *ShadowSocks) StreamConn(c net.Conn, metadata *C.Metadata) (net.Conn, error) {
	if metadata.NetWork == C.UDP && ss.option.UDPOverTCP {
		return ss.method.DialConn(c, M.ParseSocksaddr(uot.UOTMagicAddress+":443"))
	}
	return ss.method.DialConn(c, M.ParseSocksaddr(metadata.RemoteAddress()))
}

func (ss *ShadowSocks) initInPool(c net.Conn) (net.Conn, error) {
	tcpKeepAlive(c)
	return ss.initProto(c)
}

// DialContext implements C.ProxyAdapter
func (ss *ShadowSocks) DialContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (C.Conn, error) {
	connID := ""
	begin := time.Now()
	defer func() {
		log.Debugln("[ShadowSocks] DialContext all finish: %s connID: %s tcp take: %s inTransaction: %s %s --> %s", ss.addr, connID, time.Since(begin), time.Since(metadata.CreateAt), metadata.SourceDetail(), metadata.RemoteAddress())
	}()
	for {
		c, err := dialer.DialContext(ctx, "tcp", ss.addr, append([]dialer.Option{dialer.WithFromProxy(true), dialer.WithOnPoolConnect(ss.initInPool)}, ss.Base.DialOptions(opts...)...)...)
		if err != nil {
			return nil, fmt.Errorf("%s connect error: %w", ss.addr, err)
		}
		connID = ""
		if poolConn, _ := c.(*dialer.PoolConn); poolConn != nil {
			connID = poolConn.ID()
		}

		log.Debugln("[ShadowSocks] DialContext dialer finish: %s tcp connID: %s take: %s inTransaction: %s %s --> %s", ss.addr, connID, time.Since(begin), time.Since(metadata.CreateAt), metadata.SourceDetail(), metadata.RemoteAddress())

		res, err := ss.iDialContext(c, metadata)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || strings.Contains(err.Error(), "unexpected EOF") {
				log.Warnln("[ShadowSocks] iDialContext error with continue: %s connID: %s error: %s", ss.addr, connID, err)
				// continue
			} else {
				log.Warnln("[ShadowSocks] iDialContext error: %s connID: %s error: %s", ss.addr, connID, err)
			}
		}
		return res, err
	}
}

func (ss *ShadowSocks) iDialContext(c net.Conn, metadata *C.Metadata) (_ C.Conn, err error) {
	if poolConn, ok := c.(*dialer.PoolConn); !ok || poolConn == nil {
		tcpKeepAlive(c)
	}
	defer safeConnClose(c, err)
	if poolConn, ok := c.(*dialer.PoolConn); !ok || poolConn == nil {
		var err error
		c, err = ss.initProto(c)
		if err != nil {
			return nil, err
		}
	}
	c, err = ss.StreamConn(c, metadata)
	return NewConn(c, ss), err
}

// ListenPacketContext implements C.ProxyAdapter
func (ss *ShadowSocks) ListenPacketContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (C.PacketConn, error) {
	if ss.option.UDPOverTCP {
		tcpConn, err := ss.DialContext(ctx, metadata, opts...)
		if err != nil {
			return nil, err
		}
		return newPacketConn(uot.NewClientConn(tcpConn), ss), nil
	}
	pc, err := dialer.ListenPacket(ctx, "udp", "", ss.Base.DialOptions(opts...)...)
	if err != nil {
		return nil, err
	}

	addr, err := resolveUDPAddrWithPrefer("udp", ss.addr, ss.prefer)
	if err != nil {
		pc.Close()
		return nil, err
	}
	pc = ss.method.DialPacketConn(&bufio.BindPacketConn{PacketConn: pc, Addr: addr})
	return newPacketConn(pc, ss), nil
}

// ListenPacketOnStreamConn implements C.ProxyAdapter
func (ss *ShadowSocks) ListenPacketOnStreamConn(c net.Conn, metadata *C.Metadata) (_ C.PacketConn, err error) {
	if ss.option.UDPOverTCP {
		return newPacketConn(uot.NewClientConn(c), ss), nil
	}
	return nil, errors.New("no support")
}

// SupportUOT implements C.ProxyAdapter
func (ss *ShadowSocks) SupportUOT() bool {
	return ss.option.UDPOverTCP
}

func NewShadowSocks(option ShadowSocksOption) (*ShadowSocks, error) {
	addr := net.JoinHostPort(option.Server, strconv.Itoa(option.Port))
	method, err := shadowimpl.FetchMethod(option.Cipher, option.Password)
	if err != nil {
		return nil, fmt.Errorf("ss %s initialize error: %w", addr, err)
	}

	var v2rayOption *v2rayObfs.Option
	var obfsOption *simpleObfsOption
	obfsMode := ""

	decoder := structure.NewDecoder(structure.Option{TagName: "obfs", WeaklyTypedInput: true})
	if option.Plugin == "obfs" {
		opts := simpleObfsOption{Host: "bing.com"}
		if err := decoder.Decode(option.PluginOpts, &opts); err != nil {
			return nil, fmt.Errorf("ss %s initialize obfs error: %w", addr, err)
		}

		if opts.Mode != "tls" && opts.Mode != "http" {
			return nil, fmt.Errorf("ss %s obfs mode error: %s", addr, opts.Mode)
		}
		obfsMode = opts.Mode
		obfsOption = &opts
	} else if option.Plugin == "v2ray-plugin" {
		opts := v2rayObfsOption{Host: "bing.com", Mux: true}
		if err := decoder.Decode(option.PluginOpts, &opts); err != nil {
			return nil, fmt.Errorf("ss %s initialize v2ray-plugin error: %w", addr, err)
		}

		if opts.Mode != "websocket" {
			return nil, fmt.Errorf("ss %s obfs mode error: %s", addr, opts.Mode)
		}
		obfsMode = opts.Mode
		v2rayOption = &v2rayObfs.Option{
			Host:    opts.Host,
			Path:    opts.Path,
			Headers: opts.Headers,
			Mux:     opts.Mux,
		}

		if opts.TLS {
			v2rayOption.TLS = true
			v2rayOption.SkipCertVerify = opts.SkipCertVerify
		}
	}

	return &ShadowSocks{
		Base: &Base{
			name:   option.Name,
			addr:   addr,
			tp:     C.Shadowsocks,
			udp:    option.UDP,
			iface:  option.Interface,
			rmark:  option.RoutingMark,
			prefer: C.NewDNSPrefer(option.IPVersion),
		},
		method: method,

		option:      &option,
		obfsMode:    obfsMode,
		v2rayOption: v2rayOption,
		obfsOption:  obfsOption,
	}, nil
}

type ssPacketConn struct {
	net.PacketConn
	rAddr net.Addr
}

func (spc *ssPacketConn) WriteTo(b []byte, addr net.Addr) (n int, err error) {
	packet, err := socks5.EncodeUDPPacket(socks5.ParseAddrToSocksAddr(addr), b)
	if err != nil {
		return
	}
	return spc.PacketConn.WriteTo(packet[3:], spc.rAddr)
}

func (spc *ssPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, _, e := spc.PacketConn.ReadFrom(b)
	if e != nil {
		return 0, nil, e
	}

	addr := socks5.SplitAddr(b[:n])
	if addr == nil {
		return 0, nil, errors.New("parse addr error")
	}

	udpAddr := addr.UDPAddr()
	if udpAddr == nil {
		return 0, nil, errors.New("parse addr error")
	}

	copy(b, b[len(addr):])
	return n - len(addr), udpAddr, e
}
