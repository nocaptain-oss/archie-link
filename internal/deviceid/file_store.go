package deviceid

import (
	"context"
	"errors"
	"os"
)

type FileStore struct {
	Path string
}

func (s FileStore) Load(ctx context.Context, _ string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrSecretNotFound
	}
	return string(raw), err
}

func (s FileStore) Save(ctx context.Context, _ string, secret string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.WriteFile(s.Path, []byte(secret), 0o600)
}
