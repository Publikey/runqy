package monitoring

import "net/http"

// SystemVersionInfo is the response for GET /api/system/version: the build of
// the running runqy server.
type SystemVersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
}

// newSystemVersionHandlerFunc returns a handler that reports the server build
// (set via ldflags, "dev" otherwise). Protected by the dashboard auth
// middleware like the other /api/* routes.
func newSystemVersionHandlerFunc(opts Options) http.HandlerFunc {
	info := SystemVersionInfo{
		Version:   opts.Version,
		Commit:    opts.GitCommit,
		BuildDate: opts.BuildDate,
	}
	if info.Version == "" {
		info.Version = "dev"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, info)
	}
}
