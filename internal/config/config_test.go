package config

import "testing"

func TestUnsetStorageIsCSV(t *testing.T) {
	t.Setenv("STORAGE", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_MODEL", "")
	t.Setenv("ADDR", "")
	t.Setenv("DATA_DIR", "")
	t.Setenv("MAX_BODY_BYTES", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Storage != "csv" || cfg.Addr != ":8080" || cfg.DataDir != "./private/data" || cfg.LLMEnabled() {
		t.Fatalf("%+v", cfg)
	}
}

func TestDebugOffUnlessSet(t *testing.T) {
	t.Setenv("STORAGE", "")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("DEBUG", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Debug {
		t.Fatal("debug should be off")
	}
	t.Setenv("DEBUG", "1")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Debug {
		t.Fatal("DEBUG=1 should enable debug logs")
	}
}

func TestPostgresRequiresURL(t *testing.T) {
	t.Setenv("STORAGE", "postgres")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("LLM_BASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected startup config error")
	}
}
