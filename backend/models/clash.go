package models

// Clash is an immutable combat result, stored with its generation and published
// as is. IDs are local to the generation; participant group IDs are never reassigned.
type Clash struct {
	ID           uint               `json:"id" minimum:"1" doc:"Identifies the clash within its generation."`
	Time         float64            `json:"time" minimum:"0" maximum:"1" doc:"Fraction of the turn at which contact happened."`
	Participants []ClashParticipant `json:"participants" nullable:"false"`
}

type ClashParticipant struct {
	GroupID        uint    `json:"group_id" doc:"Soldier group ID; the group may no longer exist."`
	TeamColor      uint    `json:"team_color"`
	X              float64 `json:"x"`
	Y              float64 `json:"y"`
	ClashRadius    float64 `json:"clash_radius" exclusiveMinimum:"0"`
	SoldiersBefore uint    `json:"soldiers_before" doc:"Soldiers immediately before this contact."`
	SoldiersAfter  uint    `json:"soldiers_after" doc:"Soldiers immediately after this contact; 0 means the group was defeated."`
}
