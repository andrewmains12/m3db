	"github.com/m3db/m3db/storage/namespace"
	Open(namespace ts.ID, blockSize time.Duration, shard uint32, start time.Time) error
	Open(md namespace.Metadata) error
	Open(md namespace.Metadata) error