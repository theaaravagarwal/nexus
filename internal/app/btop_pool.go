package app

import (
	"context"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

// btopStreamPool owns the single PTY-backed btop session shown in Monitor.
type btopStreamPool struct {
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	sessions map[string]*pooledBtopStream
	active   string
	updates  chan btopStreamEventMsg
	nextGen  uint64
	// lastBoxes remembers, per target, the shown_boxes value the layout
	// fitting ladder last had to fall back to (and the viewport it was
	// chosen for), so the next activation for that target can start from
	// it instead of re-discovering it from the user's own config every
	// time. It is only consulted when the new viewport is not larger in
	// either dimension than the one that forced the fallback.
	lastBoxes map[string]btopRememberedBoxes
}

type btopRememberedBoxes struct {
	boxes   string
	columns int
	rows    int
}

type pooledBtopStream struct {
	target     string
	generation uint64
	columns    int
	rows       int
	boxes      string
	cancel     context.CancelFunc
	running    bool
	latest     btopStreamEventMsg
}

type btopStreamPoolClosedMsg struct{}

func newBtopStreamPool() *btopStreamPool {
	ctx, cancel := context.WithCancel(context.Background())
	return &btopStreamPool{
		ctx:       ctx,
		cancel:    cancel,
		sessions:  make(map[string]*pooledBtopStream),
		updates:   make(chan btopStreamEventMsg, 1),
		lastBoxes: make(map[string]btopRememberedBoxes),
	}
}

func (p *btopStreamPool) activate(target string, columns, rows int) (btopStreamEventMsg, bool) {
	p.mu.Lock()
	for existingTarget, session := range p.sessions {
		if existingTarget == target {
			continue
		}
		if session.cancel != nil {
			session.cancel()
		}
		delete(p.sessions, existingTarget)
	}
	p.active = target
	if session := p.sessions[target]; session != nil &&
		session.columns == columns && session.rows == rows && session.running {
		latest := session.latest
		if latest.Generation == 0 {
			latest.Generation = session.generation
			latest.Target = session.target
		}
		p.mu.Unlock()
		return latest, false
	}
	if session := p.sessions[target]; session != nil {
		if session.cancel != nil {
			session.cancel()
		}
		delete(p.sessions, target)
	}
	if p.ctx.Err() != nil {
		p.mu.Unlock()
		return btopStreamEventMsg{}, false
	}
	startBoxes := ""
	if remembered, ok := p.lastBoxes[target]; ok &&
		columns <= remembered.columns && rows <= remembered.rows {
		startBoxes = remembered.boxes
	}

	p.nextGen++
	generation := p.nextGen
	ctx, cancel := context.WithCancel(p.ctx)
	session := &pooledBtopStream{
		target: target, generation: generation, columns: columns, rows: rows,
		boxes: startBoxes, cancel: cancel, running: true,
	}
	p.sessions[target] = session
	p.mu.Unlock()

	events := make(chan btopStreamEventMsg, 1)
	go func() {
		usedBoxes := streamRemoteBtopFrom(ctx, target, generation, columns, rows, startBoxes, events)
		p.rememberBoxes(target, generation, columns, rows, usedBoxes)
	}()
	go p.consume(session, events)
	return btopStreamEventMsg{Generation: generation, Target: target}, true
}

// rememberBoxes records the shown_boxes value a completed session settled
// on, so the next activation for this target can skip straight to a layout
// that is already known to fit instead of retrying the user's config (and
// every richer table entry) first. Only a non-default (fitted) box set is
// worth remembering; "" means the user's own config rendered fine.
func (p *btopStreamPool) rememberBoxes(target string, generation uint64, columns, rows int, boxes string) {
	if boxes == "" {
		return
	}
	p.mu.Lock()
	if session := p.sessions[target]; session == nil || session.generation != generation {
		p.mu.Unlock()
		return
	}
	p.lastBoxes[target] = btopRememberedBoxes{boxes: boxes, columns: columns, rows: rows}
	p.mu.Unlock()
}

func (p *btopStreamPool) consume(session *pooledBtopStream, events <-chan btopStreamEventMsg) {
	for event := range events {
		p.mu.Lock()
		if p.sessions[session.target] != session {
			p.mu.Unlock()
			continue
		}
		session.latest = event
		if event.Done {
			session.running = false
			session.cancel = nil
		}
		active := p.active == session.target
		p.mu.Unlock()
		if active && !publishBtopStreamEvent(p.ctx, p.updates, event) {
			return
		}
	}
}

func (p *btopStreamPool) deactivate() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if session := p.sessions[p.active]; session != nil {
		if session.cancel != nil {
			session.cancel()
		}
		delete(p.sessions, p.active)
	}
	p.active = ""
	p.mu.Unlock()
}

func (p *btopStreamPool) cancelTarget(target string) {
	if p == nil || target == "" {
		return
	}
	p.mu.Lock()
	if session := p.sessions[target]; session != nil {
		if session.cancel != nil {
			session.cancel()
		}
		delete(p.sessions, target)
	}
	p.mu.Unlock()
}

func (p *btopStreamPool) close() {
	if p == nil {
		return
	}
	p.cancel()
	p.mu.Lock()
	for _, session := range p.sessions {
		if session.cancel != nil {
			session.cancel()
		}
	}
	p.sessions = make(map[string]*pooledBtopStream)
	p.active = ""
	p.mu.Unlock()
}

func waitForBtopStreamPool(pool *btopStreamPool) tea.Cmd {
	return func() tea.Msg {
		select {
		case message := <-pool.updates:
			return message
		case <-pool.ctx.Done():
			return btopStreamPoolClosedMsg{}
		}
	}
}
