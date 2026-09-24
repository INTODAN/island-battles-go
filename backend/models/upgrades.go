package models

import (
	"errors"
	"fmt"
)

type UpgradeType string

const SpeedUpgrade UpgradeType = "speed"

var (
	ErrInvalidUpgrade  = errors.New("invalid upgrade")
	ErrUpgradeMaxLevel = errors.New("upgrade is already at its maximum level")
)

// Each type defines its own level effects; the number of entries is its maximum
// level. Missing entries in a castle/group's upgrades mean the default level 1.
var upgradeValues = map[UpgradeType][]float64{
	SpeedUpgrade: {0.5, 1, 1.5, 2},
}

type Upgrades map[UpgradeType]int

func (upgrades Upgrades) Level(upgradeType UpgradeType) (int, error) {
	values, exists := upgradeValues[upgradeType]
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
	return upgradeValues[upgradeType][level-1], nil
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
	copy := make(Upgrades, len(upgrades)+len(upgradeValues))
	for upgradeType := range upgradeValues {
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
	if level == len(upgradeValues[upgradeType]) {
		return nil, fmt.Errorf("%w: %s", ErrUpgradeMaxLevel, upgradeType)
	}
	result := upgrades.Clone()
	result[upgradeType] = level + 1
	return result, nil
}
