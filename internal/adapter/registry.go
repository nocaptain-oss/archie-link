package adapter

type Registry struct {
	adapters map[string]Adapter
}

func NewRegistry(items ...Adapter) *Registry {
	adapters := make(map[string]Adapter, len(items))
	for _, item := range items {
		adapters[item.Name()] = item
	}
	return &Registry{adapters: adapters}
}

func (r *Registry) For(os string) (Adapter, error) {
	if adapter, ok := r.adapters[os]; ok {
		return adapter, nil
	}
	return nil, ErrUnsupportedPlatform
}
