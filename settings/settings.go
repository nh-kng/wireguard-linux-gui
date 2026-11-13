package settings

import (
	"encoding/json"
	"io/ioutil"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/ungerik/go-dry"
)

const FilePath = "./wireguard-linux-gui.settings"

const legacyFilePath = "./wireguird.settings"

type Settings struct {
	MultipleTunnels          bool
	StartOnTray              bool
	CheckUpdates             bool
	Debug                    bool
	RememberConnectedServers bool
	ConnectedServers         map[string]int64
	CountryFlags             bool
	Passwordless             bool
}

func (s *Settings) Init() error {
	log.Debug().Msg("Settings init")

	if err := s.Load(); err != nil {
		return err
	}

	if s.Debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	return nil
}

func (s *Settings) Save() error {
	log.Debug().Msg("saving settings")
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(FilePath, data, 0660); err != nil {
		return err
	}

	log.Debug().Msg("saved settings")
	return nil
}

func (s *Settings) Load() error {
	log.Debug().Msg("loading settings")
	s.MultipleTunnels = true
	s.RememberConnectedServers = true
	path := FilePath
	if !dry.FileExists(path) {
		path = legacyFilePath
	}
	if !dry.FileExists(path) {
		log.Debug().Msg("settings file doesnt exist")
		return nil
	}

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	settings := &Settings{MultipleTunnels: true, RememberConnectedServers: true}
	if err := json.Unmarshal(data, &settings); err != nil {
		return err
	}

	*s = *settings

	log.Debug().Interface("settings", s).Msg("loaded settings")
	return nil
}
