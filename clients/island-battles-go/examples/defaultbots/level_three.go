package defaultbots

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
)

// LevelThree plays like LevelTwo, and also:
//
//   - keeps enough soldiers in each castle to hold it against the groups
//     heading for it, and against what the strongest enemy castle could send;
//   - sends help to a castle that would fall even keeping every soldier;
//   - combines attacks: castles farther from the target send first and nearer
//     ones later, so the groups land together. Its memory keeps these plans,
//     and a turn counter, between turns;
//   - buys speed upgrades with the soldiers it can spare.
func LevelThree(_ context.Context, turn islandbattles.Turn) (islandbattles.Result, error) {
	state := levelThreeTurn{board: newBoard(turn), budgets: map[uint]*castleBudget{}, speeds: map[uint]float64{}, planned: planned{}}
	if err := turn.Memory.Decode(&state.memory); err != nil {
		// A memory it can't read is forgotten, rather than passing turns.
		state.memory = levelThreeMemory{}
	}
	state.turn = state.memory.Turn + 1
	owned := state.board.owned()
	if len(owned) == 0 {
		return islandbattles.Result{Commands: []islandbattles.Command{}, Logs: []islandbattles.LogLine{logLine(3, "I have no castles left; passing.")}}, nil
	}
	for _, castle := range owned {
		state.speeds[castle.ID] = state.board.speed(castle.Upgrades)
	}
	state.reserve(owned)
	state.launchPlans()
	state.defend(owned)
	state.attack(owned)
	state.upgrade(owned)
	return state.result()
}

// levelThreeMemory is LevelThree's memory.
type levelThreeMemory struct {
	// Turn counts the turns the bot has played in this game.
	Turn  int          `json:"turn"`
	Plans []attackPlan `json:"plans,omitempty"`
}

// attackPlan is a combined attack whose groups land on Target at the end of
// turn Lands. Launches are the groups that are still to be sent.
type attackPlan struct {
	Target   uint     `json:"target"`
	Lands    int      `json:"lands"`
	Launches []launch `json:"launches"`
}

// launch sends Soldiers from Origin at turn At.
type launch struct {
	Origin   uint `json:"origin"`
	Soldiers uint `json:"soldiers"`
	At       int  `json:"at"`
}

// Tuning of LevelThree.
const (
	// maxSpeedLevel is the highest speed level the bot buys.
	maxSpeedLevel = 3
	// upgradeSlack is how many spare soldiers a castle keeps after paying for
	// an upgrade.
	upgradeSlack = 10
	// upgradeFromTurn is the first turn the bot buys upgrades in, after it has
	// had time to take the castles near it.
	upgradeFromTurn = 15
)

// levelThreeTurn is the state of one LevelThree turn.
type levelThreeTurn struct {
	board  board
	turn   int
	memory levelThreeMemory
	// budgets are the bot's castles by ID, with what each must keep.
	budgets  map[uint]*castleBudget
	speeds   map[uint]float64
	planned  planned
	upgrades []islandbattles.Command
	sends    []islandbattles.Command
	actions  []string
	notes    []string
}

// castleBudget is what one of the bot's castles must keep this turn.
type castleBudget struct {
	castle islandbattles.Castle
	// hold is what it keeps to hold against the groups heading for it.
	hold int
	// guard is what it keeps against the enemy castle guardAgainst, which
	// could send the most to it. An attack on that castle may use it.
	guard        int
	guardAgainst *uint
	// committed is what its plans have yet to send, and used what this
	// turn's commands take.
	committed int
	used      int
	// doomed is set for a castle that falls even keeping every soldier. It
	// fights with all of them.
	doomed bool
}

// spare is how many soldiers the castle can send to target, or spend on
// anything else for a nil target.
func (budget *castleBudget) spare(target *islandbattles.Castle) int {
	if budget.doomed {
		return 0
	}
	keep := max(budget.hold, budget.guard)
	if target != nil && budget.guardAgainst != nil && *budget.guardAgainst == target.ID {
		keep = budget.hold
	}
	return int(budget.castle.Soldiers) - budget.used - budget.committed - keep
}

// reserve sets what each castle must keep: enough to hold against the groups
// heading for it, a guard against the enemy castle that could send it the
// most, and the launches its plans have yet to send.
func (state *levelThreeTurn) reserve(owned []islandbattles.Castle) {
	for _, castle := range owned {
		budget := &castleBudget{castle: castle, hold: int(garrisonToHold(state.board.arrivals(castle), state.board.team))}
		budget.guard, budget.guardAgainst = state.guard(castle)
		state.budgets[castle.ID] = budget
	}
	for _, plan := range state.memory.Plans {
		for _, next := range plan.Launches {
			if budget, found := state.budgets[next.Origin]; found {
				budget.committed += int(next.Soldiers)
			}
		}
	}
}

// guard is how many soldiers a castle keeps so that no single enemy castle
// could take it by sending everything it has, and the enemy castle that
// needs the most: with the castle's growth while the enemy group travels,
// and the margin an attacker adds, a garrison this large needs more soldiers
// than the enemy castle holds.
func (state *levelThreeTurn) guard(castle islandbattles.Castle) (int, *uint) {
	guard := 0
	var against *uint
	for _, enemy := range state.board.castles {
		if enemy.TeamColor == islandbattles.NeutralTeamColor || enemy.TeamColor == state.board.team {
			continue
		}
		turns := travelTurns(enemy.Distance(castle), state.board.speed(enemy.Upgrades))
		if needed := int(enemy.Soldiers) - turns - int(attackMargin(enemy.Soldiers)) - 1; needed > guard {
			guard, against = needed, &enemy.ID
		}
	}
	return guard, against
}

// send adds a send command, if the castle still has the soldiers.
func (state *levelThreeTurn) send(origin, target islandbattles.Castle, soldiers uint) bool {
	budget := state.budgets[origin.ID]
	if soldiers == 0 || budget.used+int(soldiers) > int(origin.Soldiers) {
		return false
	}
	budget.used += int(soldiers)
	state.sends = append(state.sends, islandbattles.SendSoldierGroup(origin.ID, target.ID, soldiers))
	state.planned[target.ID] = append(state.planned[target.ID], arrival{
		turn: travelTurns(origin.Distance(target), state.speeds[origin.ID]), id: state.board.nextGroupID() + uint(len(state.sends)),
		team: state.board.team, soldiers: soldiers,
	})
	return true
}

// launchPlans sends the launches of the remembered plans that are due, and
// drops a plan whose target the bot owns or whose origin can't send any more.
// The launches still to come count as arrivals for this turn's forecasts.
func (state *levelThreeTurn) launchPlans() {
	kept := []attackPlan{}
	for _, plan := range state.memory.Plans {
		target, found := state.board.castle(plan.Target)
		if !found || target.TeamColor == state.board.team {
			continue
		}
		waiting := []launch{}
		failed := false
		for _, next := range plan.Launches {
			budget, owned := state.budgets[next.Origin]
			if !owned {
				failed = true
				continue
			}
			if next.At > state.turn {
				waiting = append(waiting, next)
				continue
			}
			budget.committed -= int(next.Soldiers)
			if !state.send(budget.castle, target, next.Soldiers) {
				failed = true
			}
		}
		if failed {
			state.notes = append(state.notes, fmt.Sprintf("dropped the attack on castle %d", plan.Target))
			for _, next := range waiting {
				state.budgets[next.Origin].committed -= int(next.Soldiers)
			}
			continue
		}
		if len(waiting) > 0 {
			plan.Launches = waiting
			kept = append(kept, plan)
			for _, next := range waiting {
				state.planned[plan.Target] = append(state.planned[plan.Target], arrival{turn: plan.Lands - state.turn + 1, id: ^uint(0), team: state.board.team, soldiers: next.Soldiers})
			}
		}
	}
	state.memory.Plans = kept
}

// defend sends help to every castle that the groups heading for it would
// take even if it kept all its soldiers, from the nearest castles whose help
// lands in time.
func (state *levelThreeTurn) defend(owned []islandbattles.Castle) {
	for _, castle := range owned {
		budget := state.budgets[castle.ID]
		arrivals := append(state.board.arrivals(castle), state.planned[castle.ID]...)
		available := castle.Soldiers - uint(budget.used)
		missing := int(garrisonToHold(arrivals, state.board.team)) - int(available)
		if missing <= 0 {
			continue
		}
		falls := fallTurn(castle, arrivals, available, state.board.team)
		helped := uint(0)
		for _, helper := range byDistance(castle, owned) {
			spare := state.budgets[helper.ID].spare(nil)
			if helper.ID == castle.ID || spare <= 0 || travelTurns(helper.Distance(castle), state.speeds[helper.ID]) > falls {
				continue
			}
			soldiers := uint(min(spare, missing+1-int(helped)))
			if state.send(helper, castle, soldiers) {
				helped += soldiers
			}
			if int(helped) > missing {
				break
			}
		}
		if helped > 0 {
			state.actions = append(state.actions, fmt.Sprintf("sending %d soldiers to help castle %d", helped, castle.ID))
		} else {
			state.notes = append(state.notes, fmt.Sprintf("castle %d is threatened and no help can land in time", castle.ID))
		}
		if int(helped) <= missing {
			budget.doomed = true
		}
	}
}

// fallTurn is the first turn whose arrivals take a castle that keeps
// soldiers, or a turn past every arrival when it holds.
func fallTurn(castle islandbattles.Castle, arrivals []arrival, soldiers, team uint) int {
	castle.Soldiers = soldiers
	last := 0
	for _, landing := range arrivals {
		last = max(last, landing.turn)
	}
	for turn := 1; turn <= last; turn++ {
		if owner, _ := forecast(castle, arrivals, turn); owner != team {
			return turn
		}
	}
	return last + 1
}

// combined is an attack on target from several castles, whose groups land
// together after turns.
type combined struct {
	target  islandbattles.Castle
	origins []islandbattles.Castle
	need    uint
	turns   int
	score   float64
}

// attack plans the best attacks the spare soldiers allow, one at a time.
func (state *levelThreeTurn) attack(owned []islandbattles.Castle) {
	for range len(state.board.castles) {
		best, found := state.bestAttack(owned)
		if !found {
			return
		}
		state.launch(best)
	}
}

func (state *levelThreeTurn) bestAttack(owned []islandbattles.Castle) (combined, bool) {
	var best combined
	found := false
	for _, target := range state.board.others() {
		origins := []islandbattles.Castle{}
		for _, origin := range owned {
			if state.budgets[origin.ID].spare(&target) > 0 {
				origins = append(origins, origin)
			}
		}
		slices.SortStableFunc(origins, func(left, right islandbattles.Castle) int {
			return cmp.Or(cmp.Compare(state.travel(left, target), state.travel(right, target)), cmp.Compare(left.ID, right.ID))
		})
		total := 0
		for count, origin := range origins {
			total += state.budgets[origin.ID].spare(&target)
			turns := state.travel(origin, target)
			owner, defenders := forecast(target, append(state.board.arrivals(target), state.planned[target.ID]...), turns)
			if owner == state.board.team {
				break
			}
			// Enemy groups on their way from the target to the attacking
			// castles meet the attack.
			oncoming := uint(0)
			for _, group := range state.board.groups {
				if group.TeamColor != state.board.team && group.OriginCastleID == target.ID && slices.ContainsFunc(origins[:count+1], func(castle islandbattles.Castle) bool { return castle.ID == group.DestinationCastleID }) {
					oncoming += group.Soldiers
				}
			}
			need := defenders + 1 + attackMargin(defenders) + oncoming
			if total < int(need) {
				continue
			}
			value := 1.0
			if target.TeamColor != islandbattles.NeutralTeamColor {
				value = 1.5
			}
			score := value / float64(int(need)+3*turns)
			if !found || score > best.score {
				best, found = combined{target: target, origins: slices.Clone(origins[:count+1]), need: need, turns: turns, score: score}, true
			}
			break
		}
	}
	return best, found
}

func (state *levelThreeTurn) travel(origin, target islandbattles.Castle) int {
	return travelTurns(origin.Distance(target), state.speeds[origin.ID])
}

// launch sends a combined attack's groups from the castles whose groups need
// all the turns now, and remembers the others to send later, so that every
// group lands together.
func (state *levelThreeTurn) launch(attack combined) {
	remaining := attack.need
	plan := attackPlan{Target: attack.target.ID, Lands: state.turn + attack.turns - 1}
	// The farthest castles give first, so the nearest keep their soldiers
	// home longest.
	for index := len(attack.origins) - 1; index >= 0 && remaining > 0; index-- {
		origin := attack.origins[index]
		soldiers := min(uint(state.budgets[origin.ID].spare(&attack.target)), remaining)
		remaining -= soldiers
		wait := attack.turns - state.travel(origin, attack.target)
		if wait == 0 {
			state.send(origin, attack.target, soldiers)
			continue
		}
		state.budgets[origin.ID].committed += int(soldiers)
		plan.Launches = append(plan.Launches, launch{Origin: origin.ID, Soldiers: soldiers, At: state.turn + wait})
		state.planned[attack.target.ID] = append(state.planned[attack.target.ID], arrival{turn: attack.turns, id: ^uint(0), team: state.board.team, soldiers: soldiers})
	}
	if len(plan.Launches) > 0 {
		state.memory.Plans = append(state.memory.Plans, plan)
	}
	from := []string{}
	for _, origin := range attack.origins {
		from = append(from, fmt.Sprint(origin.ID))
	}
	state.actions = append(state.actions, fmt.Sprintf("attacking %s castle %d with %d soldiers from castles %s, landing in %d turns",
		describeOwner(attack.target, state.board.team), attack.target.ID, attack.need, strings.Join(from, ", "), attack.turns))
}

// upgrade buys the next speed level for castles that can spare its cost.
func (state *levelThreeTurn) upgrade(owned []islandbattles.Castle) {
	if state.turn < upgradeFromTurn {
		return
	}
	for _, castle := range owned {
		level := castle.Upgrades.Level(islandbattles.SpeedUpgrade)
		if level < 1 || level >= maxSpeedLevel {
			continue
		}
		cost, found := islandbattles.NextUpgradeCost(state.board.turn, castle, islandbattles.SpeedUpgrade)
		if !found {
			continue
		}
		budget := state.budgets[castle.ID]
		if budget.spare(nil) < int(cost)+upgradeSlack {
			continue
		}
		budget.used += int(cost)
		state.upgrades = append(state.upgrades, islandbattles.UpgradeCastle(castle.ID, islandbattles.SpeedUpgrade))
		state.actions = append(state.actions, fmt.Sprintf("buying speed level %d at castle %d", level+1, castle.ID))
	}
}

func (state *levelThreeTurn) result() (islandbattles.Result, error) {
	state.memory.Turn = state.turn
	memory, err := islandbattles.NewMemory(state.memory)
	if err != nil {
		return islandbattles.Result{}, err
	}
	// Sends go first: an upgrade bought in the same turn would change the
	// speed of groups that were planned with the castle's current speed.
	commands := append(slices.Clone(state.sends), state.upgrades...)
	if commands == nil {
		commands = []islandbattles.Command{}
	}
	logs := []islandbattles.LogLine{}
	if len(state.actions) == 0 {
		logs = append(logs, logLine(3, "Turn %d: holding; nothing worth attacking with the soldiers I can spare.", state.turn))
	} else {
		logs = append(logs, logLine(3, "Turn %d: %s.", state.turn, strings.Join(state.actions, "; ")))
	}
	if len(state.notes) > 0 || len(state.memory.Plans) > 0 {
		notes := slices.Clone(state.notes)
		for _, plan := range state.memory.Plans {
			notes = append(notes, fmt.Sprintf("%d more groups to send to castle %d", len(plan.Launches), plan.Target))
		}
		logs = append(logs, logLine(3, "Plans: %s.", strings.Join(notes, "; ")))
	}
	return islandbattles.Result{Commands: commands, Logs: logs, Memory: memory}, nil
}
