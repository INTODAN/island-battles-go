package models

import (
	"errors"
	"fmt"
)

type UpgradeType string

const SpeedUpgrade UpgradeType = "speed"

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
	SpeedUpgrade: {{Value: 0.5}, {Value: 1, SoldierCost: 10}, {Value: 1.5, SoldierCost: 20}, {Value: 2, SoldierCost: 30}},
}

type Upgrades map[UpgradeType]int

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
