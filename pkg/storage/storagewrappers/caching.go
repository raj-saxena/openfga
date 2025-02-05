package storagewrappers

import (
	"context"
	"fmt"
	"time"

	"github.com/karlseguin/ccache/v3"
	openfgav1 "github.com/openfga/api/proto/openfga/v1"
	"golang.org/x/sync/singleflight"

	"github.com/openfga/openfga/pkg/storage"
)

const ttl = time.Hour * 168

var _ storage.OpenFGADatastore = (*cachedOpenFGADatastore)(nil)

type cachedOpenFGADatastore struct {
	storage.OpenFGADatastore
	lookupGroup singleflight.Group
	cache       *ccache.Cache[*openfgav1.AuthorizationModel]
}

// NewCachedOpenFGADatastore returns a wrapper over a datastore that caches up to maxSize
// [*openfgav1.AuthorizationModel] on every call to storage.ReadAuthorizationModel.
// It caches with unlimited TTL because models are immutable. It uses LRU for eviction.
func NewCachedOpenFGADatastore(inner storage.OpenFGADatastore, maxSize int) *cachedOpenFGADatastore {
	return &cachedOpenFGADatastore{
		OpenFGADatastore: inner,
		cache:            ccache.New(ccache.Configure[*openfgav1.AuthorizationModel]().MaxSize(int64(maxSize))),
	}
}

// ReadAuthorizationModel reads the model corresponding to store and model ID.
func (c *cachedOpenFGADatastore) ReadAuthorizationModel(ctx context.Context, storeID, modelID string) (*openfgav1.AuthorizationModel, error) {
	cacheKey := fmt.Sprintf("%s:%s", storeID, modelID)
	cachedEntry := c.cache.Get(cacheKey)

	if cachedEntry != nil {
		return cachedEntry.Value(), nil
	}

	model, err := c.OpenFGADatastore.ReadAuthorizationModel(ctx, storeID, modelID)
	if err != nil {
		return nil, err
	}

	c.cache.Set(cacheKey, model, ttl) // These are immutable, once created, there cannot be edits, therefore they can be cached without ttl.

	return model, nil
}

// FindLatestAuthorizationModelID returns the last model `id` written for a store.
func (c *cachedOpenFGADatastore) FindLatestAuthorizationModelID(ctx context.Context, storeID string) (string, error) {
	v, err, _ := c.lookupGroup.Do(fmt.Sprintf("FindLatestAuthorizationModelID:%s", storeID), func() (interface{}, error) {
		return c.OpenFGADatastore.FindLatestAuthorizationModelID(ctx, storeID)
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// Close closes the datastore and cleans up any residual resources.
func (c *cachedOpenFGADatastore) Close() {
	c.cache.Stop()
	c.OpenFGADatastore.Close()
}
