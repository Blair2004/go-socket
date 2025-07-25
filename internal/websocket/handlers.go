package websocket

import (
	"time"

	"github.com/google/uuid"

	"socket-server/internal/models"
)

// handleClientMessages processes messages from a client
func (s *Server) handleClientMessages(client *models.Client, done chan bool) {
	defer func() {
		s.logger.Debug("Client %s message handler exiting", client.ID)
		done <- true
	}()

	for {
		var msg map[string]interface{}
		err := client.SafeReadJSON(&msg)
		if err != nil {
			if err == models.ErrNilConnection {
				s.logger.Debug("Client %s connection became nil during message read", client.ID)
			} else {
				s.logger.WebSocketError(client.ID, err)
			}
			break
		}

		// Reset read deadline on successful message
		if err := client.SafeSetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
			s.logger.Debug("Client %s failed to set read deadline: %v", client.ID, err)
			break
		}
		client.LastSeen = time.Now()

		// Log incoming message
		actionStr := "unknown"
		if action, ok := msg["action"].(string); ok {
			actionStr = action
		}

		s.logger.MessageReceived(client.ID, client.Username, actionStr, msg)

		// Handle different message types
		switch msg["action"] {
		case "authenticate":
			s.handleAuthentication(client, msg)
		case "join_channel":
			s.handleJoinChannel(client, msg)
		case "leave_channel":
			s.handleLeaveChannel(client, msg)
		case "send_message":
			s.handleSendMessage(client, msg)
		case "ping":
			s.handlePing(client)
		default:
			s.handleMessage(client, msg)
		}
	}
}

func (s *Server) handleMessage(client *models.Client, msg map[string]interface{}) {
	// Forward unsupported messages to Laravel
	s.logger.Debug("Forwarding unsupported message to Laravel from client %s", client.ID)

	// log the variable msg
	s.logger.Debug("Raw message from client %s: %v", client.ID, msg)

	// Convert raw message to models.Message
	message := models.Message{
		ID:        uuid.New().String(),
		Event:     getStringFromMap(msg, "action", "unknown"),
		Channel:   getStringFromMap(msg, "channel", ""),
		Data:      msg["data"],
		UserID:    client.UserID,
		Username:  client.Username,
		Timestamp: time.Now(),
	}

	// Log specifically for ping messages
	if message.Event == "ping" {
		s.logger.Info("🏓 Client %s sent ping message, event=%s, channel=%s, data=%v", client.ID, message.Event, message.Channel, message.Data)
	}

	// Check if this is a ping message that should be handled internally
	if message.Event == "ping" && message.Channel == "" && message.Data == nil {
		s.logger.Info("🏓 Handling ping internally, not sending to Laravel")
		// Just send pong back to client
		pong := models.Message{
			ID:        uuid.New().String(),
			Event:     "pong",
			Timestamp: time.Now(),
		}
		client.SendMessage(pong)
		return
	}

	start := time.Now()
	s.laravelSvc.DispatchMessage(message, client)
	duration := time.Since(start)

	if message.Event == "ping" {
		s.logger.Info("🏓 Laravel ping dispatch took: %v", duration)
	}
}

// getStringFromMap safely extracts a string value from a map
func getStringFromMap(m map[string]interface{}, key string, defaultValue string) string {
	if value, ok := m[key].(string); ok {
		return value
	}
	return defaultValue
}

// handleClientPing manages ping/pong for connection health
func (s *Server) handleClientPing(client *models.Client, pingTicker *time.Ticker, done chan bool) {
	defer func() {
		s.logger.Debug("Client %s ping handler exiting", client.ID)
		done <- true
	}()

	for range pingTicker.C {
		// Check if client connection is still valid before sending ping
		if !client.IsConnected() {
			s.logger.Debug("Client %s connection is no longer valid, stopping ping handler", client.ID)
			return
		}

		// Check if client is still registered in server
		s.mutex.RLock()
		_, exists := s.clients[client.ID]
		s.mutex.RUnlock()

		if !exists {
			s.logger.Debug("Client %s no longer registered, stopping ping handler", client.ID)
			return
		}

		// Send ping to client
		err := client.SendPing()
		if err != nil {
			// Log different error types for better debugging
			if err == models.ErrNilConnection {
				s.logger.Debug("Client %s connection became nil during ping", client.ID)
			} else {
				s.logger.Error("Failed to send ping to client %s: %v", client.ID, err)
			}
			return
		}
		s.logger.PingSent(client.ID)
	}
}

// handleAuthentication processes client authentication
func (s *Server) handleAuthentication(client *models.Client, msg map[string]interface{}) {
	tokenStr, ok := msg["token"].(string)
	if !ok {
		s.logger.Error("Client %s sent invalid token format", client.ID)
		s.sendError(client, "Invalid token format")
		return
	}

	s.logger.Debug("Client %s attempting JWT authentication", client.ID)

	claims, err := s.authService.ValidateToken(tokenStr)
	if err != nil {
		s.logger.ClientAuthenticationFailed(client.ID, err)
		s.sendError(client, "Invalid token")
		s.laravelSvc.DispatchAuthentication(client, "failed", tokenStr)
		return
	}

	// Extract user info from claims
	userID, username, email := s.authService.ExtractUserInfo(claims)
	client.SetUserInfo(userID, username, email)

	s.logger.ClientAuthenticated(client.ID, client.Username, client.UserID)
	s.laravelSvc.DispatchAuthentication(client, "success", tokenStr)
}

// handleJoinChannel adds client to a channel
func (s *Server) handleJoinChannel(client *models.Client, msg map[string]interface{}) {
	channelName, ok := msg["channel"].(string)
	privateStatus, okPrivate := msg["private"].(bool)

	if !ok {
		s.logger.Error("Client %s sent invalid channel name for join", client.ID)
		s.sendError(client, "Invalid channel name")
		return
	}

	if !okPrivate {
		privateStatus = false // Default to public channel if not specified
	}

	s.logger.Debug("Client %s (%s) attempting to join channel '%s'", client.ID, client.Username, channelName)

	// Get or create channel
	channel := s.getOrCreateChannel(channelName, privateStatus)

	// Check if channel requires authentication
	if channel.RequireAuth && client.UserID == "" {
		s.logger.Warn("Client %s denied access to channel '%s': authentication required", client.ID, channelName)
		s.sendError(client, "Channel requires authentication")
		return
	}

	// Create message for Laravel dispatch
	// Forward optional data from client, or nil if not provided
	var dataToForward interface{}
	if clientData, exists := msg["data"]; exists {
		dataToForward = clientData
	} else {
		dataToForward = nil
	}

	joinMessage := models.Message{
		ID:        uuid.New().String(),
		Channel:   channelName,
		Event:     "join_channel",
		Data:      dataToForward,
		Private:   &privateStatus,
		UserID:    client.UserID,
		Username:  client.Username,
		Timestamp: time.Now(),
	}

	// Dispatch to Laravel
	// if the command works with no errors, we'll assume the joinning is approved
	// and proceed to add the client to the channel
	if err := s.laravelSvc.DispatchMessage(joinMessage, client); err != nil {
		s.logger.Error("Failed to dispatch join_channel message to Laravel: %v", err)
	} else {

		// Add client to channel with metadata
		channel.AddClient(client)
		client.AddToChannelWithMetadata(channelName, dataToForward)

		s.logger.ChannelJoined(client.ID, client.Username, channelName)

		// Send confirmation
		confirmation := models.Message{
			ID:        uuid.New().String(),
			Event:     "joined_channel",
			Data:      map[string]string{"channel": channelName},
			Timestamp: time.Now(),
		}
		client.SendMessage(confirmation)
	}
}

// handleLeaveChannel removes client from a channel
func (s *Server) handleLeaveChannel(client *models.Client, msg map[string]interface{}) {
	channelName, ok := msg["channel"].(string)
	if !ok {
		s.logger.Error("Client %s sent invalid channel name for leave", client.ID)
		s.sendError(client, "Invalid channel name")
		return
	}

	s.logger.Debug("Client %s (%s) attempting to leave channel '%s'", client.ID, client.Username, channelName)

	channel, exists := s.GetChannel(channelName)
	if !exists {
		s.logger.Error("Client %s tried to leave non-existent channel '%s'", client.ID, channelName)
		s.sendError(client, "Channel not found")
		return
	}

	// Get stored metadata for this channel before removing client
	storedMetadata := client.GetChannelMetadata(channelName)

	// Remove client from channel
	channel.RemoveClient(client.ID)
	client.RemoveFromChannel(channelName)

	s.logger.ChannelLeft(client.ID, client.Username, channelName)

	// Create message for Laravel dispatch
	// Use stored metadata if available, otherwise fall back to client data or default
	var dataToForward interface{}
	if storedMetadata != nil {
		dataToForward = storedMetadata.Data
	} else if clientData, exists := msg["data"]; exists {
		dataToForward = clientData
	} else {
		// Default system data when no stored metadata or client data
		dataToForward = map[string]interface{}{
			"channel":   channelName,
			"client_id": client.ID,
			"user_id":   client.UserID,
			"username":  client.Username,
		}
	}

	leaveMessage := models.Message{
		ID:        uuid.New().String(),
		Channel:   channelName,
		Event:     "leave_channel",
		Data:      dataToForward,
		UserID:    client.UserID,
		Username:  client.Username,
		Timestamp: time.Now(),
	}

	// Dispatch to Laravel
	if err := s.laravelSvc.DispatchMessage(leaveMessage, client); err != nil {
		s.logger.Error("Failed to dispatch leave_channel message to Laravel: %v", err)
	}

	// Send confirmation
	confirmation := models.Message{
		ID:        uuid.New().String(),
		Event:     "left_channel",
		Data:      map[string]string{"channel": channelName},
		Timestamp: time.Now(),
	}
	client.SendMessage(confirmation)
}

// handleSendMessage processes messages sent by clients
func (s *Server) handleSendMessage(client *models.Client, msg map[string]interface{}) {
	channelName, ok := msg["channel"].(string)
	if !ok {
		s.logger.Error("Client %s sent message with invalid channel name", client.ID)
		s.sendError(client, "Invalid channel name")
		return
	}

	event, ok := msg["event"].(string)
	if !ok {
		event = "message"
	}

	data := msg["data"]

	s.logger.MessageSent(client.ID, client.Username, channelName, event, data)

	message := models.Message{
		ID:        uuid.New().String(),
		Channel:   channelName,
		Event:     event,
		Data:      data,
		UserID:    client.UserID,
		Username:  client.Username,
		Timestamp: time.Now(),
	}

	// Dispatch to Laravel if configured
	if err := s.laravelSvc.DispatchMessage(message, client); err != nil {
		s.logger.Error("Failed to dispatch message to Laravel: %v", err)
	}

	// Broadcast to all clients in channel
	s.BroadcastToChannel(channelName, message)
}

// handlePing processes ping messages
func (s *Server) handlePing(client *models.Client) {
	s.logger.PongReceived(client.ID)
	pong := models.Message{
		ID:        uuid.New().String(),
		Event:     "pong",
		Timestamp: time.Now(),
	}
	client.SendMessage(pong)
}

// disconnectClient removes a client from the server
func (s *Server) disconnectClient(client *models.Client) {
	s.logger.ClientDisconnected(client.ID, client.Username, client.RemoteAddr)

	// Remove client from server's client list
	s.mutex.Lock()
	delete(s.clients, client.ID)
	s.mutex.Unlock()

	// Remove client from all channels and notify Laravel
	channels := client.GetChannels()
	allMetadata := client.GetAllChannelMetadata()

	for channelName := range channels {
		if channel, exists := s.GetChannel(channelName); exists {
			// Remove client from channel
			channel.RemoveClient(client.ID)

			// Get stored metadata for this channel
			var dataToForward interface{}
			if metadata, exists := allMetadata[channelName]; exists && metadata != nil {
				dataToForward = metadata.Data
			} else {
				// Fallback to default system data
				dataToForward = map[string]interface{}{
					"channel":         channelName,
					"client_id":       client.ID,
					"user_id":         client.UserID,
					"username":        client.Username,
					"disconnect_type": "connection_lost", // Indicate this was due to disconnection
					"reason":          "client_disconnected",
				}
			}

			// Create leave_channel message for Laravel dispatch
			leaveMessage := models.Message{
				ID:        uuid.New().String(),
				Channel:   channelName,
				Event:     "leave_channel",
				Data:      dataToForward,
				UserID:    client.UserID,
				Username:  client.Username,
				Timestamp: time.Now(),
			}

			// Dispatch to Laravel (don't block disconnection if Laravel fails)
			if err := s.laravelSvc.DispatchMessage(leaveMessage, client); err != nil {
				s.logger.Error("Failed to dispatch disconnect leave_channel message to Laravel for channel %s: %v", channelName, err)
			} else {
				s.logger.Debug("Notified Laravel about client %s leaving channel %s due to disconnection", client.ID, channelName)
			}
		}
	}

	// Safely close the client connection
	client.Close()
}

// getOrCreateChannel gets an existing channel or creates a new one
func (s *Server) getOrCreateChannel(channelName string, private bool) *models.Channel {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	channel, exists := s.channels[channelName]
	if !exists {
		s.logger.Debug("Creating new channel '%s'", channelName)
		channel = &models.Channel{
			Name:        channelName,
			Clients:     make(map[string]*models.Client),
			IsPrivate:   private,
			RequireAuth: false,
			CreatedAt:   time.Now(),
		}
		s.channels[channelName] = channel
	}

	return channel
}

// sendError sends an error message to a client
func (s *Server) sendError(client *models.Client, errorMsg string) {
	message := models.Message{
		ID:        uuid.New().String(),
		Event:     "error",
		Data:      map[string]string{"error": errorMsg},
		Timestamp: time.Now(),
	}
	client.SendMessage(message)
}
