package nilpanic

type Cache struct {
	items map[string]string
}

func NewCache() *Cache {
	// BUG: items map is not initialized
	return &Cache{}
}

func (c *Cache) Put(key, val string) {
	c.items[key] = val
}

func (c *Cache) Get(key string) string {
	return c.items[key]
}
