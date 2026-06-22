// Package s3srv runs an S3-compatible server inside doze by embedding gofakes3
// (a pure-Go, in-process S3 implementation) over a bbolt-backed store in the
// instance's data directory. It registers itself with awslocal so the hidden
// `doze __serve s3` worker can host it; the engine/s3 driver fronts it.
//
// gofakes3 is dev-grade: object CRUD, listing, multipart (via gofakes3's core
// uploader), and presigned URLs all work; bucket versioning is not supported by
// the bolt backend (a documented limitation — see the plan's weed-mini upgrade
// path).
package s3srv

import (
	"io"
	"net/http"
	"path/filepath"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3bolt"
	bolt "go.etcd.io/bbolt"

	"github.com/nerdmenot/doze/internal/awslocal"
)

func init() { awslocal.RegisterServer("s3", New) }

// New opens the bbolt store under datadir and returns the gofakes3 HTTP handler
// plus the bolt DB as the io.Closer (gofakes3's s3bolt backend does not expose
// one, so we own the handle and close it on shutdown).
func New(datadir string) (http.Handler, io.Closer, error) {
	db, err := bolt.Open(filepath.Join(datadir, "s3.bolt"), 0o600, nil)
	if err != nil {
		return nil, nil, err
	}
	// AutoBucket off: faithful S3 semantics (NoSuchBucket until created). Buckets
	// declared in config are created by the engine/s3 Converger; clients may also
	// CreateBucket explicitly.
	faker := gofakes3.New(s3bolt.New(db), gofakes3.WithAutoBucket(false))
	return faker.Server(), db, nil
}
