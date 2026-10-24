package models

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// LobbySettings are the Island Battles settings a host chooses when creating a
// lobby. The lobby hands them back in the started snapshot. There are none
// yet, so only an empty object is valid.
type LobbySettings struct{}

// ParseLobbySettings decodes a lobby's game settings and rejects unknown fields.
func ParseLobbySettings(raw json.RawMessage) (LobbySettings, error) {
	var settings LobbySettings
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return LobbySettings{}, fmt.Errorf("invalid Island Battles settings: %w", err)
	}
	return settings, nil
}
