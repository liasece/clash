package dialer

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/Dreamacro/clash/component/resolver"
	"github.com/Dreamacro/clash/log"
	"go.uber.org/atomic"
)

var (
	dialMux                    sync.Mutex
	actualSingleDialContext    = singleDialContext
	actualDualStackDialContext = dualStackDialContext
	tcpConcurrent              = false
	DisableIPv6                = false
	ErrorInvalidedNetworkStack = errors.New("invalided network stack")
	ErrorDisableIPv6           = errors.New("IPv6 is disabled, dialer cancel")
	DefaultPools               = &Pools{}
)

func DialContext(ctx context.Context, network, address string, options ...Option) (net.Conn, error) {
	begin := time.Now()
	opt := &option{
		interfaceName: DefaultInterface.Load(),
		routingMark:   int(DefaultRoutingMark.Load()),
	}

	for _, o := range DefaultOptions {
		o(opt)
	}

	for _, o := range options {
		o(opt)
	}

	ps := opt.pools
	if ps == nil {
		ps = DefaultPools
	}
	if ps.PoolSize > 0 && opt.useConnPool && opt.fromProxy {
		defer func() {
			log.Debugln("[Dialer] [Pool] DialContext finish: take: %s %s %s", address, network, time.Since(begin))
		}()
		return poolDialContext(ctx, ps, network, address, opt)
	}

	defer func() {
		log.Debugln("[Dialer] DialContext finish: take: %s %s %s", address, network, time.Since(begin))
	}()
	return iDialContext(ctx, network, address, opt)
}

// IDByHash get id by md5 from interface
func PoolIDByHash(is ...interface{}) (string, error) {
	md5h := md5.New()
	for _, v := range is {
		data, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		_, err = io.WriteString(md5h, string(data))
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(md5h.Sum(nil)), nil
}

func poolDialContext(ctx context.Context, ps *Pools, network, address string, opt *option) (net.Conn, error) {
	key, err := PoolIDByHash(network, address,
		opt.poolID,
		opt.interfaceName,
		opt.addrReuse,
		opt.routingMark,
		opt.direct,
		opt.network,
		opt.prefer,
	)
	if err != nil {
		return nil, err
	}
	p := ps.MustGet(key, network, address, opt)
	conn, err := p.Pull(ctx)
	if err != nil {
		if !errors.Is(err, ErrPoolEmpty) {
			log.Errorln("[Dialer] [Pool] poolDialContext Pull %s %s error: %s", address, network, err.Error())
		}
		log.Debugln("[Dialer] [Pool] poolDialContext Pull %s %s: %s", address, network, err.Error())
		return iDialContext(ctx, network, address, opt)
	}
	poolConn := conn.(*PoolConn)
	log.Debugln("[Dialer] [Pool] poolDialContext success %s %s id: %s", address, network, poolConn.ID())
	return conn, nil
}

func iDialContext(ctx context.Context, network, address string, opt *option) (net.Conn, error) {
	if opt.network == 4 || opt.network == 6 {
		if strings.Contains(network, "tcp") {
			network = "tcp"
		} else {
			network = "udp"
		}

		network = fmt.Sprintf("%s%d", network, opt.network)
	}

	switch network {
	case "tcp4", "tcp6", "udp4", "udp6":
		return actualSingleDialContext(ctx, network, address, opt)
	case "tcp", "udp":
		return actualDualStackDialContext(ctx, network, address, opt)
	default:
		return nil, ErrorInvalidedNetworkStack
	}
}

func ListenPacket(ctx context.Context, network, address string, options ...Option) (net.PacketConn, error) {
	cfg := &option{
		interfaceName: DefaultInterface.Load(),
		routingMark:   int(DefaultRoutingMark.Load()),
	}

	for _, o := range DefaultOptions {
		o(cfg)
	}

	for _, o := range options {
		o(cfg)
	}

	lc := &net.ListenConfig{}
	if cfg.interfaceName != "" {
		addr, err := bindIfaceToListenConfig(cfg.interfaceName, lc, network, address)
		if err != nil {
			return nil, err
		}
		address = addr
	}
	if cfg.addrReuse {
		addrReuseToListenConfig(lc)
	}
	if cfg.routingMark != 0 {
		bindMarkToListenConfig(cfg.routingMark, lc, network, address)
	}

	return lc.ListenPacket(ctx, network, address)
}

func SetDial(concurrent bool) {
	dialMux.Lock()
	tcpConcurrent = concurrent
	if concurrent {
		actualSingleDialContext = concurrentSingleDialContext
		actualDualStackDialContext = concurrentDualStackDialContext
	} else {
		actualSingleDialContext = singleDialContext
		actualDualStackDialContext = dualStackDialContext
	}

	dialMux.Unlock()
}

func GetDial() bool {
	return tcpConcurrent
}

func dialContext(ctx context.Context, network string, destination netip.Addr, port string, opt *option) (net.Conn, error) {
	dialer := &net.Dialer{}
	if opt.interfaceName != "" {
		if err := bindIfaceToDialer(opt.interfaceName, dialer, network, destination); err != nil {
			return nil, err
		}
	}
	if opt.routingMark != 0 {
		bindMarkToDialer(opt.routingMark, dialer, network, destination)
	}

	if DisableIPv6 && destination.Is6() {
		return nil, ErrorDisableIPv6
	}

	return dialer.DialContext(ctx, network, net.JoinHostPort(destination.String(), port))
}

func dualStackDialContext(ctx context.Context, network, address string, opt *option) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	returned := make(chan struct{})
	defer close(returned)

	type dialResult struct {
		net.Conn
		error
		resolved bool
		ipv6     bool
		done     bool
	}
	results := make(chan dialResult)
	var primary, fallback dialResult

	startRacer := func(ctx context.Context, network, host string, direct bool, ipv6 bool) {
		result := dialResult{ipv6: ipv6, done: true}
		defer func() {
			select {
			case results <- result:
			case <-returned:
				if result.Conn != nil {
					_ = result.Conn.Close()
				}
			}
		}()

		var ip netip.Addr
		if ipv6 {
			if !direct {
				ip, result.error = resolver.ResolveIPv6ProxyServerHost(host)
			} else {
				ip, result.error = resolver.ResolveIPv6(host)
			}
		} else {
			if !direct {
				ip, result.error = resolver.ResolveIPv4ProxyServerHost(host)
			} else {
				ip, result.error = resolver.ResolveIPv4(host)
			}
		}
		if result.error != nil {
			return
		}
		result.resolved = true

		result.Conn, result.error = dialContext(ctx, network, ip, port, opt)
	}

	go startRacer(ctx, network+"4", host, opt.direct, false)
	go startRacer(ctx, network+"6", host, opt.direct, true)

	count := 2
	for i := 0; i < count; i++ {
		select {
		case res := <-results:
			if res.error == nil {
				return res.Conn, nil
			}

			if !res.ipv6 {
				primary = res
			} else {
				fallback = res
			}

			if primary.done && fallback.done {
				if primary.resolved {
					return nil, primary.error
				} else if fallback.resolved {
					return nil, fallback.error
				} else {
					return nil, primary.error
				}
			}
		case <-ctx.Done():
			break
		}
	}

	return nil, errors.New("dual stack tcp shake hands failed")
}

func concurrentDualStackDialContext(ctx context.Context, network, address string, opt *option) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	var ips []netip.Addr
	if opt.direct {
		ips, err = resolver.ResolveAllIP(host)
	} else {
		ips, err = resolver.ResolveAllIPProxyServerHost(host)
	}

	if err != nil {
		return nil, err
	}

	return concurrentDialContext(ctx, network, ips, port, opt)
}

func concurrentDialContext(ctx context.Context, network string, ips []netip.Addr, port string, opt *option) (net.Conn, error) {
	returned := make(chan struct{})
	defer close(returned)

	type dialResult struct {
		ip netip.Addr
		net.Conn
		error
		isPrimary bool
		done      bool
	}

	preferCount := atomic.NewInt32(0)
	results := make(chan dialResult)
	tcpRacer := func(ctx context.Context, ip netip.Addr) {
		result := dialResult{ip: ip, done: true}

		defer func() {
			select {
			case results <- result:
			case <-returned:
				if result.Conn != nil {
					_ = result.Conn.Close()
				}
			}
		}()
		if strings.Contains(network, "tcp") {
			network = "tcp"
		} else {
			network = "udp"
		}

		if ip.Is6() {
			network += "6"
			if opt.prefer != 4 {
				result.isPrimary = true
			}
		}

		if ip.Is4() {
			network += "4"
			if opt.prefer != 6 {
				result.isPrimary = true
			}
		}

		if result.isPrimary {
			preferCount.Add(1)
		}

		result.Conn, result.error = dialContext(ctx, network, ip, port, opt)
	}

	for _, ip := range ips {
		go tcpRacer(ctx, ip)
	}

	connCount := len(ips)
	var fallback dialResult
	var primaryError error
	for i := 0; i < connCount; i++ {
		select {
		case res := <-results:
			if res.error == nil {
				if res.isPrimary {
					return res.Conn, nil
				} else {
					if !fallback.done || fallback.error != nil {
						fallback = res
					}
				}
			} else {
				if res.isPrimary {
					primaryError = res.error
					preferCount.Add(-1)
					if preferCount.Load() == 0 && fallback.done && fallback.error == nil {
						return fallback.Conn, nil
					}
				}
			}
		case <-ctx.Done():
			if fallback.done && fallback.error == nil {
				return fallback.Conn, nil
			}
			break
		}
	}

	if fallback.done && fallback.error == nil {
		return fallback.Conn, nil
	}

	if primaryError != nil {
		return nil, primaryError
	}

	if fallback.error != nil {
		return nil, fallback.error
	}

	return nil, fmt.Errorf("all ips %v tcp shake hands failed", ips)
}

func singleDialContext(ctx context.Context, network string, address string, opt *option) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	var ip netip.Addr
	switch network {
	case "tcp4", "udp4":
		if !opt.direct {
			ip, err = resolver.ResolveIPv4ProxyServerHost(host)
		} else {
			ip, err = resolver.ResolveIPv4(host)
		}
	default:
		if !opt.direct {
			ip, err = resolver.ResolveIPv6ProxyServerHost(host)
		} else {
			ip, err = resolver.ResolveIPv6(host)
		}
	}
	if err != nil {
		return nil, err
	}

	return dialContext(ctx, network, ip, port, opt)
}

func concurrentSingleDialContext(ctx context.Context, network string, address string, opt *option) (net.Conn, error) {
	switch network {
	case "tcp4", "udp4":
		return concurrentIPv4DialContext(ctx, network, address, opt)
	default:
		return concurrentIPv6DialContext(ctx, network, address, opt)
	}
}

func concurrentIPv4DialContext(ctx context.Context, network, address string, opt *option) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	var ips []netip.Addr
	if !opt.direct {
		ips, err = resolver.ResolveAllIPv4ProxyServerHost(host)
	} else {
		ips, err = resolver.ResolveAllIPv4(host)
	}

	if err != nil {
		return nil, err
	}

	return concurrentDialContext(ctx, network, ips, port, opt)
}

func concurrentIPv6DialContext(ctx context.Context, network, address string, opt *option) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	var ips []netip.Addr
	if !opt.direct {
		ips, err = resolver.ResolveAllIPv6ProxyServerHost(host)
	} else {
		ips, err = resolver.ResolveAllIPv6(host)
	}

	if err != nil {
		return nil, err
	}

	return concurrentDialContext(ctx, network, ips, port, opt)
}
