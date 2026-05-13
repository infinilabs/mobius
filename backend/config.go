package main

import (
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

type PostgresConfig struct {
	Host     string `yaml:"host" json:"host"`
	Port     int    `yaml:"port" json:"port"`
	User     string `yaml:"user" json:"user"`
	Password string `yaml:"password" json:"password"`
	DBName   string `yaml:"dbname" json:"dbname"`
}

type ElasticsearchConfig struct {
	URL string `yaml:"url" json:"url"`
}

type VertexAIConfig struct {
	LLMModelID    string `yaml:"llm_model_id" json:"llm_model_id"`
	LLMLocation   string `yaml:"llm_location" json:"llm_location"`
	ImgModelID    string `yaml:"img_model_id" json:"img_model_id"`
	ImgLocation   string `yaml:"img_location" json:"img_location"`
	VideoModelID  string `yaml:"video_model_id" json:"video_model_id"`
	VideoLocation string `yaml:"video_location" json:"video_location"`
}

type BigQueryConfig struct {
	Dataset string `yaml:"dataset" json:"dataset"`
}

type GoogleCloudConfig struct {
	ProjectID       string         `yaml:"project_id" json:"project_id"`
	CredentialsPath string         `yaml:"credentials_path" json:"credentials_path"`
	BigQuery        BigQueryConfig `yaml:"bigquery" json:"bigquery"`
	VertexAI        VertexAIConfig `yaml:"vertex_ai" json:"vertex_ai"`
}

type Config struct {
	mu sync.RWMutex `yaml:"-"`

	Server struct {
		Port          int    `yaml:"port" json:"port"`
		Mode          string `yaml:"mode" json:"mode"`
		LogMaxSizeMB  int    `yaml:"log_max_size_mb" json:"log_max_size_mb"`
		LogMaxBackups int    `yaml:"log_max_backups" json:"log_max_backups"`
		LogMaxAgeDays int    `yaml:"log_max_age_days" json:"log_max_age_days"`
	} `yaml:"server" json:"server"`

	Postgres      PostgresConfig      `yaml:"postgres" json:"postgres"`
	Elasticsearch ElasticsearchConfig `yaml:"elasticsearch" json:"elasticsearch"`
	GoogleCloud   GoogleCloudConfig   `yaml:"google_cloud" json:"google_cloud"`
}

type SettingsData struct {
	Postgres      PostgresConfig      `json:"postgres"`
	Elasticsearch ElasticsearchConfig `json:"elasticsearch"`
	GoogleCloud   GoogleCloudConfig   `json:"google_cloud"`
}

func (c *Config) GetSettings() SettingsData {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return SettingsData{
		Postgres:      c.Postgres,
		Elasticsearch: c.Elasticsearch,
		GoogleCloud:   c.GoogleCloud,
	}
}

func (c *Config) ApplySettings(s SettingsData) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Postgres = s.Postgres
	c.Elasticsearch = s.Elasticsearch
	c.GoogleCloud = s.GoogleCloud
}

func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func SaveConfig(path string, cfg *Config) error {
	cfg.mu.RLock()
	defer cfg.mu.RUnlock()

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
