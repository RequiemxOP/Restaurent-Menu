package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ==========================================
// 1. DOMAIN MODELS & TYPES
// ==========================================

type MenuItem struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Price       float64 `json:"price"`
	Description string  `json:"description"`
	Image       string  `json:"image"`
	Badge       string  `json:"badge,omitempty"`
}

type Reservation struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Phone     string    `json:"phone"`
	Date      time.Time `json:"date"`
	Guests    int       `json:"guests"`
	Status    string    `json:"status"` // PENDING, CONFIRMED, CANCELLED
	CreatedAt time.Time `json:"createdAt"`
}

type ChatRequest struct {
	Message string `json:"message"`
}

type ChatResponse struct {
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

type SSEEvent struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// ==========================================
// 2. CONCURRENT DATA STORES (IN-MEMORY)
// ==========================================

// ReservationStore handles thread-safe reservation logic without a DB
type ReservationStore struct {
	sync.RWMutex
	reservations map[string]Reservation
}

func NewReservationStore() *ReservationStore {
	return &ReservationStore{
		reservations: make(map[string]Reservation),
	}
}

func (s *ReservationStore) Create(r Reservation) Reservation {
	s.Lock()
	defer s.Unlock()
	r.ID = fmt.Sprintf("RES-%d", time.Now().UnixNano())
	r.Status = "PENDING"
	r.CreatedAt = time.Now()
	s.reservations[r.ID] = r
	return r
}

func (s *ReservationStore) GetAll() []Reservation {
	s.RLock()
	defer s.RUnlock()
	list := make([]Reservation, 0, len(s.reservations))
	for _, res := range s.reservations {
		list = append(list, res)
	}
	return list
}

// Cache implements a simple thread-safe TTL cache
type CacheItem struct {
	Value      interface{}
	Expiration int64
}

type TTLCache struct {
	sync.RWMutex
	items map[string]CacheItem
}

func NewTTLCache(cleanupInterval time.Duration) *TTLCache {
	cache := &TTLCache{
		items: make(map[string]CacheItem),
	}
	go cache.startCleanupTimer(cleanupInterval)
	return cache
}

func (c *TTLCache) Set(key string, value interface{}, duration time.Duration) {
	c.Lock()
	defer c.Unlock()
	c.items[key] = CacheItem{
		Value:      value,
		Expiration: time.Now().Add(duration).UnixNano(),
	}
}

func (c *TTLCache) Get(key string) (interface{}, bool) {
	c.RLock()
	defer c.RUnlock()
	item, found := c.items[key]
	if !found {
		return nil, false
	}
	if time.Now().UnixNano() > item.Expiration {
		return nil, false
	}
	return item.Value, true
}

func (c *TTLCache) startCleanupTimer(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for range ticker.C {
		now := time.Now().UnixNano()
		c.Lock()
		for k, v := range c.items {
			if now > v.Expiration {
				delete(c.items, k)
			}
		}
		c.Unlock()
	}
}

// ==========================================
// 3. SERVER-SENT EVENTS (SSE) BROKER
// ==========================================

// SSEBroker handles broadcasting realtime events to connected clients
type SSEBroker struct {
	Notifier       chan []byte
	newClients     chan chan []byte
	closingClients chan chan []byte
	clients        map[chan []byte]bool
}

func NewSSEBroker() *SSEBroker {
	broker := &SSEBroker{
		Notifier:       make(chan []byte, 10),
		newClients:     make(chan chan []byte),
		closingClients: make(chan chan []byte),
		clients:        make(map[chan []byte]bool),
	}
	go broker.listen()
	return broker
}

func (broker *SSEBroker) listen() {
	for {
		select {
		case s := <-broker.newClients:
			broker.clients[s] = true
			log.Printf("Client added. %d registered clients", len(broker.clients))
		case s := <-broker.closingClients:
			delete(broker.clients, s)
			log.Printf("Removed client. %d registered clients", len(broker.clients))
		case event := <-broker.Notifier:
			for clientMessageChan := range broker.clients {
				select {
				case clientMessageChan <- event:
				case <-time.After(time.Second):
					log.Printf("Skipping client messaging due to timeout")
				}
			}
		}
	}
}

func (broker *SSEBroker) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	flusher, ok := rw.(http.Flusher)
	if !ok {
		http.Error(rw, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")
	rw.Header().Set("Access-Control-Allow-Origin", "http://localhost:8080")

	messageChan := make(chan []byte)
	broker.newClients <- messageChan
	
	// Ensure we only close exactly once when ServeHTTP exits
	defer func() {
		broker.closingClients <- messageChan
	}()

	for {
		select {
		case <-req.Context().Done():
			return
		case msg := <-messageChan:
			fmt.Fprintf(rw, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

// ==========================================
// 4. COMPLEX ROUTING & MIDDLEWARES
// ==========================================

type Middleware func(http.HandlerFunc) http.HandlerFunc

func Chain(f http.HandlerFunc, middlewares ...Middleware) http.HandlerFunc {
	for i := len(middlewares) - 1; i >= 0; i-- {
		f = middlewares[i](f)
	}
	return f
}

func LoggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next(w, r)
		log.Printf("[REQ] %s %s took %v", r.Method, r.URL.Path, time.Since(start))
	}
}

func RateLimiterMiddleware(next http.HandlerFunc) http.HandlerFunc {
	// A basic representation of a token bucket rate limiter logic could go here.
	// For simplicity, we just pass through, but this demonstrates complex architectural hooks.
	return func(w http.ResponseWriter, r *http.Request) {
		next(w, r)
	}
}

func CORSHeaders(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:8080")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization")
		if r.Method == "OPTIONS" {
			return
		}
		next(w, r)
	}
}

func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Mock token auth (checking query string or Authorization header)
		token := r.URL.Query().Get("token")
		if token == "" {
			reqToken := r.Header.Get("Authorization")
			splitToken := strings.Split(reqToken, "Bearer ")
			if len(splitToken) == 2 {
				token = splitToken[1]
			}
		}
		
		if token != "secret123" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// ==========================================
// 5. APPLICATION STATE & HANDLERS
// ==========================================

type AppConfig struct {
	MenuCache *TTLCache
	ResStore  *ReservationStore
	Broker    *SSEBroker
}

// simulate heavy DB processing for Reservations
func (app *AppConfig) HandleReservation(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	var res Reservation
	if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	created := app.ResStore.Create(res)

	// Broadcast successful reservation over Server-Sent Events to connected clients
	event := SSEEvent{
		Type:    "RESERVATION_UPDATE",
		Message: fmt.Sprintf("New reservation confirmed for %s!", created.Name),
	}
	msg, _ := json.Marshal(event)
	app.Broker.Notifier <- msg

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(created)
}

func (app *AppConfig) HandleMenu(w http.ResponseWriter, r *http.Request) {
	// Check Cache first
	if cachedRes, found := app.MenuCache.Get("full_menu"); found {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cachedRes.([]byte))
		return
	}

	// Simulate latency/DB Access
	time.Sleep(200 * time.Millisecond)

	items := []MenuItem{
		{1, "Greek Salad", 25.50, "Tomatoes, green bell pepper, sliced cucumber onion, olives, and feta cheese.", "./assets/images/menu-1.png", "Seasonal"},
		{2, "Lasagne", 40.00, "Vegetables, cheeses, ground meats, tomato sauce, seasonings and spices", "./assets/images/menu-2.png", ""},
		{3, "Butternut Pumpkin", 10.00, "Typesetting industry lorem Lorem Ipsum is simply dummy text of the priand.", "./assets/images/menu-3.png", ""},
		{4, "Tokusen Wagyu", 39.00, "Vegetables, cheeses, ground meats, tomato sauce, seasonings and spices.", "./assets/images/menu-4.png", "New"},
		{5, "Olivas Rellenas", 25.00, "Avocados with crab meat, red onion, crab salad stuffed red bell pepper and green bell pepper.", "./assets/images/menu-5.png", ""},
		{6, "Opu Fish", 49.00, "Vegetables, cheeses, ground meats, tomato sauce, seasonings and spices", "./assets/images/menu-6.png", ""},
	}

	bytes, _ := json.Marshal(items)
	
	// Set to cache for 1 minute
	app.MenuCache.Set("full_menu", bytes, 1*time.Minute)

	w.Header().Set("Content-Type", "application/json")
	w.Write(bytes)
}

// Rule-based NLP Engine emulator
func processAIChat(msg string) string {
	lowerMsg := strings.ToLower(msg)
	if match, _ := regexp.MatchString(`.*(book|reserve|table).*`, lowerMsg); match {
		return "I can certainly help you book a table! Let me know what date and time you're aiming for, or just fill out the online reservation form on our site."
	}
	if match, _ := regexp.MatchString(`.*(menu|food|eat).*`, lowerMsg); match {
		return "Our menu includes exquisite dishes like Lobster Tortellini and Tokusen Wagyu beef. You can look at the Delicious Menu section above for details!"
	}
	if match, _ := regexp.MatchString(`.*(hours|open|door).*`, lowerMsg); match {
		return "We are open daily from 8.00 am to 10.00 pm. We'd love to see you!"
	}

	responses := []string{
		"That's interesting! Antigravity UI is designed to wow your guests. How else can I help you?",
		"I'm an advanced AI integrated into the Grilli UI Pro experience. What would you like to explore?",
		"Our chefs are world-class. Would you be interested in making a reservation tonight?",
	}
	return responses[rand.Intn(len(responses))]
}

func (app *AppConfig) HandleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	json.NewDecoder(r.Body).Decode(&req)

	reply := processAIChat(req.Message)

	res := ChatResponse{
		Message:   reply,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

// Live Mock Order Generator simulating Realtime Kitchen Activity
func startKitchenSimulator(broker *SSEBroker) {
	dishes := []string{"Greek Salad", "Lasagne", "Tokusen Wagyu", "Opu Fish", "Lobster Tortellini"}
	statuses := []string{"started preparing", "is almost ready", "is ready for serving"}

	for {
		time.Sleep(time.Duration(rand.Intn(15)+10) * time.Second) // Random 10-25 seconds
		dish := dishes[rand.Intn(len(dishes))]
		status := statuses[rand.Intn(len(statuses))]
		event := SSEEvent{
			Type:    "KITCHEN_UPDATE",
			Message: fmt.Sprintf("Chef %s %s!", status, dish),
		}
		msg, _ := json.Marshal(event)
		broker.Notifier <- msg
	}
}

// ==========================================
// 6. MAIN SERVER LAUNCH
// ==========================================

func main() {
	rand.Seed(time.Now().UnixNano())

	// Initialize components
	app := &AppConfig{
		MenuCache: NewTTLCache(5 * time.Minute),
		ResStore:  NewReservationStore(),
		Broker:    NewSSEBroker(),
	}

	// Start asynchronous background tasks
	go startKitchenSimulator(app.Broker)

	// Set up multiplexer
	mux := http.NewServeMux()

	// Static file serving mapped exclusively to expected folders
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("./assets/"))))
	mux.Handle("/node_modules/", http.StripPrefix("/node_modules/", http.FileServer(http.Dir("./node_modules/"))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			http.ServeFile(w, r, "./index.html")
			return
		}
		if r.URL.Path == "/favicon.svg" {
			http.ServeFile(w, r, "./favicon.svg")
			return
		}
		http.NotFound(w, r)
	})

	// API Endpoints using Complex Middleware Chain
	mux.HandleFunc("/api/menu", Chain(app.HandleMenu, CORSHeaders, LoggingMiddleware, RateLimiterMiddleware))
	mux.HandleFunc("/api/reservation", Chain(app.HandleReservation, CORSHeaders, LoggingMiddleware, AuthMiddleware))
	mux.HandleFunc("/api/chat", Chain(app.HandleChat, CORSHeaders, LoggingMiddleware))
	
	// Server-Sent Events Endpoint (Protected)
	mux.Handle("/api/events", Chain(app.Broker.ServeHTTP, CORSHeaders, LoggingMiddleware, AuthMiddleware))

	// HTTP Server config with Graceful Shutdown rules
	server := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Println("Starting Advanced Server Architecture on :8080...")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Critical listen error: %v\n", err)
		}
	}()

	<-stop

	log.Println("Shutting down the server safely...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server shutdown failed: %v\n", err)
	}

	log.Println("Server gracefully stopped.")
}
