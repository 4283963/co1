package main

import (
	"log"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type Drone struct {
	ID      string  `json:"id"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Z       float64 `json:"z"`
	Battery float64 `json:"battery"`
	TargetX float64 `json:"-"`
	TargetZ float64 `json:"-"`
	Speed   float64 `json:"-"`
}

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	drones    []*Drone
	dronesMu  sync.RWMutex
	clients   = make(map[*websocket.Conn]bool)
	clientsMu sync.Mutex
	broadcast = make(chan []*Drone)
)

func initDrones() {
	drones = make([]*Drone, 5)
	for i := 0; i < 5; i++ {
		drones[i] = &Drone{
			ID:      "DRONE-" + string(rune('A'+i)),
			X:       rand.Float64()*80 - 40,
			Y:       10 + rand.Float64()*10,
			Z:       rand.Float64()*80 - 40,
			Battery: 70 + rand.Float64()*30,
			TargetX: rand.Float64()*80 - 40,
			TargetZ: rand.Float64()*80 - 40,
			Speed:   0.3 + rand.Float64()*0.5,
		}
	}
}

func updateDrones() {
	dronesMu.Lock()
	defer dronesMu.Unlock()

	for _, d := range drones {
		dx := d.TargetX - d.X
		dz := d.TargetZ - d.Z
		dist := math.Sqrt(dx*dx + dz*dz)

		if dist < 1 {
			d.TargetX = rand.Float64()*80 - 40
			d.TargetZ = rand.Float64()*80 - 40
		} else {
			d.X += (dx / dist) * d.Speed
			d.Z += (dz / dist) * d.Speed
		}

		d.Battery -= 0.02 + rand.Float64()*0.03
		if d.Battery < 0 {
			d.Battery = 0
		}

		d.Y = 10 + math.Sin(float64(time.Now().UnixNano())/1e9+float64(d.ID[0]))*2
	}
}

func broadcastDrones() {
	dronesMu.RLock()
	data := make([]*Drone, len(drones))
	copy(data, drones)
	dronesMu.RUnlock()

	broadcast <- data
}

func handleConnections(c *gin.Context) {
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer ws.Close()

	clientsMu.Lock()
	clients[ws] = true
	clientsMu.Unlock()

	dronesMu.RLock()
	initial := make([]*Drone, len(drones))
	copy(initial, drones)
	dronesMu.RUnlock()

	ws.WriteJSON(initial)

	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			clientsMu.Lock()
			delete(clients, ws)
			clientsMu.Unlock()
			break
		}
	}
}

func handleMessages() {
	for {
		data := <-broadcast
		clientsMu.Lock()
		for client := range clients {
			err := client.WriteJSON(data)
			if err != nil {
				client.Close()
				delete(clients, client)
			}
		}
		clientsMu.Unlock()
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())
	initDrones()

	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowAllOrigins: true,
		AllowMethods:    []string{"GET", "POST", "PUT", "DELETE"},
		AllowHeaders:    []string{"Origin", "Content-Type", "Accept"},
	}))

	r.GET("/ws", handleConnections)

	go handleMessages()

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			updateDrones()
			broadcastDrones()
		}
	}()

	log.Println("Server starting on :8080")
	r.Run(":8080")
}
