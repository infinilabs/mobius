package main

// Transitional aliases (plan 6.4a): configuration lives in internal/config.

import (
	"mobius/internal/config"
)

type (
	Config              = config.Config
	ProjectConfig       = config.ProjectConfig
	PostgresConfig      = config.PostgresConfig
	ElasticsearchConfig = config.ElasticsearchConfig
	VertexModel         = config.VertexModel
	VertexAIConfig      = config.VertexAIConfig
	EventConfig         = config.EventConfig
	BigQueryConfig      = config.BigQueryConfig
	GCSConfig           = config.GCSConfig
	GoogleCloudConfig   = config.GoogleCloudConfig
	UploadConfig        = config.UploadConfig
	SettingsData        = config.SettingsData
	SandboxConfig       = config.SandboxConfig
	SandboxProvider     = config.SandboxProvider
)

const (
	ProviderDocker = config.ProviderDocker
	ProviderNsJail = config.ProviderNsJail
	ProviderNone   = config.ProviderNone
)

// nsjailUsable aliases the shared probe flag so sandbox code and tests keep
// their existing Load/Store call sites.
var nsjailUsable = &config.NsJailUsable

func LoadConfig(path string) (*Config, error) { return config.Load(path) }

func SaveConfig(path string, cfg *Config) error { return config.Save(path, cfg) }
