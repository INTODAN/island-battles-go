package models

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/danielgtaylor/huma/v2"
)

type UpgradeType string

const (
	SpeedUpgrade                UpgradeType = "speed"
	ClashStopGenerationsUpgrade UpgradeType = "clash_stop_generations"
)

var (
	ErrInvalidUpgrade       = errors.New("invalid upgrade")
	ErrUpgradeMaxLevel      = errors.New("upgrade is already at its maximum level")
	ErrInsufficientSoldiers = errors.New("not enough soldiers to upgrade")
)

type upgradeLevel struct {
	Value       float64
	SoldierCost uint
}

// Each type defines its own level effects and costs; the number of entries is its maximum
// level. Missing entries in a castle/group's upgrades mean the default level 1.
var upgradeLevels = map[UpgradeType][]upgradeLevel{
	SpeedUpgrade:                {{Value: 0.5}, {Value: 1, SoldierCost: 10}, {Value: 1.5, SoldierCost: 20}, {Value: 2, SoldierCost: 30}},
	ClashStopGenerationsUpgrade: {{Value: 3}, {Value: 2, SoldierCost: 10}, {Value: 1, SoldierCost: 20}},
}

// upgradeDescriptions says what each type's level values measure.
var upgradeDescriptions = map[UpgradeType]string{
	SpeedUpgrade:                "World units a troop moves per turn",
	ClashStopGenerationsUpgrade: "Generations a troop stays stopped after a clash",
}

// UpgradeTypes lists every upgrade type in a stable order.
func UpgradeTypes() []UpgradeType {
	return slices.Sorted(maps.Keys(upgradeLevels))
}

// Schema publishes UpgradeType as one named string enum, so generated clients
// share a single type for upgrade_type fields and upgrade map keys.
func (UpgradeType) Schema(registry huma.Registry) *huma.Schema {
	const name = "UpgradeType"
	if _, exists := registry.Map()[name]; !exists {
		values := make([]any, 0, len(upgradeLevels))
		for _, upgradeType := range UpgradeTypes() {
			values = append(values, string(upgradeType))
		}
		registry.Map()[name] = &huma.Schema{Type: huma.TypeString, Enum: values, Description: "A castle upgrade. GET /game/upgrades lists each type's levels and costs."}
	}
	return &huma.Schema{Ref: "#/components/schemas/" + name}
}

// UpgradeLevelView is one level of an upgrade type.
type UpgradeLevelView struct {
	Level       int     `json:"level" minimum:"1"`
	Value       float64 `json:"value" doc:"The level's effect, in the unit its type's description names."`
	SoldierCost uint    `json:"soldier_cost" doc:"Soldiers the castle pays to reach this level from the level below. Level 1 is free."`
}

// UpgradeView is one upgrade type with every level a castle can reach.
type UpgradeView struct {
	Type        UpgradeType        `json:"type"`
	Description string             `json:"description" doc:"What the levels' values measure."`
	MaxLevel    int                `json:"max_level" minimum:"1" doc:"The highest level. Every castle starts at level 1."`
	Levels      []UpgradeLevelView `json:"levels" nullable:"false" doc:"Every level from 1 to max_level, in order."`
}

// UpgradeTable lists every upgrade type, sorted by type, with each level's
// effect and cost.
func UpgradeTable() []UpgradeView {
	table := make([]UpgradeView, 0, len(upgradeLevels))
	for _, upgradeType := range UpgradeTypes() {
		levels := make([]UpgradeLevelView, 0, len(upgradeLevels[upgradeType]))
		for index, level := range upgradeLevels[upgradeType] {
			levels = append(levels, UpgradeLevelView{Level: index + 1, Value: level.Value, SoldierCost: level.SoldierCost})
		}
		table = append(table, UpgradeView{Type: upgradeType, Description: upgradeDescriptions[upgradeType], MaxLevel: len(levels), Levels: levels})
	}
	return table
}

// Upgrades maps upgrade types to levels.
type Upgrades map[UpgradeType]int

// Schema publishes the map's keys as UpgradeType.
func (Upgrades) Schema(registry huma.Registry) *huma.Schema {
	return &huma.Schema{Type: huma.TypeObject, AdditionalProperties: &huma.Schema{Type: huma.TypeInteger, Format: "int64"},
		Extensions: map[string]any{"propertyNames": UpgradeType("").Schema(registry)}}
}

func (upgrades Upgrades) Level(upgradeType UpgradeType) (int, error) {
	values, exists := upgradeLevels[upgradeType]
	if !exists {
		return 0, fmt.Errorf("%w: unknown type %q", ErrInvalidUpgrade, upgradeType)
	}
	level, exists := upgrades[upgradeType]
	if !exists {
		level = 1
	}
	if level < 1 || level > len(values) {
		return 0, fmt.Errorf("%w: %s level must be between 1 and %d", ErrInvalidUpgrade, upgradeType, len(values))
	}
	return level, nil
}

func (upgrades Upgrades) Value(upgradeType UpgradeType) (float64, error) {
	level, err := upgrades.Level(upgradeType)
	if err != nil {
		return 0, err
	}
	return upgradeLevels[upgradeType][level-1].Value, nil
}

// Cost is the fixed soldier cost of reaching this level, not the next level.
// The default level has no purchase cost.
func (upgrades Upgrades) Cost(upgradeType UpgradeType) (uint, error) {
	level, err := upgrades.Level(upgradeType)
	if err != nil {
		return 0, err
	}
	return upgradeLevels[upgradeType][level-1].SoldierCost, nil
}

func (upgrades Upgrades) Validate() error {
	for upgradeType := range upgrades {
		if _, err := upgrades.Level(upgradeType); err != nil {
			return err
		}
	}
	return nil
}

// Clone snapshots every current level without sharing the original map.
func (upgrades Upgrades) Clone() Upgrades {
	copy := make(Upgrades, len(upgrades)+len(upgradeLevels))
	for upgradeType := range upgradeLevels {
		copy[upgradeType] = 1
	}
	for upgradeType, level := range upgrades {
		copy[upgradeType] = level
	}
	return copy
}

// Upgraded returns a new snapshot with one type advanced by exactly one level.
func (upgrades Upgrades) Upgraded(upgradeType UpgradeType) (Upgrades, error) {
	if err := upgrades.Validate(); err != nil {
		return nil, err
	}
	level, err := upgrades.Level(upgradeType)
	if err != nil {
		return nil, err
	}
	if level == len(upgradeLevels[upgradeType]) {
		return nil, fmt.Errorf("%w: %s", ErrUpgradeMaxLevel, upgradeType)
	}
	result := upgrades.Clone()
	result[upgradeType] = level + 1
	return result, nil
}
