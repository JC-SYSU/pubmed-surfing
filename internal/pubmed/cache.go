package pubmed

import (
	"fmt"
	"net/url"
	"sort"
	"sync"
	"time"
)

const CacheTTL = 10 * time.Minute
const CacheMaxItems = 256

type cacheEntry struct {
	data []byte
	at   time.Time
	seq  uint64
}
type Cache struct {
	mu    sync.Mutex
	items map[string]cacheEntry
	seq   uint64
	now   func() time.Time
}

func NewCache(now func() time.Time) *Cache {
	if now == nil {
		now = time.Now
	}
	return &Cache{items: map[string]cacheEntry{}, now: now}
}
func (c *Cache) Get(k string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[k]
	if !ok {
		return nil, false
	}
	if c.now().Sub(e.at) >= CacheTTL {
		delete(c.items, k)
		return nil, false
	}
	return append([]byte(nil), e.data...), true
}
func (c *Cache) Set(k string, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	c.items[k] = cacheEntry{append([]byte(nil), b...), c.now(), c.seq}
	if len(c.items) > CacheMaxItems {
		var oldest string
		var n uint64 = ^uint64(0)
		for k, e := range c.items {
			if e.seq < n {
				oldest, n = k, e.seq
			}
		}
		delete(c.items, oldest)
	}
}
func (c *Cache) Clear() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(c.items)
	c.items = map[string]cacheEntry{}
	return n
}
func CacheKey(endpoint string, p url.Values) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	q := url.Values{}
	for _, k := range keys {
		vals := append([]string(nil), p[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			q.Add(k, v)
		}
	}
	return fmt.Sprintf("%s?%s", endpoint, q.Encode())
}
