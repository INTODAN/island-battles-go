package models

import (
	"errors"
	"testing"
)

func TestUpgradeLevelsAndSnapshots(t *testing.T) {
	var upgrades Upgrades
	for level, speed := range []float64{.5, 1, 1.5, 2} {
		value, err := upgrades.Value(SpeedUpgrade)
		if err != nil || value != speed {
			t.Fatalf("level %d: value=%v err=%v", level+1, value, err)
		}
		if level == 3 {
			break
		}
		before := upgrades.Clone()
		upgrades, err = upgrades.Upgraded(SpeedUpgrade)
		if err != nil {
			t.Fatal(err)
		}
		if before[SpeedUpgrade] != level+1 {
			t.Fatal("upgrade changed the prior snapshot")
		}
	}
	if _, err := upgrades.Upgraded(SpeedUpgrade); !errors.Is(err, ErrUpgradeMaxLevel) {
		t.Fatalf("max-level upgrade: %v", err)
	}
	clone := upgrades.Clone()
	clone[SpeedUpgrade] = 1
	if upgrades[SpeedUpgrade] != 4 {
		t.Fatal("clone shares storage")
	}
	for _, invalid := range []Upgrades{{SpeedUpgrade: 0}, {SpeedUpgrade: -1}, {SpeedUpgrade: 5}, {"unknown": 1}} {
		if err := invalid.Validate(); !errors.Is(err, ErrInvalidUpgrade) {
			t.Fatalf("accepted invalid upgrades: %v", invalid)
		}
	}
	if _, err := upgrades.Upgraded("unknown"); !errors.Is(err, ErrInvalidUpgrade) {
		t.Fatalf("unknown upgrade: %v", err)
	}
}

func TestUpgradeTypesHaveIndependentLimitsAndEffects(t *testing.T) {
	// A second definition exercises the generic mechanism without adding an
	// unrequested upgrade to the game itself.
	const testUpgrade UpgradeType = "test-upgrade"
	upgradeValues[testUpgrade] = []float64{10, 25}
	t.Cleanup(func() { delete(upgradeValues, testUpgrade) })
	upgrades, err := (Upgrades{SpeedUpgrade: 3}).Upgraded(testUpgrade)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := upgrades.Value(testUpgrade); err != nil || value != 25 {
		t.Fatalf("effect=%v err=%v", value, err)
	}
	if _, err := upgrades.Upgraded(testUpgrade); !errors.Is(err, ErrUpgradeMaxLevel) {
		t.Fatal("type-specific cap ignored")
	}
	if upgraded, err := upgrades.Upgraded(SpeedUpgrade); err != nil || upgraded[SpeedUpgrade] != 4 {
		t.Fatal("other type inherited the wrong cap")
	}
}
