package filestore

import (
	"errors"
	"io"

	"github.com/google/uuid"

	"github.com/exotermo/hermes-app-go/internal/application"
	"github.com/exotermo/hermes-app-go/internal/domain"
)

// Blobs adapts Store to application.FileBlobs.
type Blobs struct{ Store *Store }

func (b Blobs) Save(id uuid.UUID, src io.Reader, maxBytes int64) (application.SavedBlob, error) {
	saved, err := b.Store.Save(id, src, maxBytes)
	if errors.Is(err, ErrTooLarge) {
		return application.SavedBlob{TooLarge: true}, nil
	}
	if err != nil {
		return application.SavedBlob{}, err
	}
	return application.SavedBlob{Salt: saved.Params.Salt, NoncePrefix: saved.Params.NoncePrefix, ChunkSize: saved.Params.ChunkSize,
		Size: saved.Size, SHA256: saved.SHA256, Head: saved.Head}, nil
}

func (b Blobs) Open(id uuid.UUID, file domain.ProjectFile) (io.Reader, io.Closer, error) {
	return b.Store.Open(id, Params{Salt: file.Salt, NoncePrefix: file.NoncePrefix, ChunkSize: file.ChunkSize})
}

func (b Blobs) Remove(id uuid.UUID) error { return b.Store.Remove(id) }
