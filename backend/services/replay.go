package services

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"html/template"
)

//go:embed replay.html
var replayHTML string

var replayTemplate = template.Must(template.New("replay").Parse(replayHTML))

// RenderGameReplay embeds saved simulation data in a self-contained spectator
// view. html/template escapes the JSON for script context, including player names.
func RenderGameReplay(replay []byte) (string, error) {
	var output bytes.Buffer
	if err := replayTemplate.Execute(&output, json.RawMessage(replay)); err != nil {
		return "", err
	}
	return output.String(), nil
}
