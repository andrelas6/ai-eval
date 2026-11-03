//go:build !unix

package target

import (
	"context"
	"errors"
)

func execProcess(ctx context.Context, bin string, args []string, dir string) ([]byte, []byte, error) {
	return nil, nil, errors.New("running pi is not supported on this platform")
}
