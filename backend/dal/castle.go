package dal

import (
	"fmt"
	"islandBattles/backend/models"
	"log"

	"gorm.io/gorm"
)

type CastlePairDistance struct{
	Castle1ID int
	Castle2ID int
	Distance float64
}

func distancesQuery(db gorm.DB){
	var results []CastlePairDistance

	// We use db.Table() to create the "c1" alias for the main table
	err := db.Table("castles AS c1").
	    Select("c1.id AS castle1_id, c2.id AS castle2_id, c1.position <-> c2.position AS distance").
	    Joins("JOIN castles AS c2 ON c1.id < c2.id"). // The self-join
	    Scan(&results).Error
	
	if err != nil {
		log.Fatalln("Failed to query the castle distances")
	}
	
	for _,r := range results {
		fmt.Printf("Castle %d to %d: %f\n", r.Castle1ID, r.Castle2ID, r.Distance)
	}
}

func CreateCastle(db *gorm.DB, castle *models.Castle) (uint, error) {
	err := db.Create(&castle).Error
	if err != nil {
		return 0, err
	}
	return castle.ID, nil
}