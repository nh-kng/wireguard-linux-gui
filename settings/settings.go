package settings

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

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

	// Rename only after the complete settings file has been written. A failed
	// preference save must leave the previous on-disk settings intact.
	mode := os.FileMode(0660)
	if info, err := os.Stat(FilePath); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(FilePath), ".wireguard-settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(file.Name(), FilePath); err != nil {
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
