package models

// ClashEvent is an immutable combat result. IDs are local to its generation;
// participant IDs are logical IDs, so deleted troop rows do not erase history.
type ClashEvent struct {
	ID           uint               `json:"id"`
	Time         float64            `json:"time" minimum:"0" maximum:"1"`
	Participants []ClashParticipant `json:"participants"`
}

type ClashParticipant struct {
	GroupID        uint    `json:"group_id"`
	TeamColor      uint    `json:"team_color"`
	X              float64 `json:"x"`
	Y              float64 `json:"y"`
	ClashRadius    float64 `json:"clash_radius"`
	SoldiersBefore uint    `json:"soldiers_before"`
	SoldiersAfter  uint    `json:"soldiers_after"`
}
