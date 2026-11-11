package storage

// Store handles persistent key-value records.
type Store struct {
	data map[string]string
}

