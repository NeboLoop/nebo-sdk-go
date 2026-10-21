package nebo

import (
	"testing"
)

func TestLoadEnv(t *testing.T) {
	t.Setenv("NEBO_APP_DIR", "/apps/test")
	t.Setenv("NEBO_APP_ID", "com.example.test")
	t.Setenv("NEBO_APP_NAME", "Test App")
	t.Setenv("NEBO_APP_VERSION", "1.2.3")
	t.Setenv("NEBO_DATA_DIR", "/data/test")
	t.Setenv("NEBO_API_URL", "http://127.0.0.1:27895")
	t.Setenv("NEBO_APP_TOKEN", "secret")

	env := loadEnv()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"Dir", env.Dir, "/apps/test"},
		{"ID", env.ID, "com.example.test"},
		{"Name", env.Name, "Test App"},
		{"Version", env.Version, "1.2.3"},
		{"DataDir", env.DataDir, "/data/test"},
		{"APIURL", env.APIURL, "http://127.0.0.1:27895"},
		{"Token", env.Token, "secret"},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestLoadEnvMissing(t *testing.T) {
	for _, key := range []string{"NEBO_APP_DIR", "NEBO_APP_ID", "NEBO_APP_NAME", "NEBO_APP_VERSION", "NEBO_DATA_DIR", "NEBO_API_URL", "NEBO_APP_TOKEN"} {
		t.Setenv(key, "")
	}

	env := loadEnv()

	if env.Token != "" {
		t.Errorf("Token = %q, want empty", env.Token)
	}
	if env.Name != "" {
		t.Errorf("Name = %q, want empty", env.Name)
	}
}
