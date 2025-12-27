package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed client.html
var clientHTML embed.FS

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Config struct {
	Port            int
	TriangleKey     string
	SquareKey       string
	CrossKey        string
	CircleKey       string
	LeftArrowKey    string
	RightArrowKey   string
	UpArrowKey      string
	DownArrowKey    string
	SliderLeftKey   string
	SliderRightKey  string
	Verbose         bool
}

type TouchEvent struct {
	Type    string  `json:"type"`
	ID      int     `json:"id"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Zone    string  `json:"zone"`
	VX      float64 `json:"vx,omitempty"`
}

type Controller struct {
	keyboard     KeyboardBackend
	config       *Config
	activeKeys   map[string]bool
	mu           sync.Mutex
	sliderState  struct {
		left  bool
		right bool
	}
}

func NewController(keyboard KeyboardBackend, config *Config) *Controller {
	return &Controller{
		keyboard:   keyboard,
		config:     config,
		activeKeys: make(map[string]bool),
	}
}

func (c *Controller) getKeyForZone(zone string, isSecond bool) string {
	switch zone {
	case "triangle":
		if isSecond {
			return c.config.UpArrowKey
		}
		return c.config.TriangleKey
	case "square":
		if isSecond {
			return c.config.LeftArrowKey
		}
		return c.config.SquareKey
	case "cross":
		if isSecond {
			return c.config.DownArrowKey
		}
		return c.config.CrossKey
	case "circle":
		if isSecond {
			return c.config.RightArrowKey
		}
		return c.config.CircleKey
	}
	return ""
}

func (c *Controller) HandleTouch(event TouchEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch event.Type {
	case "start":
		key := c.getKeyForZone(event.Zone, false)
		if key != "" {
			if c.config.Verbose {
				fmt.Printf("[Touch] Zone: %s -> Key: %s (press)\n", event.Zone, key)
			}
			c.keyboard.Press(key)
			c.activeKeys[fmt.Sprintf("%d", event.ID)] = true
		}

	case "end":
		key := c.getKeyForZone(event.Zone, false)
		if key != "" {
			if c.config.Verbose {
				fmt.Printf("[Touch] Zone: %s -> Key: %s (release)\n", event.Zone, key)
			}
			c.keyboard.Release(key)
			delete(c.activeKeys, fmt.Sprintf("%d", event.ID))
		}

	case "slide":
		// Handle slider
		threshold := 0.5
		if event.VX > threshold {
			if !c.sliderState.right {
				if c.config.Verbose {
					fmt.Printf("[Slide] Right (vx: %.2f)\n", event.VX)
				}
				c.keyboard.Press(c.config.SliderRightKey)
				c.sliderState.right = true
				// Auto-release after short delay
				go func() {
					time.Sleep(100 * time.Millisecond)
					c.mu.Lock()
					c.keyboard.Release(c.config.SliderRightKey)
					c.sliderState.right = false
					c.mu.Unlock()
				}()
			}
		} else if event.VX < -threshold {
			if !c.sliderState.left {
				if c.config.Verbose {
					fmt.Printf("[Slide] Left (vx: %.2f)\n", event.VX)
				}
				c.keyboard.Press(c.config.SliderLeftKey)
				c.sliderState.left = true
				// Auto-release after short delay
				go func() {
					time.Sleep(100 * time.Millisecond)
					c.mu.Lock()
					c.keyboard.Release(c.config.SliderLeftKey)
					c.sliderState.left = false
					c.mu.Unlock()
				}()
			}
		}
	}
}

func handleWebSocket(controller *Controller) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade error: %v", err)
			return
		}
		defer conn.Close()

		fmt.Printf("[WS] Client connected: %s\n", r.RemoteAddr)

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("[WS] Client disconnected: %s\n", r.RemoteAddr)
				break
			}

			var event TouchEvent
			if err := json.Unmarshal(message, &event); err != nil {
				log.Printf("JSON parse error: %v", err)
				continue
			}

			controller.HandleTouch(event)
		}
	}
}

func serveClient(w http.ResponseWriter, r *http.Request) {
	data, err := clientHTML.ReadFile("client.html")
	if err != nil {
		http.Error(w, "Failed to load client", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write(data)
}

func getLocalIPs() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	return ips
}

func main() {
	config := &Config{}

	flag.IntVar(&config.Port, "port", 3939, "Server port")
	flag.StringVar(&config.TriangleKey, "triangle", "W", "Key for triangle button")
	flag.StringVar(&config.SquareKey, "square", "A", "Key for square button")
	flag.StringVar(&config.CrossKey, "cross", "S", "Key for cross button")
	flag.StringVar(&config.CircleKey, "circle", "D", "Key for circle button")
	flag.StringVar(&config.UpArrowKey, "up", "I", "Key for up arrow")
	flag.StringVar(&config.DownArrowKey, "down", "K", "Key for down arrow")
	flag.StringVar(&config.LeftArrowKey, "left", "J", "Key for left arrow")
	flag.StringVar(&config.RightArrowKey, "right", "L", "Key for right arrow")
	flag.StringVar(&config.SliderLeftKey, "slider-left", "Q", "Key for left slide")
	flag.StringVar(&config.SliderRightKey, "slider-right", "E", "Key for right slide")
	flag.BoolVar(&config.Verbose, "verbose", false, "Print touch events")
	flag.Parse()

	fmt.Println("===========================================")
	fmt.Println("  Project Diva Controller for Linux")
	fmt.Println("===========================================")
	fmt.Println()

	keyboard, err := NewKeyboardBackend()
	if err != nil {
		log.Fatalf("Failed to initialize keyboard: %v", err)
	}
	defer keyboard.Close()

	controller := NewController(keyboard, config)

	http.HandleFunc("/", serveClient)
	http.HandleFunc("/ws", handleWebSocket(controller))

	fmt.Println("Key mappings:")
	fmt.Printf("  Triangle: %s  Square: %s  Cross: %s  Circle: %s\n",
		config.TriangleKey, config.SquareKey, config.CrossKey, config.CircleKey)
	fmt.Printf("  Arrows: Up=%s Down=%s Left=%s Right=%s\n",
		config.UpArrowKey, config.DownArrowKey, config.LeftArrowKey, config.RightArrowKey)
	fmt.Printf("  Slider: Left=%s Right=%s\n", config.SliderLeftKey, config.SliderRightKey)
	fmt.Println()

	ips := getLocalIPs()
	fmt.Printf("Server starting on port %d\n", config.Port)
	fmt.Println("Open one of these URLs on your tablet/phone:")
	for _, ip := range ips {
		fmt.Printf("  http://%s:%d\n", ip, config.Port)
	}
	fmt.Println()

	addr := fmt.Sprintf(":%d", config.Port)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
