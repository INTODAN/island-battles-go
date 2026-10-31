package handlers

import (
	"context"

	"islandBattles/backend/models"
)

type ListUpgradesResponse struct {
	Body struct {
		Upgrades []models.UpgradeView `json:"upgrades" nullable:"false"`
	}
}

// ListUpgrades lists the static upgrade table. It reads no game, so it needs
// no database and no authentication.
func ListUpgrades(context.Context, *struct{}) (*ListUpgradesResponse, error) {
	response := &ListUpgradesResponse{}
	response.Body.Upgrades = models.UpgradeTable()
	return response, nil
}
