package json

import (
	"context"
	"encoding/json"
	"io"

	"github.com/go-kit/kit/log"

	"github.com/simonswine/mi-flora-remote-write/miflora/model"
)

type JSON struct {
	logger log.Logger
}

func New(logger log.Logger) *JSON {
	return &JSON{
		logger: logger,
	}
}

func (j *JSON) Run(ctx context.Context, w io.Writer) (chan *model.Result, chan error, error) {
	resultsCh := make(chan *model.Result)
	errCh := make(chan error)

	enc := json.NewEncoder(w)

	go func() {
		defer close(errCh)

		for result := range resultsCh {
			if err := enc.Encode(result); err != nil {
				errCh <- err
				break
			}
		}
	}()

	return resultsCh, errCh, nil
}
