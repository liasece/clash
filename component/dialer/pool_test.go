package dialer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPool_Len(t *testing.T) {
	poolSize := 3
	p := &Pool{
		key:       "",
		signal:    make(chan struct{}, poolSize),
		list:      make([]*PoolConn, poolSize),
		maxNum:    poolSize,
		keepAlive: keepAliveTime,
	}
	assert.Equal(t, 0, p.Len())
	assert.NoError(t, p.push(&PoolConn{id: "1"}))
	assert.Equal(t, 1, p.Len())
	assert.NoError(t, p.push(&PoolConn{id: "2"}))
	assert.Equal(t, 2, p.Len())
	assert.NoError(t, p.push(&PoolConn{id: "3"}))
	assert.Equal(t, 3, p.Len())
	assert.Error(t, p.push(&PoolConn{id: "4"}))
	assert.Equal(t, 3, p.Len())

	var err error
	var item *PoolConn

	// pull
	item, err = p.pull()
	assert.NoError(t, err)
	assert.Equal(t, "1", item.id)
	assert.Equal(t, 2, p.Len())

	item, err = p.pull()
	assert.NoError(t, err)
	assert.Equal(t, "2", item.id)
	assert.Equal(t, 1, p.Len())
	item, err = p.pull()
	assert.NoError(t, err)
	assert.Equal(t, "3", item.id)
	assert.Equal(t, 0, p.Len())

	item, err = p.pull()
	assert.Error(t, err)
	assert.Equal(t, 0, p.Len())

	assert.Equal(t, 0, p.Len())
	assert.NoError(t, p.push(&PoolConn{id: "5"}))
	assert.Equal(t, 1, p.Len())
	assert.NoError(t, p.push(&PoolConn{id: "6"}))
	assert.Equal(t, 2, p.Len())

	item, err = p.pull()
	assert.NoError(t, err)
	assert.Equal(t, "5", item.id)

	assert.Equal(t, 1, p.Len())
	assert.NoError(t, p.push(&PoolConn{id: "7"}))
	assert.Equal(t, 2, p.Len())
	assert.NoError(t, p.push(&PoolConn{id: "8"}))
	assert.Equal(t, 3, p.Len())
	assert.Error(t, p.push(&PoolConn{id: "9"}))
	assert.Equal(t, 3, p.Len())

	item, err = p.pull()
	assert.NoError(t, err)
	assert.Equal(t, "6", item.id)

	item, err = p.pull()
	assert.NoError(t, err)
	assert.Equal(t, "7", item.id)

	item, err = p.pull()
	assert.NoError(t, err)
	assert.Equal(t, "8", item.id)
}
