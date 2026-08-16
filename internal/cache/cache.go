package cache

import (
	"sync"

	"github.com/bluele/gcache"
	"github.com/zneng101/promptgate/internal/store"
)

// Cache 提供模板的热数据缓存：原子指针替换 + 渲染结果 LRU。
//
//   - 模板缓存使用「双指针 + 读写锁」原子替换：发布新版本时整体替换 map 指针，
//     正在处理的旧请求仍持有旧指针引用，不受影响，等待 GC 回收。
//   - 渲染结果缓存使用 gcache LRU，避免相同 (模板, 变量) 反复渲染。
type Cache struct {
	mu      sync.RWMutex
	data    map[string]*store.PromptTemplate // 原子替换的模板热数据
	version int64                            // 缓存版本号，每次替换自增

	renderCache gcache.Cache // 渲染结果 LRU，键为 renderKey
}

// New 创建缓存。size 为渲染结果 LRU 容量。
func New(size int) *Cache {
	if size <= 0 {
		size = 1024
	}
	return &Cache{
		data:        make(map[string]*store.PromptTemplate),
		renderCache: gcache.New(size).LRU().Build(),
	}
}

// Get 原子读取某个模板（读锁，不阻塞并发读）。
func (c *Cache) Get(name string) (*store.PromptTemplate, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.data[name]
	return t, ok
}

// All 原子读取全部模板的快照（用于 UI 展示）。
func (c *Cache) All() map[string]*store.PromptTemplate {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]*store.PromptTemplate, len(c.data))
	for k, v := range c.data {
		out[k] = v
	}
	return out
}

// AtomicUpdate 原子替换整个模板缓存（发布新版本时调用，无需重启）。
// 直接替换指针，旧数据等待 GC 回收，正在处理的请求不受影响。
func (c *Cache) AtomicUpdate(newData map[string]*store.PromptTemplate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = newData
	c.version++
}

// Version 返回当前缓存版本号（用于前端轮询是否有更新）。
func (c *Cache) Version() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.version
}

// Len 返回缓存模板数量。
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

// GetRender 从 LRU 读取已渲染结果。
func (c *Cache) GetRender(key string) (string, bool) {
	v, err := c.renderCache.Get(key)
	if err != nil {
		return "", false
	}
	s, _ := v.(string)
	return s, true
}

// SetRender 写入已渲染结果到 LRU。
func (c *Cache) SetRender(key, value string) {
	_ = c.renderCache.Set(key, value)
}
