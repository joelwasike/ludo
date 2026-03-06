package game

import "github.com/ludo/server/pkg/models"

const (
	MainTrackSize  = 52
	HomeColumnSize = 6
	TokensPerPlayer = 4
)

// PlayerConfig holds board layout constants for each player color.
type PlayerConfig struct {
	Color           models.PlayerColor
	StartPosition   int // Where tokens enter the main track
	HomeEntry       int // Last main track position before home column
	HomeColumnStart int // Logical offset for home column positions
}

var PlayerConfigs = [4]PlayerConfig{
	{Color: models.Red, StartPosition: 0, HomeEntry: 50, HomeColumnStart: 52},
	{Color: models.Green, StartPosition: 13, HomeEntry: 11, HomeColumnStart: 58},
	{Color: models.Yellow, StartPosition: 26, HomeEntry: 24, HomeColumnStart: 64},
	{Color: models.Blue, StartPosition: 39, HomeEntry: 37, HomeColumnStart: 70},
}

// SafeZones are positions on the main track where tokens cannot be captured.
var SafeZones = map[int]bool{
	0: true, 8: true, 13: true, 21: true,
	26: true, 34: true, 39: true, 47: true,
}

// StarPositions are the special star-marked safe positions.
var StarPositions = map[int]bool{
	8: true, 21: true, 34: true, 47: true,
}

// GetPlayerConfig returns the board config for a given player color.
func GetPlayerConfig(color models.PlayerColor) PlayerConfig {
	return PlayerConfigs[color]
}

// GlobalPosition converts a player-relative position (0-51) to the
// absolute main-track position. The relative position counts how many
// steps the token has taken since entering the board.
func GlobalPosition(color models.PlayerColor, relativeSteps int) int {
	config := GetPlayerConfig(color)
	return (config.StartPosition + relativeSteps) % MainTrackSize
}

// RelativePosition converts an absolute main-track position to a
// player-relative step count (how many steps from start).
func RelativePosition(color models.PlayerColor, globalPos int) int {
	config := GetPlayerConfig(color)
	return (globalPos - config.StartPosition + MainTrackSize) % MainTrackSize
}

// IsSafeZone returns true if the given main-track position is a safe zone.
func IsSafeZone(pos int) bool {
	return SafeZones[pos]
}

// MaxMainTrackSteps is the number of steps a token can take on the
// main track before needing to enter the home column (51 steps, since
// positions 0..50 relative = 51 cells, then enter home).
const MaxMainTrackSteps = 51

// CanEnterHome checks if a token at the given relative step count
// would enter or be in the home column after moving diceValue steps.
func CanEnterHome(relativeSteps int, diceValue int) bool {
	return relativeSteps+diceValue > MaxMainTrackSteps
}

// HomeColumnPosition calculates the position within the home column (0-5)
// when a token moves from a relative main-track position with a dice value
// that takes it past the home entry.
// Returns -1 if the move doesn't enter the home column.
func HomeColumnPosition(relativeSteps int, diceValue int) int {
	newRelative := relativeSteps + diceValue
	if newRelative <= MaxMainTrackSteps {
		return -1 // Still on main track
	}
	homePos := newRelative - MaxMainTrackSteps - 1 // 0-indexed home column
	return homePos
}

// IsValidHomePosition checks if a home column position is valid (0-5)
// or if it's the finish position (6 = exactly at center).
func IsValidHomePosition(homePos int) bool {
	return homePos >= 0 && homePos <= HomeColumnSize-1
}

// IsFinishPosition checks if the home column position is the exact
// finish (position 5 is the last home cell; reaching position 6 means finished).
// Actually, home column is 0-5 (6 cells), finishing means reaching past position 5.
// We treat homePos == HomeColumnSize-1 (5) as the last cell.
// To finish, a token at home position X needs exactly (HomeColumnSize - 1 - X + 1) = (HomeColumnSize - X).
// Wait, let's reconsider: home column has indices 0,1,2,3,4,5.
// A token finishes when it moves exactly to position HomeColumnSize (6), meaning
// it has exited the home column entirely. But it's cleaner to say:
// Home column positions: 0-5 (6 cells). Token finishes by landing exactly on position 5
// with no overshoot. Let me reconsider the model:
//
// Model: home column has 6 positions (0-5). When a token at home position p
// rolls diceValue d, it can move to p+d. If p+d == 5, the token finishes (reaches center).
// If p+d < 5, it moves forward in the home column. If p+d > 5, invalid (overshoot).
//
// For tokens entering the home column from the main track:
// relativeSteps + diceValue - MaxMainTrackSteps - 1 gives the home column position.
// So entering with exactly 1 step past the entry = home column position 0.
// Entering with 7 steps past = home column position 6 = INVALID (overshoot).
// The finish condition: homePos == 5 (the 6th and final cell).

// IsExactFinish checks if a move into the home column lands exactly
// on the finish (home position 5).
func IsExactFinish(homePos int) bool {
	return homePos == HomeColumnSize - 1
}

// CanMoveInHomeColumn checks if a token at homePos can move diceValue
// steps within the home column. Returns the new position and whether
// the token finishes.
func CanMoveInHomeColumn(homePos int, diceValue int) (newPos int, finishes bool, valid bool) {
	newPos = homePos + diceValue
	if newPos == HomeColumnSize-1 {
		return newPos, true, true
	}
	if newPos < HomeColumnSize-1 {
		return newPos, false, true
	}
	// Overshoot — invalid move
	return -1, false, false
}

// GetPathOnMainTrack returns the sequence of global positions a token
// passes through when moving from globalFrom by diceValue steps on the
// main track. Used for animation path and collision checking.
func GetPathOnMainTrack(globalFrom int, diceValue int) []int {
	path := make([]int, diceValue)
	for i := 1; i <= diceValue; i++ {
		path[i-1] = (globalFrom + i) % MainTrackSize
	}
	return path
}
