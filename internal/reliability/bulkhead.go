package reliability

import "errors"

var ErrBulkheadFull = errors.New("bulkhead capacity reached")

type Bulkhead struct {
	sem chan struct{}
}

func NewBulkhead(maxConcurrency int) *Bulkhead {
	if maxConcurrency <= 0 {
		maxConcurrency = 50
	}
	return &Bulkhead{
		sem: make(chan struct{}, maxConcurrency),
	}
}

func (b *Bulkhead) Acquire() error {
	select {
	case b.sem <- struct{}{}:
		return nil
	default:
		return ErrBulkheadFull
	}
}

func (b *Bulkhead) Release() {
	select {
	case <-b.sem:
	default:
	}
}

func (b *Bulkhead) ActiveCount() int {
	return len(b.sem)
}

func (b *Bulkhead) Capacity() int {
	return cap(b.sem)
}
