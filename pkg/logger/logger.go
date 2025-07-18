package logger

import (
	"log"
	"os"
)

// Logger wraps the standard logger with additional functionality
type Logger struct {
	*log.Logger
	debug bool
}

// New creates a new logger instance
func New(debug bool) *Logger {
	return &Logger{
		Logger: log.New(os.Stdout, "", log.LstdFlags),
		debug:  debug,
	}
}

// Debug logs a debug message if debug mode is enabled
func (l *Logger) Debug(format string, args ...interface{}) {
	if l.debug {
		l.Printf("[DEBUG] "+format, args...)
	}
}

// Info logs an info message
func (l *Logger) Info(format string, args ...interface{}) {
	l.Printf("[INFO] "+format, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(format string, args ...interface{}) {
	l.Printf("[WARN] "+format, args...)
}

// Error logs an error message
func (l *Logger) Error(format string, args ...interface{}) {
	l.Printf("[ERROR] "+format, args...)
}

// Fatal logs a fatal message and exits
func (l *Logger) Fatal(format string, args ...interface{}) {
	l.Printf("[FATAL] "+format, args...)
	os.Exit(1)
}

// ClientConnected logs a client connection
func (l *Logger) ClientConnected(clientID, remoteAddr, userAgent string) {
	l.Info("Client connected: %s from %s (User-Agent: %s)", clientID, remoteAddr, userAgent)
}

// ClientDisconnected logs a client disconnection
func (l *Logger) ClientDisconnected(clientID, username, remoteAddr string) {
	l.Info("Client %s (%s) disconnected from %s", clientID, username, remoteAddr)
}

// ClientAuthenticated logs successful authentication
func (l *Logger) ClientAuthenticated(clientID, username, userID string) {
	l.Info("✅ Client %s authenticated successfully as user %s (%s)", clientID, username, userID)
}

// ClientAuthenticationFailed logs failed authentication
func (l *Logger) ClientAuthenticationFailed(clientID string, err error) {
	l.Error("❌ Client %s JWT authentication failed: %v", clientID, err)
}

// MessageReceived logs an incoming message
func (l *Logger) MessageReceived(clientID, username, action string, data interface{}) {
	l.Info("📥 INCOMING MESSAGE from client %s (user: %s): action=%s", clientID, username, action)
}

// MessageSent logs an outgoing message
func (l *Logger) MessageSent(clientID, username, channel, event string, data interface{}) {
	l.Info("📤 MESSAGE SENT by client %s (%s) to channel '%s': event=%s", clientID, username, channel, event)
}

// ChannelJoined logs a channel join
func (l *Logger) ChannelJoined(clientID, username, channel string) {
	l.Info("Client %s (%s) successfully joined channel '%s'", clientID, username, channel)
}

// ChannelLeft logs a channel leave
func (l *Logger) ChannelLeft(clientID, username, channel string) {
	l.Info("Client %s (%s) successfully left channel '%s'", clientID, username, channel)
}

// WebSocketError logs WebSocket errors with context
func (l *Logger) WebSocketError(clientID string, err error) {
	if isNormalClosure(err) {
		l.Info("✅ Client %s disconnected normally", clientID)
	} else if isAbnormalClosure(err) {
		l.Warn("🔌 Client %s disconnected abnormally (code 1006 - network issue, browser closed, etc.): %v", clientID, err)
	} else {
		l.Error("❌ Client %s disconnected with error: %v", clientID, err)
	}
}

// PingSent logs a ping sent to client
func (l *Logger) PingSent(clientID string) {
	l.Debug("📍 Sent ping to client %s", clientID)
}

// PongReceived logs a pong received from client
func (l *Logger) PongReceived(clientID string) {
	l.Debug("🏓 Pong received from client %s", clientID)
}

// LaravelCommand logs Laravel command execution
func (l *Logger) LaravelCommand(command string) {
	l.Info("🚀 Executing Laravel command: %s", command)
}

// LaravelCommandSuccess logs successful Laravel command execution
func (l *Logger) LaravelCommandSuccess(command, output string) {
	l.Info("Laravel command '%s' executed successfully: %s", command, output)
}

// LaravelCommandError logs failed Laravel command execution
func (l *Logger) LaravelCommandError(command string, err error, output string) {
	l.Error("Error executing Laravel command '%s': %v, Output: %s", command, err, output)
}

// TempFileCreated logs temporary file creation
func (l *Logger) TempFileCreated(filePath string) {
	l.Debug("Created temp payload file: %s", filePath)
}

// TempFileCleanup logs temporary file cleanup
func (l *Logger) TempFileCleanup(count int) {
	if count > 0 {
		l.Info("Cleaned up %d expired temp files", count)
	}
}

// isNormalClosure checks if the error is a normal WebSocket closure
func isNormalClosure(err error) bool {
	return err != nil && err.Error() == "websocket: close 1000 (normal closure)"
}

// isAbnormalClosure checks if the error is an abnormal WebSocket closure
func isAbnormalClosure(err error) bool {
	return err != nil && (err.Error() == "websocket: close 1006 (abnormal closure)" ||
		err.Error() == "websocket: close 1001 (going away)")
}
