package batch

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPool_GetPut(t *testing.T) {
	t.Parallel()

	p := NewPool[int](10)

	b1 := p.Get()
	assert.NotNil(t, b1)
	assert.Equal(t, 0, b1.Len())

	b1.Append(1, nil, nil, 0)
	b1.Append(2, nil, nil, 0)
	assert.Equal(t, 2, b1.Len())

	p.Put(b1)

	b2 := p.Get()
	assert.NotNil(t, b2)
	assert.Equal(t, 0, b2.Len())
}

func TestPool_Concurrent(t *testing.T) {
	t.Parallel()

	p := NewPool[int](5)
	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range iterations {
				b := p.Get()
				b.Append(1, nil, nil, 0)
				b.Append(2, nil, nil, 0)
				p.Put(b)
			}
		})
	}
	wg.Wait()
}

func TestPool_DefaultCapacity(t *testing.T) {
	t.Parallel()

	p := NewPool[int](0)
	b := p.Get()
	assert.NotNil(t, b)

	for i := range 99 {
		assert.False(t, b.Append(i, nil, nil, 0))
	}
	assert.True(t, b.Append(99, nil, nil, 0))
}
