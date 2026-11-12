package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"islandBattles/backend/bots"
	"islandBattles/backend/models"
	"islandBattles/lobby/domain"
)

// BotNotChosenProblemType is the lobby's game_requirement problem type for a
// member who tries to get ready, or is on the roster of a start, before
// choosing a bot.
const BotNotChosenProblemType = "urn:island-battles:problem:bot-not-chosen"

// botNotChosenMessage tells the player what to do; the lobby shows it.
const botNotChosenMessage = "Choose your bot before getting ready: your localhost bot or a default bot level."

// MemberSettings are Island Battles' member settings in the lobby, which
// each person sets for themselves with the lobby's setLobbyMemberSettings:
// the bot that plays for them. The game copies each roster player's bot when
// the lobby starts.
type MemberSettings struct {
	Bot *models.BotChoiceView `json:"bot"`
}

// MemberBot reads a person's member settings, {"bot": {"kind": "localhost"}}
// or {"bot": {"kind": "default", "level": N}} with a level that
// GET /bots/defaults lists, and returns the bot. Its errors are written for
// the person who sets the settings.
func MemberBot(settings json.RawMessage) (models.BotChoiceView, error) {
	var decoded MemberSettings
	decoder := json.NewDecoder(bytes.NewReader(settings))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil || decoded.Bot == nil {
		return models.BotChoiceView{}, errors.New(`member_settings must be {"bot": {"kind": "localhost"}} or {"bot": {"kind": "default", "level": N}} with a level that GET /bots/defaults lists`)
	}
	if err := ValidateBotChoice(*decoded.Bot); err != nil {
		return models.BotChoiceView{}, err
	}
	return *decoded.Bot, nil
}

// ValidateMemberSettings is Island Battles' member settings validator for the
// lobby: the settings name the person's bot, as MemberBot reads it.
func ValidateMemberSettings(settings json.RawMessage) error {
	_, err := MemberBot(settings)
	return err
}

// ValidateBotChoice accepts the localhost bot without a level, or a default
// bot with a level that bots.Defaults lists.
func ValidateBotChoice(choice models.BotChoiceView) error {
	switch choice.Kind {
	case models.LocalhostBot:
		if choice.Level != nil {
			return reject(422, "level is only for kind default")
		}
		return nil
	case models.DefaultBot:
		levels := []string{}
		for _, bot := range bots.Defaults() {
			if choice.Level != nil && *choice.Level == bot.Level {
				return nil
			}
			levels = append(levels, fmt.Sprint(bot.Level))
		}
		return reject(422, "kind default needs a level that GET /bots/defaults lists: "+strings.Join(levels, ", "))
	default:
		return reject(422, "kind must be localhost or default")
	}
}

// BotReadyCheck is the lobby's ready check for Island Battles: a member may
// get ready, and a roster start, only once their member settings name a bot.
// It reads only the members' settings, which the lobby passes.
func BotReadyCheck(members []domain.ReadyMember, atStart bool) error {
	var missing []string
	for _, member := range members {
		if _, err := MemberBot(member.Settings); err != nil {
			missing = append(missing, member.DisplayName)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if atStart {
		return domain.GameRequirement(BotNotChosenProblemType, botNotChosenAtStartMessage(missing))
	}
	return domain.GameRequirement(BotNotChosenProblemType, botNotChosenMessage)
}

// botNotChosenAtStartMessage names the members that the host is waiting for.
func botNotChosenAtStartMessage(names []string) string {
	return "Waiting for " + strings.Join(names, ", ") + " to choose a bot: their localhost bot or a default bot level."
}

// lobbyBotLevels returns the default bot levels that the backend plays for
// the roster, by player ID, from the start's snapshot: each bot seat's level,
// and the level in each player's member settings. A player who chose their
// localhost bot is absent. The start requires a bot of every roster player,
// so a player is absent otherwise only when a retried start no longer finds
// them among the members, because they left since; their own client plays.
func lobbyBotLevels(snapshot domain.Snapshot) (map[string]uint, error) {
	members := map[string]domain.Member{}
	for _, member := range snapshot.Members {
		members[member.PlayerID] = member
	}
	levels := map[string]uint{}
	for _, entry := range snapshot.StartRoster {
		member, found := members[entry.PlayerID]
		if !found {
			continue
		}
		if member.IsSeat() {
			level, err := BotSeatLevel(member.SeatSettings)
			if err != nil {
				return nil, fmt.Errorf("bot seat %s: %w", member.ID, err)
			}
			levels[entry.PlayerID] = level
			continue
		}
		bot, err := MemberBot(member.MemberSettings)
		if err == nil && bot.Kind == models.DefaultBot && bot.Level != nil {
			levels[entry.PlayerID] = *bot.Level
		}
	}
	return levels, nil
}

// botSeatSettings are the seat_settings of an Island Battles bot seat.
type botSeatSettings struct {
	BotLevel *uint `json:"bot_level"`
}

// BotSeatName is the display name of a bot seat of a level.
func BotSeatName(level uint) string { return fmt.Sprintf("Bot (level %d)", level) }

// BotSeatLevel reads a bot seat's seat_settings, {"bot_level": N}, and
// returns the level, which must be one that GET /bots/defaults lists. Its
// errors are written for the host who adds the seat.
func BotSeatLevel(settings json.RawMessage) (uint, error) {
	var levels []string
	for _, bot := range bots.Defaults() {
		levels = append(levels, fmt.Sprint(bot.Level))
	}
	invalid := fmt.Errorf(`seat_settings must be {"bot_level": N} with a level that GET /bots/defaults lists: %s`, strings.Join(levels, ", "))
	var decoded botSeatSettings
	decoder := json.NewDecoder(bytes.NewReader(settings))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil || decoded.BotLevel == nil {
		return 0, invalid
	}
	if _, found := bots.ForLevel(*decoded.BotLevel); !found {
		return 0, invalid
	}
	return *decoded.BotLevel, nil
}

// ValidateBotSeat is Island Battles' seat validator for the lobby: a seat is
// a default bot of the level in its settings, which the backend plays, and
// is named after the level, such as "Bot (level 2)".
func ValidateBotSeat(settings json.RawMessage) (string, error) {
	level, err := BotSeatLevel(settings)
	if err != nil {
		return "", err
	}
	return BotSeatName(level), nil
}
