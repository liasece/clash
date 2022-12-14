package dialer

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/Dreamacro/clash/log"
)

var (
	poolSize          = 5
	ErrPoolEmpty      = errors.New("pool is empty")
	DefaultTCPTimeout = 5 * time.Second
)

type Pools struct {
	m sync.Map
}

func (p *Pools) MustGet(id string, network, address string, opt *option) *Pool {
	v, ok := p.m.Load(id)
	if !ok {
		// new a pool
		pool := &Pool{
			key:    id,
			signal: make(chan struct{}, poolSize),
			list:   make([]*PoolConn, poolSize),
			maxNum: poolSize,

			network: network,
			address: address,
			opt:     opt,
		}
		pool.Watch()
		p.m.Store(id, pool)
		log.Debugln("[Pool] MustGet create pool: %s %s %s", id, network, address)
		return pool
	}

	return v.(*Pool)
}

type PoolConn struct {
	conn   net.Conn
	cancel context.CancelFunc
}

type Pool struct {
	key       string
	once      sync.Once
	signal    chan struct{}
	mu        sync.Mutex
	list      []*PoolConn
	maxNum    int
	headIndex int
	tailIndex int

	network string
	address string
	opt     *option
}

// watch this signal to new a connect
func (p *Pool) Watch() {
	p.once.Do(func() {
		go func() {
			for {
				_, ok := <-p.signal
				if !ok {
					return
				}
				begin := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), DefaultTCPTimeout)
				conn, err := iDialContext(ctx, p.network, p.address, p.opt)
				if err != nil {
					log.Debugln("[Pool] Watch iDialContext error (%s) {%s} addr: %s %s", err.Error(), p.key, p.address, p.network)
					continue
				}
				log.Debugln("[Pool] Watch iDialContext finish: take: %s %s", time.Since(begin), p.address)

				p.mu.Lock()
				p.push(&PoolConn{
					conn:   conn,
					cancel: cancel,
				})
				p.mu.Unlock()
			}
		}()
		for i := 0; i < p.maxNum; i++ {
			p.signal <- struct{}{}
		}
	})
}

func (p *Pool) push(conn *PoolConn) error {
	nextTail := (p.tailIndex + 1) % p.maxNum
	if nextTail == p.headIndex {
		return errors.New("pool is full")
	}
	p.list[p.tailIndex] = conn
	p.tailIndex = nextTail
	return nil
}

func (p *Pool) Pull(ctx context.Context) (net.Conn, error) {
	p.mu.Lock()
	res, err := p.pull()
	p.mu.Unlock()

	// new a conn
	p.signal <- struct{}{}

	if err != nil {
		return nil, err
	}

	go func() {
		ch := ctx.Done()
		if ch == nil {
			return
		}
		<-ch
		res.cancel()
	}()

	return res.conn, err
}

func (p *Pool) pull() (*PoolConn, error) {
	// get a conn from pool
	if p.headIndex == p.tailIndex {
		return nil, ErrPoolEmpty
	}
	conn := p.list[p.headIndex]
	p.list[p.headIndex] = nil
	p.headIndex = (p.headIndex + 1) % p.maxNum
	return conn, nil
}
