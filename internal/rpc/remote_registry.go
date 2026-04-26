package rpc

import (
	"sync"

	"ai-agent/internal/engine"
)

// RemoteRegistry は guard.register / verifier.register でラッパーから登録された
// 名前付きガード/Verifier を保持する。configure 時に builtin で名前解決できなかった
// ものをここから引いてフォールバックする。
type RemoteRegistry struct {
	mu             sync.RWMutex
	inputGuards    map[string]engine.InputGuard
	toolCallGuards map[string]engine.ToolCallGuard
	outputGuards   map[string]engine.OutputGuard
	verifiers      map[string]engine.Verifier
}

// NewRemoteRegistry は空の RemoteRegistry を生成する。
func NewRemoteRegistry() *RemoteRegistry {
	return &RemoteRegistry{
		inputGuards:    make(map[string]engine.InputGuard),
		toolCallGuards: make(map[string]engine.ToolCallGuard),
		outputGuards:   make(map[string]engine.OutputGuard),
		verifiers:      make(map[string]engine.Verifier),
	}
}

// AddInputGuard は名前付き InputGuard を登録する（既存の同名は上書き）。
func (r *RemoteRegistry) AddInputGuard(g engine.InputGuard) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inputGuards[g.Name()] = g
}

// AddToolCallGuard は名前付き ToolCallGuard を登録する。
func (r *RemoteRegistry) AddToolCallGuard(g engine.ToolCallGuard) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolCallGuards[g.Name()] = g
}

// AddOutputGuard は名前付き OutputGuard を登録する。
func (r *RemoteRegistry) AddOutputGuard(g engine.OutputGuard) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outputGuards[g.Name()] = g
}

// AddVerifier は名前付き Verifier を登録する。
func (r *RemoteRegistry) AddVerifier(v engine.Verifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.verifiers[v.Name()] = v
}

// LookupInputGuard は名前から InputGuard を解決する。未登録時は (nil, false)。
func (r *RemoteRegistry) LookupInputGuard(name string) (engine.InputGuard, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.inputGuards[name]
	return g, ok
}

// LookupToolCallGuard は名前から ToolCallGuard を解決する。
func (r *RemoteRegistry) LookupToolCallGuard(name string) (engine.ToolCallGuard, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.toolCallGuards[name]
	return g, ok
}

// LookupOutputGuard は名前から OutputGuard を解決する。
func (r *RemoteRegistry) LookupOutputGuard(name string) (engine.OutputGuard, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.outputGuards[name]
	return g, ok
}

// LookupVerifier は名前から Verifier を解決する。
func (r *RemoteRegistry) LookupVerifier(name string) (engine.Verifier, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.verifiers[name]
	return v, ok
}
