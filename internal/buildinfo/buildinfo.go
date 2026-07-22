package buildinfo

// Values are overridden by the container build using -ldflags.
var (
	version = "dev"
	commit  = "unknown"
)

type Metadata struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

type Provider interface {
	Metadata() Metadata
}

type Static Metadata

func (s Static) Metadata() Metadata {
	return Metadata(s)
}

func Current() Provider {
	return Static{Version: version, Commit: commit}
}
