package nebo

import "os"

// AppEnv provides typed access to the environment Nebo sets when it launches
// your app. Nebo clears the environment first, so these are the only NEBO_*
// variables your app sees.
type AppEnv struct {
	Dir     string // NEBO_APP_DIR — the app's (versioned) code directory
	ID      string // NEBO_APP_ID — app ID from manifest
	Name    string // NEBO_APP_NAME — app name from manifest
	Version string // NEBO_APP_VERSION — app version from manifest
	DataDir string // NEBO_DATA_DIR — the app's persistent data directory; also the working directory
	APIURL  string // NEBO_API_URL — Nebo's local API, http://127.0.0.1:<port>
	Token   string // NEBO_APP_TOKEN — per-launch secret; Nebo sends it on every gRPC call, send it to NEBO_API_URL as "Authorization: Bearer <token>"
}

func loadEnv() *AppEnv {
	return &AppEnv{
		Dir:     os.Getenv("NEBO_APP_DIR"),
		ID:      os.Getenv("NEBO_APP_ID"),
		Name:    os.Getenv("NEBO_APP_NAME"),
		Version: os.Getenv("NEBO_APP_VERSION"),
		DataDir: os.Getenv("NEBO_DATA_DIR"),
		APIURL:  os.Getenv("NEBO_API_URL"),
		Token:   os.Getenv("NEBO_APP_TOKEN"),
	}
}
