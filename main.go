package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultAddr    = ":8080"
	maxJSONBody    = 1 << 20
	menuCacheTTL   = time.Minute
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
	Category    string  `json:"category"`
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

type AppStats struct {
	MenuItems        int       `json:"menuItems"`
	Reservations    int       `json:"reservations"`
	PendingBookings int       `json:"pendingBookings"`
	Cancelled       int       `json:"cancelled"`
	StartedAt       time.Time `json:"startedAt"`
	UptimeSeconds   int64     `json:"uptimeSeconds"`
}

type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

type errorResponse struct {
	Error string `json:"error"`
}

var menuItems = []MenuItem{
	{1, "Greek Salad", 25.50, "Tomatoes, green bell pepper, sliced cucumber onion, olives, and feta cheese.", "./assets/images/menu-1.png", "Salads", "Seasonal"},
	{2, "Lasagne", 40.00, "Vegetables, cheeses, ground meats, tomato sauce, seasonings and spices.", "./assets/images/menu-2.png", "Mains", ""},
	{3, "Butternut Pumpkin", 10.00, "Roasted pumpkin, soft herbs, toasted seeds, and a silky house dressing.", "./assets/images/menu-3.png", "Starters", ""},
	{4, "Tokusen Wagyu", 39.00, "Char-grilled wagyu, seasonal vegetables, and a warm pepper jus.", "./assets/images/menu-4.png", "Mains", "New"},
	{5, "Olivas Rellenas", 25.00, "Avocados with crab meat, red onion, and stuffed bell pepper.", "./assets/images/menu-5.png", "Starters", ""},
	{6, "Opu Fish", 49.00, "Fresh fish with aromatic spices, vegetables, and a bright citrus finish.", "./assets/images/menu-6.png", "Seafood", ""},
}

var (
	reservationIntentPattern = regexp.MustCompile(`.*(book|reserve|table).*`)
	menuIntentPattern        = regexp.MustCompile(`.*(menu|food|eat).*`)
	hoursIntentPattern       = regexp.MustCompile(`.*(hours|open|door).*`)
)

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
	now := time.Now().UTC()
	r.ID = fmt.Sprintf("RES-%d", now.UnixNano())
	r.Status = "PENDING"
	r.CreatedAt = now
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
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})
	return list
}

func (s *ReservationStore) Cancel(id string) (Reservation, bool) {
	s.Lock()
	defer s.Unlock()
	res, ok := s.reservations[id]
	if !ok {
		return Reservation{}, false
	}
	res.Status = "CANCELLED"
	s.reservations[id] = res
	return res, true
}

func (s *ReservationStore) Stats() (total int, pending int, cancelled int) {
	s.RLock()
	defer s.RUnlock()
	for _, res := range s.reservations {
		total++
		switch res.Status {
		case "CANCELLED":
			cancelled++
		case "PENDING":
			pending++
		}
	}
	return total, pending, cancelled
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
	item, found := c.items[key]
	c.RUnlock()
	if !found {
		return nil, false
	}
	if time.Now().UnixNano() > item.Expiration {
		c.Lock()
		delete(c.items, key)
		c.Unlock()
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

type rateLimitBucket struct {
	Count     int
	ResetTime time.Time
}

type RateLimiter struct {
	sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]rateLimitBucket
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		limit:   limit,
		window:  window,
		buckets: make(map[string]rateLimitBucket),
	}
}

func (rl *RateLimiter) Allow(key string) bool {
	now := time.Now()
	rl.Lock()
	defer rl.Unlock()

	bucket := rl.buckets[key]
	if bucket.ResetTime.IsZero() || now.After(bucket.ResetTime) {
		rl.buckets[key] = rateLimitBucket{Count: 1, ResetTime: now.Add(rl.window)}
		return true
	}
	if bucket.Count >= rl.limit {
		return false
	}
	bucket.Count++
	rl.buckets[key] = bucket
	return true
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
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
		writeError(rw, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")

	messageChan := make(chan []byte, 8)
	broker.newClients <- messageChan

	// Ensure we only close exactly once when ServeHTTP exits
	defer func() {
		broker.closingClients <- messageChan
		close(messageChan)
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

func MethodMiddleware(methods ...string) Middleware {
	allowed := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		allowed[method] = struct{}{}
	}

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if _, ok := allowed[r.Method]; !ok {
				w.Header().Set("Allow", strings.Join(methods, ", "))
				writeError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
			next(w, r)
		}
	}
}

func RateLimiterMiddleware(limiter *RateLimiter) Middleware {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !limiter.Allow(clientIP(r)) {
				writeError(w, http.StatusTooManyRequests, "too many requests, please slow down")
				return
			}
			next(w, r)
		}
	}
}

func CORSHeaders(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowedOrigin := os.Getenv("ALLOWED_ORIGIN")
		if allowedOrigin == "" {
			allowedOrigin = "http://localhost:8080"
		}
		if origin == allowedOrigin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
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
			if strings.HasPrefix(reqToken, "Bearer ") {
				token = strings.TrimSpace(strings.TrimPrefix(reqToken, "Bearer "))
			}
		}

		expected := os.Getenv("API_TOKEN")
		if expected == "" {
			expected = "secret123"
		}

		if token != expected {
			writeError(w, http.StatusUnauthorized, "unauthorized")
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
	Limiter   *RateLimiter
	StartedAt time.Time
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("json encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return false
	}
	return true
}

func validateReservation(res Reservation) error {
	res.Name = strings.TrimSpace(res.Name)
	res.Phone = strings.TrimSpace(res.Phone)

	switch {
	case res.Name == "":
		return fmt.Errorf("name is required")
	case len(res.Name) > 80:
		return fmt.Errorf("name must be 80 characters or fewer")
	case res.Phone == "":
		return fmt.Errorf("phone number is required")
	case len(res.Phone) > 32:
		return fmt.Errorf("phone number is too long")
	case res.Guests < 1 || res.Guests > 12:
		return fmt.Errorf("guests must be between 1 and 12")
	case res.Date.IsZero():
		return fmt.Errorf("reservation date and time are required")
	case res.Date.Before(time.Now().Add(-time.Minute)):
		return fmt.Errorf("reservation date must be in the future")
	}

	return nil
}

func filterMenuItems(r *http.Request) []MenuItem {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	category := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("category")))
	sortMode := strings.TrimSpace(r.URL.Query().Get("sort"))

	items := make([]MenuItem, 0, len(menuItems))
	for _, item := range menuItems {
		if query != "" {
			searchTarget := strings.ToLower(item.Name + " " + item.Description + " " + item.Badge + " " + item.Category)
			if !strings.Contains(searchTarget, query) {
				continue
			}
		}
		if category != "" && strings.ToLower(item.Category) != category {
			continue
		}
		items = append(items, item)
	}

	switch sortMode {
	case "price_asc":
		sort.Slice(items, func(i, j int) bool {
			return items[i].Price < items[j].Price
		})
	case "price_desc":
		sort.Slice(items, func(i, j int) bool {
			return items[i].Price > items[j].Price
		})
	case "name":
		sort.Slice(items, func(i, j int) bool {
			return items[i].Name < items[j].Name
		})
	}

	return items
}

// simulate heavy DB processing for Reservations
func (app *AppConfig) HandleReservation(w http.ResponseWriter, r *http.Request) {
	var res Reservation
	if ok := decodeJSON(w, r, &res); !ok {
		return
	}
	res.Name = strings.TrimSpace(res.Name)
	res.Phone = strings.TrimSpace(res.Phone)
	if err := validateReservation(res); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res.Date = res.Date.UTC()

	created := app.ResStore.Create(res)

	// Broadcast successful reservation over Server-Sent Events to connected clients
	event := SSEEvent{
		Type:    "RESERVATION_UPDATE",
		Message: fmt.Sprintf("New reservation confirmed for %s!", created.Name),
	}
	msg, _ := json.Marshal(event)
	app.Broker.Notifier <- msg

	writeJSON(w, http.StatusCreated, created)
}

func (app *AppConfig) HandleMenu(w http.ResponseWriter, r *http.Request) {
	cacheKey := "menu:" + r.URL.RawQuery
	// Check Cache first
	if cachedRes, found := app.MenuCache.Get(cacheKey); found {
		w.Header().Set("Content-Type", "application/json")
		w.Write(cachedRes.([]byte))
		return
	}

	bytes, err := json.Marshal(filterMenuItems(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load menu")
		return
	}

	// Set to cache for 1 minute
	app.MenuCache.Set(cacheKey, bytes, menuCacheTTL)

	w.Header().Set("Content-Type", "application/json")
	w.Write(bytes)
}

func (app *AppConfig) HandleReservations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, app.ResStore.GetAll())
}

func (app *AppConfig) HandleReservationByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/reservations/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}

	cancelled, ok := app.ResStore.Cancel(id)
	if !ok {
		writeError(w, http.StatusNotFound, "reservation not found")
		return
	}

	event := SSEEvent{
		Type:    "RESERVATION_UPDATE",
		Message: fmt.Sprintf("Reservation %s was cancelled.", cancelled.ID),
	}
	msg, _ := json.Marshal(event)
	app.Broker.Notifier <- msg

	writeJSON(w, http.StatusOK, cancelled)
}

func (app *AppConfig) HandleStats(w http.ResponseWriter, r *http.Request) {
	total, pending, cancelled := app.ResStore.Stats()
	writeJSON(w, http.StatusOK, AppStats{
		MenuItems:        len(menuItems),
		Reservations:    total,
		PendingBookings: pending,
		Cancelled:       cancelled,
		StartedAt:       app.StartedAt,
		UptimeSeconds:   int64(time.Since(app.StartedAt).Seconds()),
	})
}

func (app *AppConfig) HandleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC(),
	})
}

// Rule-based NLP Engine emulator
func processAIChat(msg string) string {
	lowerMsg := strings.ToLower(msg)
	if reservationIntentPattern.MatchString(lowerMsg) {
		return "I can certainly help you book a table! Let me know what date and time you're aiming for, or just fill out the online reservation form on our site."
	}
	if menuIntentPattern.MatchString(lowerMsg) {
		return "Our menu includes exquisite dishes like Lobster Tortellini and Tokusen Wagyu beef. You can look at the Delicious Menu section above for details!"
	}
	if hoursIntentPattern.MatchString(lowerMsg) {
		return "We are open daily from 8.00 am to 10.00 pm. We'd love to see you!"
	}

	responses := []string{
		"That's interesting! Grilli can help with menu picks, opening hours, and table bookings. How else can I help?",
		"I'm the Grilli dining assistant. Ask me about dishes, reservations, or today's restaurant flow.",
		"Our chefs are world-class. Would you be interested in making a reservation tonight?",
	}
	return responses[rand.Intn(len(responses))]
}

func (app *AppConfig) HandleChat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if ok := decodeJSON(w, r, &req); !ok {
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	if len(req.Message) > 500 {
		writeError(w, http.StatusBadRequest, "message must be 500 characters or fewer")
		return
	}

	reply := processAIChat(req.Message)

	res := ChatResponse{
		Message:   reply,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	writeJSON(w, http.StatusOK, res)
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
		Limiter:   NewRateLimiter(120, time.Minute),
		StartedAt: time.Now().UTC(),
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

	// API Endpoints using middleware chain
	mux.HandleFunc("/api/menu", Chain(
		app.HandleMenu,
		CORSHeaders,
		LoggingMiddleware,
		RateLimiterMiddleware(app.Limiter),
		MethodMiddleware(http.MethodGet, http.MethodOptions),
	))
	mux.HandleFunc("/api/reservation", Chain(
		app.HandleReservation,
		CORSHeaders,
		LoggingMiddleware,
		RateLimiterMiddleware(app.Limiter),
		MethodMiddleware(http.MethodPost, http.MethodOptions),
		AuthMiddleware,
	))
	mux.HandleFunc("/api/chat", Chain(
		app.HandleChat,
		CORSHeaders,
		LoggingMiddleware,
		RateLimiterMiddleware(app.Limiter),
		MethodMiddleware(http.MethodPost, http.MethodOptions),
	))
	mux.HandleFunc("/api/reservations", Chain(
		app.HandleReservations,
		CORSHeaders,
		LoggingMiddleware,
		RateLimiterMiddleware(app.Limiter),
		MethodMiddleware(http.MethodGet, http.MethodOptions),
		AuthMiddleware,
	))
	mux.HandleFunc("/api/reservations/", Chain(
		app.HandleReservationByID,
		CORSHeaders,
		LoggingMiddleware,
		RateLimiterMiddleware(app.Limiter),
		MethodMiddleware(http.MethodDelete, http.MethodOptions),
		AuthMiddleware,
	))
	mux.HandleFunc("/api/stats", Chain(
		app.HandleStats,
		CORSHeaders,
		LoggingMiddleware,
		RateLimiterMiddleware(app.Limiter),
		MethodMiddleware(http.MethodGet, http.MethodOptions),
	))
	mux.HandleFunc("/api/health", Chain(
		app.HandleHealth,
		CORSHeaders,
		LoggingMiddleware,
		MethodMiddleware(http.MethodGet, http.MethodOptions),
	))

	// Server-Sent Events Endpoint (Protected)
	mux.Handle("/api/events", Chain(
		app.Broker.ServeHTTP,
		CORSHeaders,
		LoggingMiddleware,
		MethodMiddleware(http.MethodGet, http.MethodOptions),
		AuthMiddleware,
	))

	addr := os.Getenv("PORT")
	if addr == "" {
		addr = defaultAddr
	}
	if !strings.HasPrefix(addr, ":") {
		addr = ":" + addr
	}

	// HTTP Server config with Graceful Shutdown rules
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Starting restaurant server on %s...", addr)
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
