package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Os            string
	DNS           string `json:"dns"` // todo
	EncryptionKey string `json:"encryption_key"`
	SocksPort     string `json:"socks_port"`
	ConferenceID  string `json:"conference_id"`
}

func (p *Program) getConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		log("WARNING: Could not get system config directory: %v", err)
		return "config.json"
	}
	configDir := filepath.Join(dir, "olcrtc")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		log("WARNING: Could not create config directory: %v", err)
	}
	return filepath.Join(configDir, "config.json")

}

func (p *Program) loadConfig() *Config {
	configPath := p.getConfigPath()
	log("Loading config from: %s", configPath)
	// default values
	cfg := &Config{
		DNS:           "1.1.1.1",
		EncryptionKey: "",
		SocksPort:     "1080",
		ConferenceID:  "",
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			log("Config file not found. Using default configuration.")
		} else {
			log("WARNING: Could not read config file: %v", err)
		}
		return cfg
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		log("WARNING: Could not parse config file: %v", err)
		return cfg
	}
	cfg.ConferenceID = strings.ReplaceAll(cfg.ConferenceID, " ", "")
	log("Config loaded successfully")
	return cfg
}

func (p *Program) saveConfig(dns, encryptionKey, socksPort, conferenceID string) {
	log("Saving configuration...")

	conferenceID = strings.ReplaceAll(conferenceID, " ", "")

	p.Config = &Config{
		DNS:           dns,
		EncryptionKey: encryptionKey,
		SocksPort:     socksPort,
		ConferenceID:  conferenceID,
	}

	configPath := p.getConfigPath()
	data, err := json.MarshalIndent(p.Config, "", "  ")
	if err != nil {
		log("ERROR: Could not marshal config: %v", err)
		p.showError(err)
		return
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		log("ERROR: Could not write config file: %v", err)
		p.showError(err)
		return
	}

	log("Configuration saved to: %s", configPath)
}
