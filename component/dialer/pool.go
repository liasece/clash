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
	keepAliveTime     = 30 * time.Second
	keepPoolAliveTime = 10 * time.Minute
)

type Pools struct {
	m  map[string]*Pool
	mu sync.Mutex
}

func (p *Pools) MustGet(id string, network, address string, opt *option) *Pool {
	p.mu.Lock()
	if p.m == nil {
		p.m = make(map[string]*Pool)
	}
	for k, v := range p.m {
		if v != nil && v.closed {
			p.m[k] = nil
		}
	}
	v, ok := p.m[id]
	if !ok || v == nil || v.closed {
		// new a pool
		pool := &Pool{
			key:       id,
			signal:    make(chan struct{}, poolSize),
			list:      make([]*PoolConn, poolSize),
			maxNum:    poolSize,
			keepAlive: keepAliveTime,

			network: network,
			address: address,
			opt:     opt,
		}
		pool.Watch()
		p.m[id] = pool
		log.Debugln("[Pool] MustGet create pool: %s %s %s", id, network, address)
		v = pool
	}
	p.mu.Unlock()
	return v
}

type PoolConn struct {
	createAt time.Time
	conn     net.Conn
	cancel   context.CancelFunc
}

type Pool struct {
	key        string
	once       sync.Once
	signal     chan struct{}
	mu         sync.Mutex
	list       []*PoolConn
	maxNum     int
	headIndex  int
	currentNum int
	keepAlive  time.Duration
	lastPullAt time.Time
	closed     bool

	network string
	address string
	opt     *option
}

// watch this signal to new a connect
func (p *Pool) Watch() {
	p.once.Do(func() {
		go func() {
			timerDuration := p.keepAlive / 2
			timer := time.NewTimer(timerDuration)
			{
				// init pool
				if p.lastPullAt.IsZero() {
					p.lastPullAt = time.Now()
				}
			}
			defer timer.Stop()

			for {
				select {
				case _, ok := <-p.signal:
					if !ok {
						return
					}
					for i := 0; i < p.maxNum; i++ {
						if p.Len() >= p.maxNum {
							break
						}
						// full pool
						begin := time.Now()
						ctx, cancel := context.WithTimeout(context.Background(), DefaultTCPTimeout)
						conn, err := iDialContext(ctx, p.network, p.address, p.opt)
						if err != nil {
							log.Debugln("[Pool] Watch iDialContext error %s %s {%s} error: %s", p.address, p.network, p.key, err.Error())
							break
						}
						log.Debugln("[Pool] Watch iDialContext finish: %s %s take: %s", p.address, p.network, time.Since(begin))

						err = p.Push(&PoolConn{
							conn:     conn,
							cancel:   cancel,
							createAt: time.Now(),
						})
						if err != nil {
							break
						}
					}
				case _, ok := <-timer.C:
					if !ok {
						return
					}
					if time.Since(p.lastPullAt) > keepPoolAliveTime {
						p.Close()
						return
					}
					p.FlushAlive()
					timer.Reset(timerDuration)
				}
			}
		}()
		p.signal <- struct{}{}
	})
}

func (p *Pool) FlushAlive() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for p.len() > 0 {
		conn := p.list[p.headIndex]
		if time.Since(conn.createAt) > p.keepAlive {
			conn.cancel()
			p.pull()
			log.Debugln("[Pool] FlushAlive pull 1 %s %s %s", p.key, p.address, conn.createAt.String())
		} else {
			break
		}
	}
}

func (p *Pool) Close() {
	close(p.signal)
	p.mu.Lock()
	defer p.mu.Unlock()
	for p.len() > 0 {
		conn, err := p.pull()
		if err != nil {
			break
		}
		conn.cancel()
	}
	p.closed = true
	log.Debugln("[Pool] Close 1 %s %s", p.key, p.address)
}

func (p *Pool) Push(conn *PoolConn) error {
	p.mu.Lock()
	err := p.push(conn)
	p.mu.Unlock()
	return err
}

func (p *Pool) push(conn *PoolConn) error {
	if p.currentNum == p.maxNum {
		return errors.New("pool is full")
	}
	insertPos := (p.headIndex + p.currentNum) % p.maxNum
	p.list[insertPos] = conn
	p.currentNum++
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
	if p.currentNum == 0 {
		return nil, ErrPoolEmpty
	}
	conn := p.list[p.headIndex]
	p.list[p.headIndex] = nil
	p.headIndex = (p.headIndex + 1) % p.maxNum
	p.currentNum--
	p.lastPullAt = time.Now()
	return conn, nil
}

func (p *Pool) Len() int {
	p.mu.Lock()
	l := p.len()
	p.mu.Unlock()
	return l
}

func (p *Pool) len() int {
	return p.currentNum
}
