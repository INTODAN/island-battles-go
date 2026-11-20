package models_test

import (
	"errors"
	"testing"

	"islandBattles/backend/models"
)

func TestNormalizeBotName(t *testing.T) {
	for name, want := range map[string]string{
		"Kraken":                   "Kraken",
		"  Kraken  ":               "Kraken",
		"Deep_Sea-Bot 2":           "Deep_Sea-Bot 2",
		"Ägir":                     "Ägir",
		"abc":                      "abc",
		"abcdefghijklmnopqrstuvwx": "abcdefghijklmnopqrstuvwx",
		"Bot level 2":              "Bot level 2",
	} {
		got, err := models.NormalizeBotName(name)
		if err != nil || got != want {
			t.Errorf("NormalizeBotName(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
	for _, name := range []string{"", "ab", "  ab  ", "abcdefghijklmnopqrstuvwxy", "Kraken!", "Kra.ken", "Kraken\t2", "Bot (level 2)", "bot (level", "\xff\xfe\xfd"} {
		if got, err := models.NormalizeBotName(name); !errors.Is(err, models.ErrInvalidBotName) {
			t.Errorf("NormalizeBotName(%q) = %q, %v; want ErrInvalidBotName", name, got, err)
		}
	}
	if key := models.BotNameKey("Kraken Ägir"); key != "kraken ägir" {
		t.Errorf("BotNameKey = %q", key)
	}
}
