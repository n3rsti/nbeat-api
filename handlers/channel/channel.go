package channel

import (
	"log"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var (
	// Define a structure to hold all connections and a mutex for safe access
	clientRegistry = struct {
		m     sync.Mutex
		conns map[string][]*websocket.Conn
	}{conns: make(map[string][]*websocket.Conn)}
)

type Handler struct{}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func AddConnection(conn *websocket.Conn, channelId string) {
	clientRegistry.m.Lock()
	if _, exists := clientRegistry.conns[channelId]; exists {
		log.Println("Alr exists")
		clientRegistry.conns[channelId] = append(clientRegistry.conns[channelId], conn)
	} else {
		clientRegistry.conns[channelId] = []*websocket.Conn{conn}
		log.Println("Added")
	}
	clientRegistry.m.Unlock()
}

func RemoveConnection(conn *websocket.Conn, channelId string) {
	clientRegistry.m.Lock()
	for i, c := range clientRegistry.conns[channelId] {
		if c == conn {
			clientRegistry.conns[channelId] = append(clientRegistry.conns[channelId][:i], clientRegistry.conns[channelId][i+1:]...)
			break
		}
	}
	clientRegistry.m.Unlock()
}

func (h *Handler) Channel(c *gin.Context) {
	w := c.Writer
	r := c.Request

	channelId := c.Param("id")

	log.Println(channelId)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("Error during connection upgradation:", err)
		return
	}
	defer conn.Close()

	AddConnection(conn, channelId)

	defer RemoveConnection(conn, channelId)

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("Error during message reading:", err)
			break
		}
		log.Printf("Received: %s", message)

		broadcastMessage(messageType, message, channelId)
		log.Println(clientRegistry.conns)
	}
}

func broadcastMessage(messageType int, message []byte, channelId string) {
	clientRegistry.m.Lock()
	defer clientRegistry.m.Unlock()
	for _, conn := range clientRegistry.conns[channelId] {
		if err := conn.WriteMessage(messageType, message); err != nil {
			log.Println("Error during message broadcasting:", err)
		}
	}
}
