package dal_test

import (
	"sync"
	"testing"

	"islandBattles/backend/dal"
	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
)

// A house bot's competitor is created when first needed, once per level,
// also when two requests need it at once.
func TestFindOrCreateHouseCompetitor(t *testing.T) {
	database := testutils.InitTestDB(t)
	const requests = 4
	ids := make([]string, requests)
	errs := make([]error, requests)
	var group sync.WaitGroup
	for index := range requests {
		group.Go(func() {
			competitor, err := dal.FindOrCreateHouseCompetitor(database, 2)
			ids[index], errs[index] = competitor.ID, err
		})
	}
	group.Wait()
	for index := range requests {
		if errs[index] != nil {
			t.Fatal(errs[index])
		}
		if ids[index] != ids[0] {
			t.Fatalf("house competitors %v, want one", ids)
		}
	}
	house, err := dal.FindOrCreateHouseCompetitor(database, 2)
	if err != nil {
		t.Fatal(err)
	}
	if house.ID != ids[0] || house.Kind != models.HouseCompetitor || house.HouseLevel == nil || *house.HouseLevel != 2 ||
		house.AccountID != nil || house.Name != nil || house.Rating != models.InitialRating {
		t.Fatalf("house competitor = %+v", house)
	}
	other, err := dal.FindOrCreateHouseCompetitor(database, 3)
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == house.ID || *other.HouseLevel != 3 {
		t.Fatalf("level 3 = %+v, want its own competitor", other)
	}
	var count int64
	if err := database.Model(&models.Competitor{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("%d competitors, want 2", count)
	}
}
