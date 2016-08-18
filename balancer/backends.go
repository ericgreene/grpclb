package balancer

import (
	"sort"
	"sync"
	"time"

	balancerpb "github.com/bsm/grpclb/grpclb_balancer_v1"
)

type strset []string

func toStrset(vv []string) strset {
	sort.Strings(vv)
	return strset(vv)
}

func (s strset) Contains(v string) bool {
	pos := sort.SearchStrings(s, v)
	return pos < len(s) && s[pos] == v
}

// --------------------------------------------------------------------

type backends struct {
	target string
	set    map[string]*backend
	mu     sync.RWMutex

	queryInterval time.Duration
}

func newBackends(target string, queryInterval time.Duration) *backends {
	return &backends{
		target:        target,
		set:           make(map[string]*backend),
		queryInterval: queryInterval,
	}
}

func (b *backends) Servers() []*balancerpb.Server {
	b.mu.RLock()
	defer b.mu.RUnlock()

	servers := make([]*balancerpb.Server, 0, len(b.set))
	for _, b := range b.set {
		servers = append(servers, b.Server())
	}
	return servers
}

func (b *backends) Update(addrs strset) (err error) {
	var removed []*backend
	var added []string

	b.mu.Lock()
	for addr, backend := range b.set {
		if !addrs.Contains(addr) {
			removed = append(removed, backend)
			delete(b.set, addr)
		}
	}

	for _, addr := range addrs {
		if _, ok := b.set[addr]; !ok {
			added = append(added, addr)
		}
	}
	b.mu.Unlock()

	// Close removed backends
	for _, b := range removed {
		_ = b.Close()
	}

	// Connect to added backends, in parallel
	if len(added) != 0 {
		err = b.connectAll(addrs)
	}
	return
}

func (b *backends) connectAll(addrs []string) (err error) {
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := range addrs {
		wg.Add(1)
		go func(addr string) {
			if e := b.connect(addr); e != nil {
				mu.Lock()
				err = e
				mu.Unlock()
			}
			wg.Done()
		}(addrs[i])
	}
	wg.Wait()
	return
}

func (b *backends) connect(addr string) error {
	backend, err := newBackend(b.target, addr, b.queryInterval)
	if err != nil {
		return err
	}

	b.mu.Lock()
	b.set[addr] = backend
	b.mu.Unlock()
	return nil
}
