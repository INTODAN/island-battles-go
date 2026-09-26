package models

// ClashEvent is an immutable combat result. IDs are local to its generation;
// participant IDs are persistent IDs, so deleted troop rows do not erase history.
type ClashEvent struct {
	ID           uint               `json:"id" minimum:"0"`
	Time         float64            `json:"time" minimum:"0" maximum:"1"`
	Participants []ClashParticipant `json:"participants" nullable:"false"`
}

type ClashParticipant struct {
	GroupID        uint    `json:"group_id" minimum:"0"`
	TeamColor      uint    `json:"team_color" minimum:"0"`
	X              float64 `json:"x"`
	Y              float64 `json:"y"`
	ClashRadius    float64 `json:"clash_radius" minimum:"0"`
	SoldiersBefore uint    `json:"soldiers_before" minimum:"0"`
	SoldiersAfter  uint    `json:"soldiers_after" minimum:"0"`
}
