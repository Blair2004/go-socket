package config

import "errors"

var (
	// ErrEmptyPort indicates an empty port configuration
	ErrEmptyPort = errors.New("port cannot be empty")

	// ErrEmptyJWTSecret indicates an empty JWT secret
	ErrEmptyJWTSecret = errors.New("JWT secret cannot be empty")

	// ErrEmptyHTTPToken indicates an empty HTTP API token
	ErrEmptyHTTPToken = errors.New("HTTP API token cannot be empty")
)
