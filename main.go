package main

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Player represents a connected player
type Player struct {
	ID   string  `json:"id"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Dead bool    `json:"dead"`
	Conn *websocket.Conn
}

type Projectile struct {
	ID     string  `json:"id"`
	Owner  string  `json:"owner"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	DX     float64 `json:"dx"`
	DY     float64 `json:"dy"`
	Speed  float64 `json:"speed"`
	Expiry time.Time
}

type Move struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type Shoot struct {
	DX float64 `json:"dx"`
	DY float64 `json:"dy"`
}

type ClientAction struct {
	Move  Move  `json:"move"`
	Shoot Shoot `json:"shoot"`
}

// GameState manages all players
type GameState struct {
	Players     map[string]*Player
	Projectiles map[string]*Projectile
	Mutex       sync.Mutex
}

// Global game state
var gameState = GameState{
	Players:     make(map[string]*Player),
	Projectiles: make(map[string]*Projectile),
}

// WebSocket Upgrader
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handles new WebSocket connections
func handleConn(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Error upgrading connection:", err)
		return
	}
	defer conn.Close()

	playerID := uuid.New().String()

	player := &Player{
		Conn: conn,
		ID:   playerID,
		X:    50,
		Y:    50,
	}

	// Add player to game state
	gameState.Mutex.Lock()
	gameState.Players[player.ID] = player
	gameState.Mutex.Unlock()

	log.Println("Player connected:", player.ID)

	// Send the player's ID to them
	err = conn.WriteJSON(map[string]string{"id": playerID})
	if err != nil {
		log.Println("Error sending player ID:", err)
		delete(gameState.Players, player.ID)
		return
	}

	// Listen for movement updates
	for {
		var msg ClientAction
		err := conn.ReadJSON(&msg)
		if err != nil {
			log.Println("Error reading JSON:", err)
			break
		}

		gameState.Mutex.Lock()
		if move := msg.Move; move.X != 0 || move.Y != 0 {
			player.X = float64(move.X)
			player.Y = float64(move.Y)
		}
		if shoot := msg.Shoot; shoot.DX != 0 || shoot.DY != 0 {
			projectileID := uuid.New().String()
			projectile := &Projectile{
				ID:     projectileID,
				Owner:  player.ID,
				X:      player.X,
				Y:      player.Y,
				DX:     float64(shoot.DX),
				DY:     float64(shoot.DY),
				Speed:  15,
				Expiry: time.Now().Add(5 * time.Second),
			}
			gameState.Projectiles[projectileID] = projectile
		}
		gameState.Mutex.Unlock()
	}

	// Remove player on disconnect
	gameState.Mutex.Lock()
	delete(gameState.Players, player.ID)
	gameState.Mutex.Unlock()
	log.Println("Player disconnected:", player.ID)
}

// Broadcast game state to all players
func broadcastState() {
	for {
		time.Sleep(16 * time.Millisecond)

		gameState.Mutex.Lock()
		state := struct {
			Players     map[string]Player     `json:"players"`
			Projectiles map[string]Projectile `json:"projectiles"`
		}{
			Players:     make(map[string]Player),
			Projectiles: make(map[string]Projectile),
		}

		for id, player := range gameState.Players {
			state.Players[id] = *player
		}

		for id, projectile := range gameState.Projectiles {
			state.Projectiles[id] = *projectile
		}

		stillAlive := 0
		for _, player := range gameState.Players {
			if !player.Dead {
				stillAlive++
			}
		}

		gameState.Mutex.Unlock()

		// Send the state to all players
		for _, player := range gameState.Players {
			err := player.Conn.WriteJSON(state)
			if err != nil {
				delete(gameState.Players, player.ID)
				log.Println("Error broadcasting state:", err)
			}
		}

		if stillAlive == 1 {
			gameState.Mutex.Lock()
			time.Sleep(3 * time.Second)
			for _, player := range gameState.Players {
				player.Dead = false
				player.X = 50
				player.Y = 50
			}
			gameState.Mutex.Unlock()
		}
	}
}

func updateProjectiles() {
	for {
		time.Sleep(16 * time.Millisecond) // ~60 FPS

		gameState.Mutex.Lock()
		now := time.Now()

		for id, proj := range gameState.Projectiles {
			if now.After(proj.Expiry) {
				delete(gameState.Projectiles, id) // Remove expired projectiles
				continue
			}
			// Move projectile
			proj.X += proj.DX * proj.Speed
			proj.Y += proj.DY * proj.Speed
			for _, player := range gameState.Players {
				if player.ID == proj.Owner {
					continue
				}
				if player.X-40 < proj.X && proj.X < player.X+40 && player.Y-40 < proj.Y && proj.Y < player.Y+40 {
					delete(gameState.Projectiles, id)
					player.Dead = true
					break
				}
			}
		}

		gameState.Mutex.Unlock()
	}
}

func main() {
	http.HandleFunc("/ws", handleConn)

	go broadcastState()
	go updateProjectiles()

	log.Println("Game server running on ws://localhost:8080/ws")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("Server error:", err)
	}
}

// ipconfig getifaddr en0
