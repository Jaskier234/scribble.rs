package game

import (
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	uuid "github.com/satori/go.uuid"
	"golang.org/x/text/cases"
)

// Lobby represents a game session.
// FIXME Field visibilities should be changed in case we ever serialize this.
type Lobby struct {
	// ID uniquely identified the Lobby.
	ID string

	// DrawingTime is the amount of seconds that each player has available to
	// finish their drawing.
	DrawingTime int
	// MaxRounds defines how many iterations a lobby does before the game ends.
	// One iteration means every participant does one drawing.
	MaxRounds int
	// MaxPlayers defines the maximum amount of players in a single lobby.
	MaxPlayers int
	// CustomWords are additional words that will be used in addition to the
	// predefined words.
	CustomWords []string
	words       []string
	public      bool

	// players references all participants of the Lobby.
	players []*Player

	// whether the game has started, is ongoing or already over.
	state gameState
	// drawer references the Player that is currently drawing.
	drawer *Player
	// owner references the Player that created the lobby.
	owner *Player
	// CurrentWord represents the word that was last selected. If no word has
	// been selected yet or the round is already over, this should be empty.
	CurrentWord string
	// wordHints for the current word.
	wordHints []*WordHint
	// wordHintsShown are the same as wordHints with characters visible.
	wordHintsShown []*WordHint
	// Round is the round that the Lobby is currently in. This is a number
	// between 0 and MaxRounds. 0 indicates that it hasn't started yet.
	Round int
	// wordChoice represents the current choice of words present to the drawer.
	wordChoice []string
	Wordpack   string
	// RoundEndTime represents the time at which the current round will end.
	// This is a UTC unix-timestamp in milliseconds.
	RoundEndTime int64

	timeLeftTicker        *time.Ticker
	scoreEarnedByGuessers int
	CustomWordsChance     int
	ClientsPerIPLimit     int
	// currentDrawing represents the state of the current canvas. The elements
	// consist of LineEvent and FillEvent. Please do not modify the contents
	// of this array an only move AppendLine and AppendFill on the respective
	// lobby object.
	currentDrawing []interface{}
	EnableVotekick bool

	lowercaser cases.Caser
}

type gameState string

const (
	unstarted gameState = "unstarted"
	ongoing   gameState = "ongoing"
	gameOver  gameState = "gameOver"
)

// WordHint describes a character of the word that is to be guessed, whether
// the character should be shown and whether it should be underlined on the
// UI.
type WordHint struct {
	Character rune `json:"character"`
	Underline bool `json:"underline"`
}

// Line is the struct that a client send when drawing
type Line struct {
	FromX     float32 `json:"fromX"`
	FromY     float32 `json:"fromY"`
	ToX       float32 `json:"toX"`
	ToY       float32 `json:"toY"`
	Color     string  `json:"color"`
	LineWidth float32 `json:"lineWidth"`
}

// Fill represents the usage of the fill bucket.
type Fill struct {
	X     float32 `json:"x"`
	Y     float32 `json:"y"`
	Color string  `json:"color"`
}

// Player represents a participant in a Lobby.
type Player struct {
	// userSession uniquely identifies the player.
	userSession      string
	ws               *websocket.Conn
	socketMutex      *sync.Mutex
	lastKnownAddress string

	votedForKick map[string]bool

	// ID uniquely identified the Player.
	ID string `json:"id"`
	// Name is the players displayed name
	Name string `json:"name"`
	// Score is the points that the player got in the current Lobby.
	Score int `json:"score"`
	// Connected defines whether the players websocket connection is currently
	// established. This has previously been in state but has been moved out
	// in order to avoid losing the state on refreshing the page.
	// While checking the websocket against nil would be enough, we still need
	// this field for sending it via the APIs.
	Connected bool `json:"connected"`
	// Rank is the current ranking of the player in his Lobby
	LastScore int         `json:"lastScore"`
	Rank      int         `json:"rank"`
	State     PlayerState `json:"state"`
}

// GetLastKnownAddress returns the last known IP-Address used for an HTTP request.
func (player *Player) GetLastKnownAddress() string {
	return player.lastKnownAddress
}

// SetLastKnownAddress sets the last known IP-Address used for an HTTP request.
// Can be retrieved via GetLastKnownAddress().
func (player *Player) SetLastKnownAddress(address string) {
	player.lastKnownAddress = address
}

// GetWebsocket simply returns the players websocket connection. This method
// exists to encapsulate the websocket field and prevent accidental sending
// the websocket data via the network.
func (player *Player) GetWebsocket() *websocket.Conn {
	return player.ws
}

// SetWebsocket sets the given connection as the players websocket connection.
func (player *Player) SetWebsocket(socket *websocket.Conn) {
	player.ws = socket
}

// GetWebsocketMutex returns a mutex for locking the websocket connection.
// Since gorilla websockets shits it self when two calls happen at
// the same time, we need a mutex per player, since each player has their
// own socket. This getter extends to prevent accidentally sending the mutex
// via the network.
func (player *Player) GetWebsocketMutex() *sync.Mutex {
	return player.socketMutex
}

// GetUserSession returns the players current user session.
func (player *Player) GetUserSession() string {
	return player.userSession
}

type PlayerState string

const (
	Guessing PlayerState = "guessing"
	Drawing  PlayerState = "drawing"
	Standby  PlayerState = "standby"
)

// GetPlayer searches for a player, identifying them by usersession.
func (lobby *Lobby) GetPlayer(userSession string) *Player {
	for _, player := range lobby.players {
		if player.userSession == userSession {
			return player
		}
	}

	return nil
}

func (lobby *Lobby) ClearDrawing() {
	lobby.currentDrawing = make([]interface{}, 0, 0)
}

// AppendLine adds a line direction to the current drawing. This exists in order
// to prevent adding arbitrary elements to the drawing, as the backing array is
// an empty interface type.
func (lobby *Lobby) AppendLine(line *LineEvent) {
	lobby.currentDrawing = append(lobby.currentDrawing, line)
}

// AppendFill adds a fill direction to the current drawing. This exists in order
// to prevent adding arbitrary elements to the drawing, as the backing array is
// an empty interface type.
func (lobby *Lobby) AppendFill(fill *FillEvent) {
	lobby.currentDrawing = append(lobby.currentDrawing, fill)
}

// GetLobby returns a Lobby that has a matching ID or no Lobby if none could
// be found.
func GetLobby(id string) *Lobby {
	for _, l := range lobbies {
		if l.ID == id {
			return l
		}
	}

	return nil
}

// GetActiveLobbyCount indicates how many activate lobby there are. This includes
// both private and public lobbies and it doesn't matter whether the game is
// already over, hasn't even started or is still ongoing.
func GetActiveLobbyCount() int {
	return len(lobbies)
}

// RemoveLobby deletes a lobby, not allowing anyone to connect to it again.
func RemoveLobby(id string) {
	indexToDelete := -1
	for index, l := range lobbies {
		if l.ID == id {
			indexToDelete = index
			break
		}
	}

	if indexToDelete != -1 {
		lobby := lobbies[indexToDelete]
		lobbies = append(lobbies[:indexToDelete], lobbies[indexToDelete+1:]...)
		log.Printf("Closing lobby %s. There are currently %d open lobbies left.\n", lobby.ID, len(lobbies))
	}
}

func createPlayer(name string) *Player {
	return &Player{
		Name:         name,
		ID:           uuid.NewV4().String(),
		userSession:  uuid.NewV4().String(),
		Score:        0,
		LastScore:    0,
		Rank:         1,
		votedForKick: make(map[string]bool),
		socketMutex:  &sync.Mutex{},
		State:        Guessing,
		Connected:    false,
	}
}

func createLobby(
	drawingTime int,
	rounds int,
	maxPlayers int,
	customWords []string,
	customWordsChance int,
	clientsPerIPLimit int,
	enableVotekick bool) *Lobby {

	createDeleteMutex.Lock()

	lobby := &Lobby{
		ID:                uuid.NewV4().String(),
		DrawingTime:       drawingTime,
		MaxRounds:         rounds,
		MaxPlayers:        maxPlayers,
		CustomWords:       customWords,
		CustomWordsChance: customWordsChance,
		ClientsPerIPLimit: clientsPerIPLimit,
		EnableVotekick:    enableVotekick,
		currentDrawing:    make([]interface{}, 0, 0),
		state:             unstarted,
	}

	if len(customWords) > 1 {
		rand.Shuffle(len(lobby.CustomWords), func(i, j int) {
			lobby.CustomWords[i], lobby.CustomWords[j] = lobby.CustomWords[j], lobby.CustomWords[i]
		})
	}

	lobbies = append(lobbies, lobby)

	createDeleteMutex.Unlock()

	return lobby
}

// JSEvent contains an eventtype and optionally any data.
type JSEvent struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

func (lobby *Lobby) HasConnectedPlayers() bool {
	for _, otherPlayer := range lobby.players {
		if otherPlayer.Connected {
			return true
		}
	}

	return false
}

func (lobby *Lobby) IsPublic() bool {
	return lobby.public
}

func (lobby *Lobby) GetPlayers() []*Player {
	return lobby.players
}

func GetPublicLobbies() []*Lobby {
	var publicLobbies []*Lobby
	for _, lobby := range lobbies {
		if lobby.IsPublic() {
			publicLobbies = append(publicLobbies, lobby)
		}
	}

	return publicLobbies
}
