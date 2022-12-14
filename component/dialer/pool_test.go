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
	assert.NoError(t, p.push(nil))
	assert.Equal(t, 1, p.Len())
	assert.NoError(t, p.push(nil))
	assert.Equal(t, 2, p.Len())
	assert.NoError(t, p.push(nil))
	assert.Equal(t, 3, p.Len())
	assert.Error(t, p.push(nil))
	assert.Equal(t, 3, p.Len())

	// pull
	assert.NoError(t, func() error { _, err := p.pull(); return err }())
	assert.Equal(t, 2, p.Len())
	assert.NoError(t, func() error { _, err := p.pull(); return err }())
	assert.Equal(t, 1, p.Len())
	assert.NoError(t, func() error { _, err := p.pull(); return err }())
	assert.Equal(t, 0, p.Len())
	assert.Error(t, func() error { _, err := p.pull(); return err }())
	assert.Equal(t, 0, p.Len())

	assert.Equal(t, 0, p.Len())
	assert.NoError(t, p.push(nil))
	assert.Equal(t, 1, p.Len())
	assert.NoError(t, p.push(nil))
	assert.Equal(t, 2, p.Len())
	assert.NoError(t, func() error { _, err := p.pull(); return err }())
	assert.Equal(t, 1, p.Len())
	assert.NoError(t, p.push(nil))
	assert.Equal(t, 2, p.Len())
	assert.NoError(t, p.push(nil))
	assert.Equal(t, 3, p.Len())
	assert.Error(t, p.push(nil))
	assert.Equal(t, 3, p.Len())
}
