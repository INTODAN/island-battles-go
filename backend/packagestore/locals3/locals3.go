// Package locals3 is the bot package bucket of local development: a fake S3
// (gofakes3) that the local server host serves under Prefix and that keeps
// each package as a file in a folder on disk. Browsers and the CLI upload to
// it with the same presigned POST as to S3, and the backend reads it with the
// same range requests, so the local path is the deployed one. It checks no
// signatures or upload policies, so the 250 MiB limit of the presigned POST
// is checked only when the upload is completed. Only the local server host
// links it; the Lambda host uses the real bucket.
package locals3

import (
	"fmt"
	"net/http"
	"strings"

	"islandBattles/backend/packagestore"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3afero"
	"github.com/spf13/afero"
)

// Prefix is the path the local server host serves the fake S3 under.
const Prefix = "/local-s3"

// Bucket is the fake bucket's name.
const Bucket = "bot-packages"

// New keeps packages in directory, which it creates, and returns the store,
// whose requests and presigned POSTs go to origin+Prefix, and the handler to
// serve at Prefix. origin is where both the backend and players reach the
// local server, such as http://localhost:8888.
func New(directory, origin string) (packagestore.Store, http.Handler, error) {
	files, err := s3afero.NewBasePathFs(afero.NewOsFs(), directory, s3afero.FsPathCreateAll)
	if err != nil {
		return nil, nil, fmt.Errorf("create the local bot package folder %s: %w", directory, err)
	}
	backend, err := s3afero.SingleBucket(Bucket, files, nil)
	if err != nil {
		return nil, nil, err
	}
	fake := gofakes3.New(backend, gofakes3.WithLogger(gofakes3.DiscardLog()))
	store := packagestore.Endpoint(strings.TrimRight(origin, "/")+Prefix, Bucket, "local", "local")
	return store, http.StripPrefix(Prefix, fake.Server()), nil
}
