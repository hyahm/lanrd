package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefault(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "not-exist.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0:7080", c.Server.Addr)
	assert.Equal(t, 30, c.Capture.FPS)
	assert.Equal(t, 64, c.Capture.Tile)
	assert.Equal(t, 80, c.Capture.Quality)
	assert.False(t, c.Input.BlockLocal)
}

func TestLoadYAMLAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := "server:\n  addr: 0.0.0.0:9000\n  token: secret\ncapture:\n  fps: 30\n  tile: 128\n  quality: 85\ninput:\n  blockLocal: false\n"
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))

	c, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "0.0.0.0:9000", c.Server.Addr)
	assert.Equal(t, "secret", c.Server.Token)
	assert.Equal(t, 30, c.Capture.FPS)
	assert.Equal(t, 128, c.Capture.Tile)
	assert.Equal(t, 85, c.Capture.Quality)

	t.Setenv("LANRD_TOKEN", "envtok")
	t.Setenv("LANRD_FPS", "999")
	t.Setenv("LANRD_BLOCK_LOCAL", "true")
	c, err = Load(path)
	require.NoError(t, err)
	assert.Equal(t, "envtok", c.Server.Token)
	assert.Equal(t, 60, c.Capture.FPS) // 被 Normalize 收敛
	assert.True(t, c.Input.BlockLocal)
}

func TestNormalize(t *testing.T) {
	c := Default()
	c.Capture.FPS = 1
	c.Capture.Quality = 5
	c.Capture.Tile = 33
	c.Normalize()
	assert.Equal(t, 5, c.Capture.FPS)
	assert.Equal(t, 30, c.Capture.Quality)
	assert.Equal(t, 64, c.Capture.Tile)
}
