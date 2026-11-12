// Package statuspoll runs status queries without blocking a UI event loop.
package statuspoll

// Poller serializes queries and coalesces requests arriving during a query.
// Refresh, Close, and dispatched callbacks must run on the UI thread.
type Poller[T any] struct {
	query    func() (T, error)
	dispatch func(func())
	apply    func(T, error)
	running  bool
	pending  bool
	closed   bool
}

func New[T any](query func() (T, error), dispatch func(func()), apply func(T, error)) *Poller[T] {
	return &Poller[T]{query: query, dispatch: dispatch, apply: apply}
}

func (p *Poller[T]) Refresh() {
	if p.closed {
		return
	}
	if p.running {
		p.pending = true
		return
	}
	p.running = true
	go func() {
		value, err := p.query()
		p.dispatch(func() {
			p.running = false
			if p.closed {
				return
			}
			if p.pending {
				// An action or visibility change requested fresher data while
				// this query was running. Do not display its older snapshot.
				p.pending = false
				p.Refresh()
				return
			}
			p.apply(value, err)
		})
	}()
}

func (p *Poller[T]) Close() {
	p.closed = true
}
