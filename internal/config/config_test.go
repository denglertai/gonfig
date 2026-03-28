package config

import "testing"

func TestLoadAppConfig_DefaultPluginPath(t *testing.T) {
	v := SetupViper()
	cfg := LoadAppConfig(v)

	if cfg.PluginPath != "./plugins" {
		t.Fatalf("expected default plugin path %q, got %q", "./plugins", cfg.PluginPath)
	}
}

func TestLoadAppConfig_PluginPathFromEnv(t *testing.T) {
	t.Setenv("GONFIG_PLUGIN_PATH", "/tmp/custom-plugins")

	v := SetupViper()
	cfg := LoadAppConfig(v)

	if cfg.PluginPath != "/tmp/custom-plugins" {
		t.Fatalf("expected plugin path from env %q, got %q", "/tmp/custom-plugins", cfg.PluginPath)
	}
}
