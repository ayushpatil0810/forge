# nilpanic fixture

Expected fix: In `store.go`, initialize `items` map: `return &Cache{items: make(map[string]string)}`.
