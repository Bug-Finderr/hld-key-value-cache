package cache

type Entry struct {
	key   string
	value string
}

func (e *Entry) Key() string {
	return e.key
}

func (e *Entry) Value() string {
	return e.value
}
