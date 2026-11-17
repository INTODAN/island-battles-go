package main

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
)

// Game is a loaded game with what the tools derive from it once: every
// group's provenance and, for every turn after the first, what changed.
//
// Turn n is the game's nth board, frames[n-1], as the replay's "Turn n / m"
// shows it. The board of turn n is what the commands of turn n-1 made: a
// group sent on turn n-1 first appears on turn n, and turn n's clashes
// happened while it was made. Turn n's bot logs are what the bots logged
// while they chose their commands on turn n's board.
type Game struct {
	// Key names the game in the tools' game argument.
	Key string
	// Source says where it came from: a demo file or a lobby code.
	Source   string
	Demo     Demo
	LoadedAt time.Time
	// LobbyCode is set for a game read by lobby code.
	LobbyCode string

	teamNames map[uint]string
	groups    map[uint]groupOrigin
	// changes[n-1] is what changed to make turn n; changes[0] is empty.
	changes []turnChanges
	// costSearch is what is left of the arrival steps that working out
	// upgrade costs may take for the whole game, so a crafted demo can't
	// hold up loading it.
	costSearch int
}

// The bounds of working out an upgrade cost from the soldiers when groups
// arrived at the castle: the costs tried for one castle on one turn, and
// the arrival steps tried for the whole game. A turn's upgrades cost at
// most a few dozen soldiers.
const (
	maxCostTried     = 1000
	maxCostSearch    = 20_000_000
	costSearchFailed = "The upgrade cost wasn't worked out: the soldiers would need a cost of more than %d, or the demo has more to search than this server allows."
)

// groupOrigin is where and when a soldier group was sent.
type groupOrigin struct {
	team        uint
	from, to    uint
	soldiers    uint
	firstTurn   int
	knownCastle bool
}

// turnChanges is what made one turn from the turn before it.
type turnChanges struct {
	sent     []sentGroup
	arrived  []arrivedGroup
	defeated []defeatedGroup
	// ledgers holds every castle that sent, received, upgraded, or changed
	// hands or soldiers in other ways than growing, by castle ID.
	ledgers map[uint]*CastleLedger
}

type sentGroup struct {
	id, team, from, to, soldiers uint
}

type arrivedGroup struct {
	id, team, from, castle, soldiers uint
}

type defeatedGroup struct {
	id, team, soldiers uint
	opponents          []uint
	x, y               float64
	// sameTurn marks a group sent and defeated within one turn.
	sameTurn bool
}

// newGame derives the game's provenance and changes from a checked demo.
func newGame(key, source string, demo Demo) *Game {
	game := &Game{Key: key, Source: source, Demo: demo, LoadedAt: time.Now().UTC(), teamNames: teamNames(demo.Teams), groups: map[uint]groupOrigin{},
		costSearch: maxCostSearch}
	for index, frame := range demo.Frames {
		turn := index + 1
		firstClash := map[uint]islandbattles.ClashParticipant{}
		for _, clash := range frame.Clashes {
			for _, participant := range clash.Participants {
				if _, seen := firstClash[participant.GroupID]; !seen {
					firstClash[participant.GroupID] = participant
				}
			}
		}
		for _, group := range frame.SoldierGroups {
			if _, known := game.groups[group.ID]; known {
				continue
			}
			soldiers := group.Soldiers
			if participant, clashed := firstClash[group.ID]; clashed {
				soldiers = participant.SoldiersBefore
			}
			game.groups[group.ID] = groupOrigin{team: group.TeamColor, from: group.OriginCastleID, to: group.DestinationCastleID, soldiers: soldiers, firstTurn: turn, knownCastle: true}
		}
		// A group that was sent and destroyed within one turn shows only in
		// that turn's clashes.
		for groupID, participant := range firstClash {
			if _, known := game.groups[groupID]; !known {
				game.groups[groupID] = groupOrigin{team: participant.TeamColor, soldiers: participant.SoldiersBefore, firstTurn: turn}
			}
		}
	}
	game.changes = make([]turnChanges, len(demo.Frames))
	for turn := 2; turn <= len(demo.Frames); turn++ {
		game.changes[turn-1] = game.computeChanges(turn)
	}
	return game
}

// teamNames names every team by its lobby name, or "Team <colour>" when the
// name is empty or shared.
func teamNames(teams []DemoTeam) map[uint]string {
	counts := map[string]int{}
	for _, team := range teams {
		counts[strings.ToLower(team.Name)]++
	}
	names := map[uint]string{0: "neutral"}
	for _, team := range teams {
		name := team.Name
		if name == "" || counts[strings.ToLower(name)] > 1 || strings.EqualFold(name, "neutral") {
			name = fmt.Sprintf("Team %d", team.Color)
		}
		names[team.Color] = name
	}
	return names
}

// team names a team colour.
func (game *Game) team(color uint) string {
	if name, found := game.teamNames[color]; found {
		return name
	}
	return fmt.Sprintf("Team %d", color)
}

// resolveTeam finds a team by name, as the tools show it or as the lobby
// named it, or by colour number. "neutral" is colour 0.
func (game *Game) resolveTeam(text string) (uint, error) {
	text = strings.TrimSpace(text)
	for color, name := range game.teamNames {
		if strings.EqualFold(name, text) {
			return color, nil
		}
	}
	for _, team := range game.Demo.Teams {
		if strings.EqualFold(team.Name, text) {
			return team.Color, nil
		}
	}
	if color, err := strconv.ParseUint(text, 10, 32); err == nil {
		if _, found := game.teamNames[uint(color)]; found {
			return uint(color), nil
		}
	}
	known := make([]string, 0, len(game.Demo.Teams))
	for _, team := range game.Demo.Teams {
		known = append(known, fmt.Sprintf("%s (%d)", game.team(team.Color), team.Color))
	}
	return 0, fmt.Errorf("no team %q: the teams are %s, or neutral", text, strings.Join(known, ", "))
}

// Turns is how many turns the game has.
func (game *Game) Turns() int { return len(game.Demo.Frames) }

// frame returns turn n's board.
func (game *Game) frame(turn int) islandbattles.Generation { return game.Demo.Frames[turn-1] }

// checkTurn says why turn is not one of the game's turns.
func (game *Game) checkTurn(turn int) error {
	if turn < 1 || turn > game.Turns() {
		return fmt.Errorf("turn %d is not in this game, which has turns 1 to %d", turn, game.Turns())
	}
	return nil
}

func castlesByID(frame islandbattles.Generation) map[uint]islandbattles.Castle {
	castles := make(map[uint]islandbattles.Castle, len(frame.Castles))
	for _, castle := range frame.Castles {
		castles[castle.ID] = castle
	}
	return castles
}

func groupsByID(frame islandbattles.Generation) map[uint]islandbattles.SoldierGroup {
	groups := make(map[uint]islandbattles.SoldierGroup, len(frame.SoldierGroups))
	for _, group := range frame.SoldierGroups {
		groups[group.ID] = group
	}
	return groups
}

// computeChanges works out what made turn from the turn before it: the
// groups sent, the groups that arrived or were defeated, and each affected
// castle's ledger.
func (game *Game) computeChanges(turn int) turnChanges {
	previous, current := game.frame(turn-1), game.frame(turn)
	previousGroups, currentGroups := groupsByID(previous), groupsByID(current)
	clashes := map[uint][]islandbattles.ClashParticipant{}
	opponents := map[uint][]uint{}
	for _, clash := range current.Clashes {
		for _, participant := range clash.Participants {
			clashes[participant.GroupID] = append(clashes[participant.GroupID], participant)
			for _, other := range clash.Participants {
				if other.TeamColor != participant.TeamColor && !slices.Contains(opponents[participant.GroupID], other.TeamColor) {
					opponents[participant.GroupID] = append(opponents[participant.GroupID], other.TeamColor)
				}
			}
		}
	}
	changes := turnChanges{ledgers: map[uint]*CastleLedger{}}
	for _, group := range current.SoldierGroups {
		if _, existed := previousGroups[group.ID]; !existed {
			origin := game.groups[group.ID]
			changes.sent = append(changes.sent, sentGroup{id: group.ID, team: group.TeamColor, from: group.OriginCastleID, to: group.DestinationCastleID, soldiers: origin.soldiers})
		}
	}
	for groupID, participants := range clashes {
		_, existed := previousGroups[groupID]
		_, exists := currentGroups[groupID]
		last := participants[len(participants)-1]
		if !existed && !exists && last.SoldiersAfter == 0 {
			// Sent and destroyed within the turn: no board shows where from.
			changes.defeated = append(changes.defeated, defeatedGroup{id: groupID, team: last.TeamColor, soldiers: participants[0].SoldiersBefore, opponents: opponents[groupID], x: last.X, y: last.Y, sameTurn: true})
		}
	}
	for _, group := range previous.SoldierGroups {
		if _, exists := currentGroups[group.ID]; exists {
			continue
		}
		soldiers := group.Soldiers
		if participants := clashes[group.ID]; len(participants) > 0 {
			last := participants[len(participants)-1]
			if last.SoldiersAfter == 0 {
				changes.defeated = append(changes.defeated, defeatedGroup{id: group.ID, team: group.TeamColor, soldiers: participants[0].SoldiersBefore, opponents: opponents[group.ID], x: last.X, y: last.Y})
				continue
			}
			soldiers = last.SoldiersAfter
		}
		changes.arrived = append(changes.arrived, arrivedGroup{id: group.ID, team: group.TeamColor, from: group.OriginCastleID, castle: group.DestinationCastleID, soldiers: soldiers})
	}
	slices.SortFunc(changes.sent, func(left, right sentGroup) int { return cmp.Compare(left.id, right.id) })
	slices.SortFunc(changes.arrived, func(left, right arrivedGroup) int { return cmp.Compare(left.id, right.id) })
	slices.SortFunc(changes.defeated, func(left, right defeatedGroup) int { return cmp.Compare(left.id, right.id) })

	previousCastles := castlesByID(previous)
	for _, castle := range current.Castles {
		before, existed := previousCastles[castle.ID]
		if !existed {
			continue
		}
		ledger := game.ledger(turn, before, castle, changes)
		// A castle that only grew has nothing to explain.
		if len(ledger.SentOut) > 0 || len(ledger.Arrivals) > 0 || len(ledger.UpgradesBought) > 0 || ledger.OwnerBefore != ledger.OwnerAfter || ledger.UpgradeCost != nil || !ledger.Reconciled {
			changes.ledgers[castle.ID] = ledger
		}
	}
	return changes
}

// CastleLedger accounts for one castle's soldiers from one turn to the next,
// in the order the game runs a turn: the commands send groups and buy
// upgrades, groups move and clash, every castle a team owns grows (see
// growth), then the groups that reached their castle arrive in group ID
// order.
type CastleLedger struct {
	Turn           int    `json:"turn" jsonschema:"The turn this ledger makes, from the turn before it."`
	CastleID       uint   `json:"castle_id"`
	OwnerBefore    string `json:"owner_before"`
	OwnerAfter     string `json:"owner_after"`
	SoldiersBefore uint   `json:"soldiers_before"`
	SoldiersAfter  uint   `json:"soldiers_after"`
	// SentOut are the groups the castle sent with the turn before's commands.
	SentOut        []LedgerGroup  `json:"sent_out,omitempty" jsonschema:"Groups the castle sent with the previous turn's commands."`
	UpgradesBought map[string]int `json:"upgrades_bought,omitempty" jsonschema:"Upgrade types bought, with the level reached."`
	UpgradeCost    *uint          `json:"upgrade_cost,omitempty" jsonschema:"The soldiers the upgrades cost, worked out from the soldiers."`
	Growth         uint           `json:"growth" jsonschema:"What the castle grew by when castles grew: its production (its generation_amount level, 1 to 4 soldiers), plus a burst that arrived this turn, less what repaid its burst debt; 0 when no team owned it."`
	// GarrisonBeforeArrivals is what defended the castle when the first group
	// arrived.
	GarrisonBeforeArrivals int           `json:"garrison_before_arrivals"`
	Arrivals               []ArrivalStep `json:"arrivals,omitempty"`
	Reconciled             bool          `json:"reconciled" jsonschema:"Whether these steps give exactly soldiers_after and owner_after."`
	Note                   string        `json:"note,omitempty"`
}

// LedgerGroup is a group a castle sent.
type LedgerGroup struct {
	GroupID  uint `json:"group_id"`
	ToCastle uint `json:"to_castle"`
	Soldiers uint `json:"soldiers"`
}

// ArrivalStep is one group reaching a castle, and what it did there.
type ArrivalStep struct {
	GroupID        uint   `json:"group_id"`
	Team           string `json:"team"`
	FromCastle     uint   `json:"from_castle"`
	SentOnTurn     int    `json:"sent_on_turn" jsonschema:"The turn whose commands sent the group, whose bot logs say why; 0 when it was sent before the first turn the demo holds."`
	SoldiersSent   uint   `json:"soldiers_sent"`
	Soldiers       uint   `json:"soldiers" jsonschema:"Soldiers that arrived, after any clashes on the way."`
	GarrisonBefore uint   `json:"garrison_before"`
	Result         string `json:"result" jsonschema:"reinforced (a friendly group), captured (more attackers than defenders) or repelled."`
	GarrisonAfter  uint   `json:"garrison_after"`
	OwnerAfter     string `json:"owner_after"`
}

// growth is what a castle that a team owned grew by from before to after, as
// the game plays it: its production, the generation_amount level after the
// turn's upgrades (level n produces n soldiers), less what repaid its burst
// debt. A castle without debt before and with debt after burst this turn:
// the burst arrived on top of its production, and the debt is the burst. (A
// castle that burst and was captured in the same turn has no debt after it,
// so its ledger doesn't reconcile.)
func growth(before, after islandbattles.Castle) uint {
	production := uint(min(max(after.Upgrades.Level(islandbattles.GenerationAmountUpgrade), 1), 4))
	if before.BurstDebt == 0 && after.BurstDebt > 0 {
		return production + after.BurstDebt
	}
	return production - min(before.BurstDebt, production)
}

// ledger accounts for castle from before, on the turn before, to after. The
// upgrades' cost isn't in a demo, so it is worked out: the smallest cost
// that gives the soldiers and owner shown. Without arrivals that is one
// subtraction; with arrivals the costs are tried in turn, within
// maxCostTried and the game's costSearch.
func (game *Game) ledger(turn int, before, after islandbattles.Castle, changes turnChanges) *CastleLedger {
	ledger := &CastleLedger{Turn: turn, CastleID: after.ID, OwnerBefore: game.team(before.TeamColor), OwnerAfter: game.team(after.TeamColor),
		SoldiersBefore: before.Soldiers, SoldiersAfter: after.Soldiers}
	var sentOut uint
	for _, sent := range changes.sent {
		if sent.from == after.ID {
			ledger.SentOut = append(ledger.SentOut, LedgerGroup{GroupID: sent.id, ToCastle: sent.to, Soldiers: sent.soldiers})
			sentOut += sent.soldiers
		}
	}
	for upgradeType, level := range after.Upgrades {
		if level > before.Upgrades.Level(upgradeType) {
			if ledger.UpgradesBought == nil {
				ledger.UpgradesBought = map[string]int{}
			}
			ledger.UpgradesBought[string(upgradeType)] = level
		}
	}
	if before.TeamColor != 0 {
		ledger.Growth = growth(before, after)
	}
	var arrivals []arrivedGroup
	for _, arrival := range changes.arrived {
		if arrival.castle == after.ID {
			arrivals = append(arrivals, arrival)
		}
	}
	simulate := func(cost int) (int, []ArrivalStep, bool) {
		garrison := int(before.Soldiers) - int(sentOut) - cost + int(ledger.Growth)
		if garrison < 0 {
			return garrison, nil, false
		}
		owner := before.TeamColor
		steps := make([]ArrivalStep, 0, len(arrivals))
		for _, arrival := range arrivals {
			origin := game.groups[arrival.id]
			step := ArrivalStep{GroupID: arrival.id, Team: game.team(arrival.team), FromCastle: arrival.from, SentOnTurn: origin.firstTurn - 1,
				SoldiersSent: origin.soldiers, Soldiers: arrival.soldiers, GarrisonBefore: uint(garrison)}
			switch {
			case arrival.team == owner:
				garrison += int(arrival.soldiers)
				step.Result = "reinforced"
			case int(arrival.soldiers) > garrison:
				owner = arrival.team
				garrison = int(arrival.soldiers) - garrison
				step.Result = "captured"
			default:
				garrison -= int(arrival.soldiers)
				step.Result = "repelled"
			}
			step.GarrisonAfter = uint(garrison)
			step.OwnerAfter = game.team(owner)
			steps = append(steps, step)
		}
		return garrison, steps, garrison == int(after.Soldiers) && owner == after.TeamColor
	}
	garrisonBefore := func(cost int) int {
		return int(before.Soldiers) - int(sentOut) - cost + int(ledger.Growth)
	}
	maxCost := int(before.Soldiers) - int(sentOut) + int(ledger.Growth)
	searched := true
	costs := func(yield func(int) bool) {
		if len(arrivals) == 0 {
			// The garrison only pays the cost, so the cost is what is missing.
			if missing := maxCost - int(after.Soldiers); missing >= 0 && before.TeamColor == after.TeamColor {
				yield(missing)
			}
			return
		}
		for cost := 0; cost <= maxCost; cost++ {
			if cost > maxCostTried || game.costSearch < len(arrivals) {
				searched = false
				return
			}
			game.costSearch -= len(arrivals)
			if !yield(cost) {
				return
			}
		}
	}
	for cost := range costs {
		if _, steps, matches := simulate(cost); matches {
			ledger.Arrivals, ledger.Reconciled, ledger.GarrisonBeforeArrivals = steps, true, garrisonBefore(cost)
			if cost > 0 {
				spent := uint(cost)
				ledger.UpgradeCost = &spent
				if len(ledger.UpgradesBought) == 0 {
					ledger.Note = fmt.Sprintf("%d soldiers left the castle that no board shows: a group sent and destroyed or landed within the same turn.", cost)
					for _, defeated := range changes.defeated {
						if defeated.sameTurn && defeated.team == before.TeamColor && defeated.soldiers == spent {
							ledger.Note = fmt.Sprintf("%d soldiers left the castle that no board shows: most likely group %d, which was sent and defeated within the turn.", cost, defeated.id)
						}
					}
				}
			}
			return ledger
		}
	}
	_, ledger.Arrivals, _ = simulate(0)
	ledger.GarrisonBeforeArrivals = garrisonBefore(0)
	ledger.Note = "The boards don't account for this castle exactly: a group that was sent and arrived within one turn shows on no board."
	if !searched {
		ledger.Note = fmt.Sprintf(costSearchFailed, maxCostTried)
	}
	return ledger
}

// TeamTotals is a team's forces on one turn.
type TeamTotals struct {
	Team           string `json:"team"`
	Castles        int    `json:"castles"`
	CastleSoldiers uint   `json:"castle_soldiers"`
	Groups         int    `json:"groups"`
	GroupSoldiers  uint   `json:"group_soldiers"`
}

// totals sums every team's forces on turn, in team order, with neutral
// castles last when there are any.
func (game *Game) totals(turn int) []TeamTotals {
	frame := game.frame(turn)
	byTeam := map[uint]*TeamTotals{}
	order := make([]uint, 0, len(game.Demo.Teams)+1)
	for _, team := range game.Demo.Teams {
		byTeam[team.Color] = &TeamTotals{Team: game.team(team.Color)}
		order = append(order, team.Color)
	}
	get := func(color uint) *TeamTotals {
		if totals, found := byTeam[color]; found {
			return totals
		}
		byTeam[color] = &TeamTotals{Team: game.team(color)}
		order = append(order, color)
		return byTeam[color]
	}
	for _, castle := range frame.Castles {
		totals := get(castle.TeamColor)
		totals.Castles++
		totals.CastleSoldiers += castle.Soldiers
	}
	for _, group := range frame.SoldierGroups {
		totals := get(group.TeamColor)
		totals.Groups++
		totals.GroupSoldiers += group.Soldiers
	}
	result := make([]TeamTotals, 0, len(order))
	for _, color := range order {
		result = append(result, *byTeam[color])
	}
	return result
}

// capture is a castle that changed hands.
type capture struct {
	turn          int
	castle        uint
	from, to      uint
	soldiersAfter uint
	// capturedBy is the group whose arrival took the castle, if the boards
	// show it.
	capturedBy *uint
}

// captures lists every castle that changed hands in turns from to to, in
// turn and castle order.
func (game *Game) captures(from, to int) []capture {
	var result []capture
	for turn := max(from, 2); turn <= to; turn++ {
		previous := castlesByID(game.frame(turn - 1))
		for _, castle := range game.frame(turn).Castles {
			before, existed := previous[castle.ID]
			if !existed || before.TeamColor == castle.TeamColor {
				continue
			}
			found := capture{turn: turn, castle: castle.ID, from: before.TeamColor, to: castle.TeamColor, soldiersAfter: castle.Soldiers}
			if ledger := game.changes[turn-1].ledgers[castle.ID]; ledger != nil {
				for _, step := range ledger.Arrivals {
					if step.Result == "captured" {
						found.capturedBy = &step.GroupID
					}
				}
			}
			result = append(result, found)
		}
	}
	return result
}

// distance is the straight-line distance between two points, rounded to two
// decimals.
func distance(x1, y1, x2, y2 float64) float64 {
	return round2(math.Hypot(x2-x1, y2-y1))
}

func round2(value float64) float64 { return math.Round(value*100) / 100 }

// logLines lists a team's players' lines on turn, as "player: level logger:
// message", at most limit of them, and how many it left out.
func (game *Game) teamLogLines(turn int, team uint, limit int) ([]string, int) {
	if turn < 1 || turn > game.Turns() {
		return nil, 0
	}
	var lines []string
	omitted := 0
	for _, player := range game.Demo.Logs[turn-1].Players {
		if player.TeamColor != team {
			continue
		}
		for _, line := range player.Lines {
			if len(lines) >= limit {
				omitted++
				continue
			}
			lines = append(lines, player.DisplayName+": "+formatLine(line))
		}
	}
	return lines, omitted
}

// logsHidden says whether the copy leaves out team's lines on turn.
func (game *Game) logsHidden(turn int, team uint) bool {
	return turn >= 1 && turn <= game.Turns() && slices.Contains(game.Demo.Logs[turn-1].HiddenTeamColors, team)
}

// hiddenTeams names the teams whose lines the copy leaves out on any turn
// from from to to.
func (game *Game) hiddenTeams(from, to int) []string {
	var colors []uint
	for turn := max(from, 1); turn <= min(to, game.Turns()); turn++ {
		for _, color := range game.Demo.Logs[turn-1].HiddenTeamColors {
			if !slices.Contains(colors, color) {
				colors = append(colors, color)
			}
		}
	}
	slices.Sort(colors)
	names := make([]string, len(colors))
	for index, color := range colors {
		names[index] = game.team(color)
	}
	return names
}

// hiddenLogsReason says why the copy leaves some teams' lines out. Other
// teams' logs stay hidden until the game ends, since bots often log their
// plans.
func (game *Game) hiddenLogsReason() string {
	if game.LobbyCode != "" {
		return "the game is still running, and a watcher sees no bot logs until it has ended; call load_lobby again once it has ended to read every team's"
	}
	return "this demo was saved before the game ended, so it holds only its saver's team's logs; a demo saved after the end holds every team's"
}

// logsSummary says whose bot logs the copy holds.
func (game *Game) logsSummary() string {
	hidden := game.hiddenTeams(1, game.Turns())
	switch {
	case len(hidden) == 0:
		return "Every team's bot logs."
	case len(hidden) == len(game.Demo.Teams):
		return "No bot logs: " + game.hiddenLogsReason() + "."
	}
	return "No bot logs of " + strings.Join(hidden, ", ") + ": " + game.hiddenLogsReason() + "."
}

// formatLine writes a log line compactly, as "level logger: message".
func formatLine(line islandbattles.LogLine) string {
	return fmt.Sprintf("%s %s: %s", line.Level, line.Logger, line.Message)
}
